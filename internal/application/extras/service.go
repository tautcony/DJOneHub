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
}

type callCandidate struct {
	Index                    int
	Direction, State, Number string
}

var clccPattern = regexp.MustCompile(`\+CLCC:\s*(\d+),(\d+),(\d+),(\d+),(\d+)(?:,"([^"]*)",\d+)?`)

type GPSFix struct {
	UTC        string    `json:"utc"`
	Latitude   string    `json:"latitude"`
	Longitude  string    `json:"longitude"`
	HDOP       string    `json:"hdop"`
	Altitude   string    `json:"altitude"`
	Fix        string    `json:"fix"`
	Satellites string    `json:"satellites"`
	Timestamp  time.Time `json:"timestamp"`
}

type GPSStatus struct {
	Enabled       bool      `json:"enabled"`
	LastFix       *GPSFix   `json:"last_fix,omitempty"`
	LastChecked   time.Time `json:"last_checked"`
	LastError     string    `json:"last_error,omitempty"`
	PollIntervalS int       `json:"poll_interval_s"`
}

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
	devices          *device.Service
	ops              *operation.Manager
	runtime          *runtime.Runtime
	store            *storage.JSONStore
	notesMu          sync.Mutex
	notes            map[string]ProfileNote
	notesLoaded      bool
	callMu           sync.RWMutex
	active           *CallRecord
	history          []CallRecord
	callConfigured   bool
	lastPoll         time.Time
	lastPollError    string
	gpsMu            sync.RWMutex
	gpsEnabled       bool
	gpsFix           *GPSFix
	gpsChecked       time.Time
	gpsError         string
	gpsLastPublished gpsPublished
}

// gpsPublished is the last gps.updated payload fingerprint; events are only
// published when the state actually changed.
type gpsPublished struct {
	enabled                        bool
	utc, lat, lng, hdop, sats, err string
}

func NewService(devices *device.Service, ops *operation.Manager, rt *runtime.Runtime, storePath string) *Service {
	return &Service{devices: devices, ops: ops, runtime: rt, store: storage.NewJSONStore(storePath), notes: map[string]ProfileNote{}}
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

func (s *Service) Start(ctx context.Context) {
	go s.callPoller(ctx)
	go s.gpsPoller(ctx)
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
		s.setCallPollError(err)
		return
	}
	s.callMu.Lock()
	configured := s.callConfigured
	s.callMu.Unlock()
	if !configured {
		if _, err = raw.RawAT(ctx, "AT+CLIP=1"); err != nil {
			s.setCallPollError(err)
			return
		}
		s.callMu.Lock()
		s.callConfigured = true
		s.callMu.Unlock()
	}
	response, err := raw.RawAT(ctx, "AT+CLCC")
	if err != nil {
		s.setCallPollError(err)
		return
	}
	s.applyCalls(parseCLCC(response), time.Now().UTC())
	s.setCallPollError(nil)
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

func (s *Service) applyCalls(calls []callCandidate, now time.Time) {
	var selected *callCandidate
	for i := range calls {
		if selected == nil || callPriority(calls[i].State) > callPriority(selected.State) {
			selected = &calls[i]
		}
	}
	s.callMu.Lock()
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
		s.active = &CallRecord{ID: fmt.Sprintf("%d-%d", now.UnixMilli(), selected.Index), Index: selected.Index, Direction: selected.Direction, State: selected.State, Number: selected.Number, StartedAt: now, UpdatedAt: now}
		active = callEventFor(*s.active)
		if s.active.Direction == "incoming" && (s.active.State == "incoming" || s.active.State == "waiting") {
			activeType = notification.EventCallIncoming
		} else {
			activeType = notification.EventCallUpdated
		}
	} else {
		oldState, oldNumber := s.active.State, s.active.Number
		s.active.State, s.active.UpdatedAt = selected.State, now
		if selected.Number != "" {
			s.active.Number = selected.Number
		}
		if s.active.State != oldState || s.active.Number != oldNumber {
			active = callEventFor(*s.active)
			activeType = notification.EventCallUpdated
		}
	}
	s.callMu.Unlock()
	if archived != nil {
		s.publishCallEnd(*archived)
	}
	if activeType != "" {
		s.ops.Publish(activeType, active)
	}
}

