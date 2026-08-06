package extras

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vohive/internal/application/device"
	"github.com/iniwex5/vohive/internal/application/notification"
	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/backend"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/runtime"
	"github.com/iniwex5/vohive/internal/storage"
	"github.com/iniwex5/vohive/pkg/logger"
)

type CallRecord struct {
	ID        string     `json:"id"`
	Index     int        `json:"index"`
	Direction string     `json:"direction"`
	State     string     `json:"state"`
	Number    string     `json:"number,omitempty"`
	StartedAt time.Time  `json:"started_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Missed    bool       `json:"missed"`
	ICCID     string     `json:"iccid,omitempty"`
	// NotificationEligible is process-local policy state. It is deliberately
	// excluded from the API and persistence contracts: notification
	// reconciliation uses it to distinguish real calls from startup leftovers.
	NotificationEligible bool `json:"-"`
}

type callCandidate struct {
	Index                    int
	Direction, State, Number string
}

var clccPattern = regexp.MustCompile(`\+CLCC:\s*(\d+),(\d+),(\d+),(\d+),(\d+)(?:,"([^"]*)",\d+)?`)

type Status struct {
	Active        *CallRecord  `json:"active"`
	History       []CallRecord `json:"history"`
	Polling       bool         `json:"polling"`
	PollIntervalS int          `json:"poll_interval_s"`
	LastPoll      time.Time    `json:"last_poll"`
	LastPollError string       `json:"last_poll_error,omitempty"`
}

type ProfileNote struct {
	Label string `json:"label"`
	Phone string `json:"phone"`
	Tags  string `json:"tags"`
}