// CallEventFromRecord converts a call record into the bridge DTO; used by the
// app assembly to baseline the notification policy at startup.
func CallEventFromRecord(record CallRecord) notification.CallEvent { return callEventFor(record) }

// callEventFor converts a call record into the bridge DTO.
func callEventFor(record CallRecord) notification.CallEvent {
	return notification.CallEvent{ID: record.ID, Direction: record.Direction, State: record.State, Number: record.Number, StartedAt: record.StartedAt, EndedAt: record.EndedAt, Missed: record.Missed}
}

func (s *Service) publishCallEnd(record CallRecord) {
	event := callEventFor(record)
	if record.Missed && record.Direction == "incoming" {
		s.ops.Publish(notification.EventCallMissed, event)
		return
	}
	s.ops.Publish(notification.EventCallEnded, event)
}

func (s *Service) setCallPollError(err error) {
	s.callMu.Lock()
	defer s.callMu.Unlock()
	s.lastPoll = time.Now().UTC()
	s.lastPollError = ""
	if err != nil {
		s.lastPollError = err.Error()
	}
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

func (s *Service) gpsPoller(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	select {
	case <-ctx.Done():
		return
	case <-time.After(2 * time.Second):
	}
	s.syncGPS(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.gpsMu.RLock()
			enabled := s.gpsEnabled
			s.gpsMu.RUnlock()
			if enabled {
				_, _ = s.refreshGPS(ctx)
			}
		}
	}
}

func (s *Service) syncGPS(ctx context.Context) {
	raw, err := s.raw(ctx, domain.CapabilityGPS, "gps_status")
	if err != nil {
		return
	}
	response, err := raw.RawAT(ctx, "AT+QGPS?")
	if err != nil {
		return
	}
	s.gpsMu.Lock()
	s.gpsEnabled = strings.Contains(response, "+QGPS: 1")
	if !s.gpsEnabled {
		s.gpsFix = nil
	}
	s.gpsMu.Unlock()
	s.publishGPSIfChanged()
}

func parseGPS(response string) (*GPSFix, error) {
	var line string
	for _, candidate := range strings.Split(response, "\n") {
		candidate = strings.TrimSpace(candidate)
		if strings.HasPrefix(candidate, "+QGPSLOC:") {
			line = strings.TrimSpace(strings.TrimPrefix(candidate, "+QGPSLOC:"))
			break
		}
	}
	if line == "" {
		return nil, fmt.Errorf("no GPS fix is available")
	}
	fields := strings.Split(line, ",")
	if len(fields) < 11 {
		return nil, fmt.Errorf("GPS response is incomplete")
	}
	return &GPSFix{UTC: strings.TrimSpace(fields[0]), Latitude: strings.TrimSpace(fields[1]), Longitude: strings.TrimSpace(fields[2]), HDOP: strings.TrimSpace(fields[3]), Altitude: strings.TrimSpace(fields[4]), Fix: strings.TrimSpace(fields[5]), Satellites: strings.TrimSpace(fields[10]), Timestamp: time.Now().UTC()}, nil
}