type Service struct {
	devices        *device.Service
	ops            *operation.Manager
	runtime        *runtime.Runtime
	store          storage.ValueStore
	callStore      *storage.SQLiteStore
	notesMu        sync.Mutex
	notes          map[string]ProfileNote
	notesLoaded    bool
	callMu         sync.RWMutex
	active         *CallRecord
	history        []CallRecord
	callConfigured bool
	// baselineDone marks the end of the startup settling window. Calls already
	// present during that window are presumed to have started before the app did,
	// so they are tracked as state but never announced: no call.incoming, and no
	// call.missed/call.ended when they disappear. Only calls first seen after the
	// baseline publish real events.
	baselineDone bool
	// baselineSnapshots counts successful CLCC snapshots consumed during the
	// startup settling window. Two snapshots avoid treating a modem entry that
	// appears one poll late as a new user call.
	baselineSnapshots int
	lastPoll          time.Time
	lastPollError     string

	// stopMu guards the cancel/done pair created by Start for the internally
	// started call poller, following the notification service's Stop pattern.
	stopMu sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewService(devices *device.Service, ops *operation.Manager, rt *runtime.Runtime, store storage.ValueStore, callStore ...*storage.SQLiteStore) *Service {
	service := &Service{devices: devices, ops: ops, runtime: rt, store: store, notes: map[string]ProfileNote{}}
	if len(callStore) > 0 && callStore[0] != nil {
		service.callStore = callStore[0]
		history, err := service.callStore.ListCalls(100)
		if err != nil {
			service.lastPollError = fmt.Sprintf("load call history: %v", err)
		} else {
			service.history = make([]CallRecord, 0, len(history))
			for _, record := range history {
				service.history = append(service.history, callRecordFromStorage(record))
			}
		}
	}
	return service
}

func callRecordFromStorage(record storage.CallRecord) CallRecord {
	return CallRecord{
		ID: record.ID, Index: record.Index, Direction: record.Direction, State: record.State,
		Number: record.Number, StartedAt: record.StartedAt, UpdatedAt: record.UpdatedAt,
		EndedAt: record.EndedAt, Missed: record.Missed, ICCID: record.ICCID,
	}
}

func (s *Service) raw(ctx context.Context, capability domain.Capability, operationName string) (backend.RawATBackend, error) {
	b, err := s.devices.RequireCapability(capability, operationName)
	if err != nil {
		return nil, err
	}
	raw, ok := b.(backend.RawATBackend)
	if !ok {
		return nil, derrors.CapabilityMissing(string(capability), operationName, "the selected backend does not expose raw AT commands")
	}
	return raw, nil
}

// Start runs the internally started call poller. It stores the cancel/done
// pair that Stop uses to join the poller before shutdown closes the store.
func (s *Service) Start(ctx context.Context) {
	s.stopMu.Lock()
	if s.cancel != nil {
		s.stopMu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.cancel = cancel
	s.done = done
	s.stopMu.Unlock()
	s.callMu.Lock()
	s.baselineDone = false
	s.baselineSnapshots = 0
	if s.active != nil {
		s.active.NotificationEligible = false
	}
	s.callMu.Unlock()
	logger.Info("[calls] startup baseline reset", "target_snapshots", callBaselineSnapshotCount)
	go func() {
		defer close(done)
		s.callPoller(runCtx)
	}()
}

// Stop cancels the call poller and waits for it to join within the deadline.
// Repeated calls are safe.
func (s *Service) Stop(ctx context.Context) error {
	s.stopMu.Lock()
	cancel := s.cancel
	done := s.done
	s.cancel = nil
	s.done = nil
	s.stopMu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	if done != nil {
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (s *Service) callPoller(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	select {
	case <-ctx.Done():
		return
	case <-time.After(2 * time.Second):
	}
	for {
		s.pollCall(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) pollCall(ctx context.Context) {
	raw, err := s.raw(ctx, domain.CapabilityCallMonitor, "call_status")
	if err != nil {
		if s.setCallPollError(err) {
			logger.Warn("[calls] poll unavailable", "error", err)
		}
		return
	}
	iccid := s.devices.CurrentICCID(ctx)
	s.callMu.Lock()
	configured := s.callConfigured
	s.callMu.Unlock()
	if !configured {
		if _, err = raw.RawAT(ctx, "AT+CLIP=1"); err != nil {
			if s.setCallPollError(err) {
				logger.Warn("[calls] AT+CLIP setup failed", "error", err)
			}
			return
		}
		s.callMu.Lock()
		s.callConfigured = true
		s.callMu.Unlock()
	}
	response, err := raw.RawAT(ctx, "AT+CLCC")
	if err != nil {
		if s.setCallPollError(err) {
			logger.Warn("[calls] CLCC poll failed", "error", err)
		}
		return
	}
	calls := parseCLCC(response)
	s.callMu.RLock()
	baselineDone, baselineSnapshots := s.baselineDone, s.baselineSnapshots
	s.callMu.RUnlock()
	logger.Debug("[calls] CLCC snapshot",
		"candidates", len(calls),
		"baseline_done", baselineDone,
		"baseline_snapshots", baselineSnapshots,
		"parsed", callCandidatesSummary(calls),
	)
	if err := s.applyCalls(calls, time.Now().UTC(), iccid); err != nil {
		if s.setCallPollError(err) {
			logger.Warn("[calls] apply snapshot failed", "error", err)
		}
		return
	}
	if s.setCallPollError(nil) {
		logger.Info("[calls] polling recovered")
	}
}

func callCandidatesSummary(calls []callCandidate) string {
	if len(calls) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(calls))
	for _, call := range calls {
		parts = append(parts, fmt.Sprintf("{index=%d direction=%s state=%s number=%q}", call.Index, call.Direction, call.State, call.Number))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func parseCLCC(response string) []callCandidate {
	matches := clccPattern.FindAllStringSubmatch(response, -1)
	result := make([]callCandidate, 0, len(matches))
	for _, match := range matches {
		if match[4] != "0" {
			continue
		}
		index, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		result = append(result, callCandidate{Index: index, Direction: mapDirection(match[2]), State: mapCallState(match[3]), Number: strings.TrimSpace(match[6])})
	}
	return result
}

func mapDirection(value string) string {
	if value == "1" {
		return "incoming"
	}
	return "outgoing"
}
func mapCallState(value string) string {
	switch value {
	case "0":
		return "active"
	case "1":
		return "held"
	case "2":
		return "dialing"
	case "3":
		return "alerting"
	case "4":
		return "incoming"
	case "5":
		return "waiting"
	default:
		return "unknown"
	}
}
func callPriority(value string) int {
	switch value {
	case "incoming", "waiting":
		return 5
	case "active":
		return 4
	case "alerting":
		return 3
	case "dialing":
		return 2
	case "held":
		return 1
	default:
		return 0
	}
}

const callBaselineSnapshotCount = 2

func (s *Service) applyCalls(calls []callCandidate, now time.Time, iccid string) error {
	var selected *callCandidate
	for i := range calls {
		if selected == nil || callPriority(calls[i].State) > callPriority(selected.State) {
			selected = &calls[i]
		}
	}
	s.callMu.Lock()
	baselineDone := s.baselineDone
	if !baselineDone {
		s.baselineSnapshots++
		if s.baselineSnapshots >= callBaselineSnapshotCount {
			s.baselineDone = true
			logger.Info("[calls] startup baseline complete", "snapshots", s.baselineSnapshots)
		}
	}
	announced := s.active != nil && s.active.NotificationEligible
	var archived *CallRecord
	var active notification.CallEvent
	var activeType string
	if selected == nil {
		if s.active != nil {
			ended := now
			s.active.EndedAt = &ended
			s.active.UpdatedAt = now
			s.active.Missed = s.active.Direction == "incoming" && (s.active.State == "incoming" || s.active.State == "waiting")
			record := *s.active
			s.history = append([]CallRecord{record}, s.history...)
			if len(s.history) > 100 {
				s.history = s.history[:100]
			}
			s.active = nil
			archived = &record
		}
	} else if s.active == nil || s.active.Index != selected.Index || s.active.Direction != selected.Direction {
		if s.active != nil {
			ended := now
			s.active.EndedAt = &ended
			s.active.UpdatedAt = now
			s.active.Missed = s.active.Direction == "incoming" && (s.active.State == "incoming" || s.active.State == "waiting")
			record := *s.active
			s.history = append([]CallRecord{record}, s.history...)
			if len(s.history) > 100 {
				s.history = s.history[:100]
			}
			archived = &record
		}
		s.active = &CallRecord{
			ID: fmt.Sprintf("%d-%d", now.UnixMilli(), selected.Index), Index: selected.Index,
			Direction: selected.Direction, State: selected.State, Number: selected.Number,
			StartedAt: now, UpdatedAt: now, ICCID: iccid, NotificationEligible: baselineDone,
		}
		// Calls seen while the startup settling window is open are presumed to
		// have started before the app did and are only tracked, never announced.
		// Only transitions seen after the baseline publish real events.
		if baselineDone {
			active = callEventFor(*s.active)
			if s.active.Direction == "incoming" && (s.active.State == "incoming" || s.active.State == "waiting") {
				activeType = notification.EventCallIncoming
			} else {
				activeType = notification.EventCallUpdated
			}
		}
	} else {
		oldState, oldNumber := s.active.State, s.active.Number
		s.active.State, s.active.UpdatedAt = selected.State, now
		if selected.Number != "" {
			s.active.Number = selected.Number
		}
		if s.active.NotificationEligible && (s.active.State != oldState || s.active.Number != oldNumber) {
			active = callEventFor(*s.active)
			activeType = notification.EventCallUpdated
		}
	}
	activeEligibleAfter := s.active != nil && s.active.NotificationEligible
	s.callMu.Unlock()
	if archived != nil {
		if s.callStore != nil {
			if err := s.callStore.InsertCall(storage.CallRecord{
				ID: archived.ID, Index: archived.Index, Direction: archived.Direction, State: archived.State,
				Number: archived.Number, StartedAt: archived.StartedAt, UpdatedAt: archived.UpdatedAt,
				EndedAt: archived.EndedAt, Missed: archived.Missed, ICCID: archived.ICCID,
			}); err != nil {
				return err
			}
		}
		// A call whose start was never announced ends silently: no ended/missed
		// prompt for a leftover call the user was not notified about.
		if announced {
			logger.Info("[calls] publish end", "call_id", archived.ID, "index", archived.Index, "direction", archived.Direction, "state", archived.State, "missed", archived.Missed)
			s.publishCallEnd(*archived)
		} else {
			logger.Info("[calls] silent archive", "call_id", archived.ID, "index", archived.Index, "direction", archived.Direction, "state", archived.State, "missed", archived.Missed, "reason", "unannounced_startup_call")
		}
	}
	if activeType != "" {
		logger.Info("[calls] publish event", "event", activeType, "call_id", active.ID, "direction", active.Direction, "state", active.State, "baseline_done", baselineDone)
		s.ops.Publish(activeType, active)
	} else if selected != nil {
		logger.Debug("[calls] suppress active", "index", selected.Index, "direction", selected.Direction, "state", selected.State, "baseline_done", baselineDone, "notification_eligible", activeEligibleAfter)
	}
	return nil
}

// CallEventFromRecord converts a call record into the bridge DTO; used by the
// app assembly to baseline the notification policy at startup.
func CallEventFromRecord(record CallRecord) notification.CallEvent { return callEventFor(record) }

// callEventFor converts a call record into the bridge DTO.
func callEventFor(record CallRecord) notification.CallEvent {
	return notification.CallEvent{
		ID: record.ID, Direction: record.Direction, State: record.State, Number: record.Number,
		StartedAt: record.StartedAt, EndedAt: record.EndedAt, Missed: record.Missed,
		NotificationEligible: record.NotificationEligible,
	}
}

func (s *Service) publishCallEnd(record CallRecord) {
	event := callEventFor(record)
	if record.Missed && record.Direction == "incoming" {
		s.ops.Publish(notification.EventCallMissed, event)
		return
	}
	s.ops.Publish(notification.EventCallEnded, event)
}

// setCallPollError updates diagnostics and reports whether the error state
// changed, so a stable polling failure is logged once instead of every tick.
func (s *Service) setCallPollError(err error) bool {
	s.callMu.Lock()
	defer s.callMu.Unlock()
	s.lastPoll = time.Now().UTC()
	message := ""
	if err != nil {
		message = err.Error()
	}
	changed := s.lastPollError != message
	s.lastPollError = message
	return changed
}
func (s *Service) Calls(context.Context) Status {
	s.callMu.RLock()
	defer s.callMu.RUnlock()
	var active *CallRecord
	if s.active != nil {
		copy := *s.active
		active = &copy
	}
	return Status{Active: active, History: append([]CallRecord(nil), s.history...), Polling: true, PollIntervalS: 3, LastPoll: s.lastPoll, LastPollError: s.lastPollError}
}

func (s *Service) Reject(ctx context.Context) error {
	raw, err := s.raw(ctx, domain.CapabilityCallMonitor, "call_reject")
	if err != nil {
		return err
	}
	_, err = raw.RawAT(ctx, "AT+CHUP")
	return err
}

func (s *Service) loadNotes() error {
	if s.notesLoaded {
		return nil
	}
	if err := s.store.Read(&s.notes); err != nil {
		return err
	}
	s.notesLoaded = true
	return nil
}
func (s *Service) Notes(context.Context) (map[string]ProfileNote, error) {
	s.notesMu.Lock()
	defer s.notesMu.Unlock()
	if err := s.loadNotes(); err != nil {
		return nil, err
	}
	out := make(map[string]ProfileNote, len(s.notes))
	for key, value := range s.notes {
		out[key] = value
	}
	return out, nil
}
func (s *Service) SaveNote(_ context.Context, iccid string, note ProfileNote) error {
	iccid = strings.TrimSpace(iccid)
	note.Label, note.Phone, note.Tags = strings.TrimSpace(note.Label), strings.TrimSpace(note.Phone), strings.TrimSpace(note.Tags)
	// 校验失败分类为 InvalidRequest, 由 HTTP 层映射为显式结构化错误码
	// (design D15), 而不是落入通用 500。
	if iccid == "" {
		return derrors.New(derrors.InvalidRequest, "iccid is required", false, nil)
	}
	if len(iccid) > 22 {
		return derrors.New(derrors.InvalidRequest, "iccid is too long", false, nil)
	}
	if len(note.Label) > 80 || len(note.Phone) > 80 || len(note.Tags) > 200 {
		return derrors.New(derrors.InvalidRequest, "profile note is too long", false, nil)
	}
	s.notesMu.Lock()
	defer s.notesMu.Unlock()
	if err := s.loadNotes(); err != nil {
		return err
	}
	if note.Label == "" && note.Phone == "" && note.Tags == "" {
		delete(s.notes, iccid)
	} else {
		s.notes[iccid] = note
	}
	return s.store.Write(s.notes)
}

// Marshal helper used by callers that want to persist an extras snapshot.
func (s *Service) MarshalNotes() ([]byte, error) {
	notes, err := s.Notes(context.Background())
	if err != nil {
		return nil, err
	}
	return json.Marshal(notes)
}