func (s *Service) GPS(ctx context.Context) GPSStatus {
	s.gpsMu.RLock()
	defer s.gpsMu.RUnlock()
	return GPSStatus{Enabled: s.gpsEnabled, LastFix: s.gpsFix, LastChecked: s.gpsChecked, LastError: s.gpsError, PollIntervalS: 15}
}
func (s *Service) refreshGPS(ctx context.Context) (*GPSFix, error) {
	raw, err := s.raw(ctx, domain.CapabilityGPS, "gps_refresh")
	if err != nil {
		s.setGPS(nil, err)
		return nil, err
	}
	response, err := raw.RawAT(ctx, "AT+QGPSLOC=2")
	if err != nil {
		s.setGPS(nil, err)
		return nil, err
	}
	fix, err := parseGPS(response)
	s.setGPS(fix, err)
	return fix, err
}
func (s *Service) setGPS(fix *GPSFix, err error) {
	s.gpsMu.Lock()
	s.gpsChecked = time.Now().UTC()
	if err != nil {
		s.gpsError = err.Error()
	} else {
		s.gpsFix, s.gpsError = fix, ""
	}
	s.gpsMu.Unlock()
	s.publishGPSIfChanged()
}
func (s *Service) StartGPS(ctx context.Context) (GPSStatus, error) {
	raw, err := s.raw(ctx, domain.CapabilityGPS, "gps_start")
	if err != nil {
		return s.GPS(ctx), err
	}
	response, err := raw.RawAT(ctx, "AT+QGPS=1")
	if err != nil || !strings.Contains(response, "OK") {
		if err == nil {
			err = fmt.Errorf("modem did not confirm GPS start")
		}
		return s.GPS(ctx), err
	}
	s.gpsMu.Lock()
	s.gpsEnabled = true
	s.gpsError = ""
	s.gpsMu.Unlock()
	s.publishGPSIfChanged()
	_, _ = s.refreshGPS(ctx)
	return s.GPS(ctx), nil
}
func (s *Service) StopGPS(ctx context.Context) (GPSStatus, error) {
	raw, err := s.raw(ctx, domain.CapabilityGPS, "gps_stop")
	if err != nil {
		return s.GPS(ctx), err
	}
	response, err := raw.RawAT(ctx, "AT+QGPSEND")
	if err != nil || !strings.Contains(response, "OK") {
		if err == nil {
			err = fmt.Errorf("modem did not confirm GPS stop")
		}
		return s.GPS(ctx), err
	}
	s.gpsMu.Lock()
	s.gpsEnabled = false
	s.gpsFix, s.gpsError = nil, ""
	s.gpsMu.Unlock()
	s.publishGPSIfChanged()
	return s.GPS(ctx), nil
}

// publishGPSIfChanged publishes gps.updated only when the observable state
// (enabled, fix, error) changed since the last publication.
func (s *Service) publishGPSIfChanged() {
	s.gpsMu.Lock()
	defer s.gpsMu.Unlock()
	event := notification.GPSUpdateEvent{Enabled: s.gpsEnabled, LastChecked: s.gpsChecked, LastError: s.gpsError}
	var current gpsPublished
	if s.gpsFix != nil {
		current = gpsPublished{enabled: s.gpsEnabled, utc: s.gpsFix.UTC, lat: s.gpsFix.Latitude, lng: s.gpsFix.Longitude, hdop: s.gpsFix.HDOP, sats: s.gpsFix.Satellites}
		event.Fix = &notification.GPSFixEvent{UTC: s.gpsFix.UTC, Latitude: s.gpsFix.Latitude, Longitude: s.gpsFix.Longitude, HDOP: s.gpsFix.HDOP, Satellites: s.gpsFix.Satellites}
	} else {
		current = gpsPublished{enabled: s.gpsEnabled, err: s.gpsError}
	}
	if current == s.gpsLastPublished {
		return
	}
	s.gpsLastPublished = current
	s.ops.Publish(notification.EventGPSUpdated, event)
}
func (s *Service) RefreshGPS(ctx context.Context) (*GPSFix, error) { return s.refreshGPS(ctx) }

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
	if iccid == "" {
		return fmt.Errorf("iccid is required")
	}
	if len(note.Label) > 80 || len(note.Phone) > 80 || len(note.Tags) > 200 {
		return fmt.Errorf("profile note is too long")
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
