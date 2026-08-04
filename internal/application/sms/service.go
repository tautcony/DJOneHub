package sms

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
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
	"github.com/iniwex5/vohive/pkg/smscodec"
)

type Service struct {
	devices *device.Service
	ops     *operation.Manager
	runtime *runtime.Runtime
	mu      sync.RWMutex
	cache   []backend.SMSMessage
	sent    []backend.SMSMessage
	store   *storage.SQLiteStore
	loaded  bool
}

func NewService(devices *device.Service, ops *operation.Manager, runtime *runtime.Runtime, store ...*storage.SQLiteStore) *Service {
	service := &Service{devices: devices, ops: ops, runtime: runtime}
	if len(store) == 0 || store[0] == nil {
		return service
	}
	service.store = store[0]
	records, err := service.store.ListSMS("outbound")
	if err == nil {
		for _, record := range records {
			service.sent = append(service.sent, backend.SMSMessage{
				Index: record.ProviderID, Recipient: record.Recipient, Body: record.Body,
				ReceivedAt: record.ReceivedAt, RecordedAt: record.RecordedAt,
				ConcatRef: record.ConcatRef, PartNumber: record.PartNumber, TotalParts: record.TotalParts,
			})
		}
		sortMessages(service.sent)
		if len(service.sent) > 500 {
			service.sent = service.sent[:500]
		}
		service.cache = clone(service.sent)
		// Restore inbound history too: their created_at is the stable ordering
		// key, so restarting never reshuffles threads by SMSC clock skew.
		inbound, err := service.store.ListSMS("inbound")
		if err == nil {
			for _, record := range inbound {
				service.cache = append(service.cache, backend.SMSMessage{
					Index: record.ProviderID, Sender: record.Sender, Recipient: record.Recipient, Body: record.Body,
					ReceivedAt: record.ReceivedAt, RecordedAt: record.RecordedAt,
					ConcatRef: record.ConcatRef, PartNumber: record.PartNumber, TotalParts: record.TotalParts,
				})
			}
			sortMessages(service.cache)
			if len(service.cache) > 500 {
				service.cache = service.cache[:500]
			}
		}
	}
	return service
}

// MigrateLegacySentHistory imports the pre-SQLite JSON array once. Inserts
// are idempotent, so retaining the old file is harmless and recoverable.
func MigrateLegacySentHistory(store *storage.SQLiteStore, path string) error {
	if store == nil || path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var messages []backend.SMSMessage
	if err := json.Unmarshal(data, &messages); err != nil {
		return err
	}
	for _, message := range messages {
		if message.Body == "" || message.ReceivedAt.IsZero() {
			continue
		}
		if err := store.InsertSMS(storage.SMSRecord{
			Direction: "outbound", ProviderID: message.Index, Recipient: message.Recipient,
			Body: message.Body, ReceivedAt: message.ReceivedAt, RecordedAt: message.ReceivedAt,
		}); err != nil {
			return err
		}
	}
	return nil
}

// Start runs the periodic refresh that drives sms.received events, replacing
// the legacy notifier's 3-second polling.
func (s *Service) Start(ctx context.Context) {
	go s.poller(ctx)
}

func (s *Service) poller(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	select {
	case <-ctx.Done():
		return
	case <-time.After(2 * time.Second):
	}
	for {
		if _, err := s.Refresh(ctx); err != nil {
			// Polling errors are silent; the next tick retries.
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) List(ctx context.Context) ([]backend.SMSMessage, error) {
	_, err := s.devices.RequireCapability(domain.CapabilitySMSRead, "list_sms")
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	loaded := s.loaded
	items := clone(s.cache)
	s.mu.RUnlock()
	if loaded {
		return items, nil
	}
	return s.Refresh(ctx)
}

// Refresh reads module storage and merges it with locally persisted sent
// messages. Clear only affects the module inbox.
func (s *Service) Refresh(ctx context.Context) ([]backend.SMSMessage, error) {
	b, err := s.devices.RequireCapability(domain.CapabilitySMSRead, "refresh_sms")
	if err != nil {
		return nil, err
	}
	release, err := s.runtime.Acquire(ctx, runtime.ResourceAT)
	if err != nil {
		return nil, err
	}
	defer release()
	items, err := b.ListSMS(ctx)
	if err != nil {
		s.mu.RLock()
		cached := clone(s.cache)
		s.mu.RUnlock()
		if len(cached) > 0 {
			return cached, nil
		}
		return nil, err
	}
	items = Reassemble(items)
	s.mu.RLock()
	prevCount, loaded := len(s.cache), s.loaded
	s.mu.RUnlock()
	merged, fresh := s.merge(items, s.devices.CurrentICCID(ctx))
	// The first load only establishes the baseline; subsequent refreshes
	// publish the incremental messages that were not in the cache before.
	if loaded {
		for _, message := range fresh {
			s.ops.Publish(notification.EventSMSReceived, toSMSMessageEvent(message))
		}
	}
	if len(merged) != prevCount || !loaded {
		s.ops.Publish("sms.updated", map[string]any{"count": len(merged)})
	}
	return merged, nil
}

func toSMSMessageEvent(message backend.SMSMessage) notification.SMSMessageEvent {
	return notification.SMSMessageEvent{Index: message.Index, Sender: message.Sender, Recipient: message.Recipient, Body: message.Body, ReceivedAt: message.ReceivedAt, RecordedAt: message.RecordedAt, ICCID: message.ICCID}
}

func (s *Service) merge(items []backend.SMSMessage, iccid string) ([]backend.SMSMessage, []backend.SMSMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := make(map[string]struct{}, len(s.cache)+len(items))
	for _, item := range s.cache {
		seen[smsCacheKey(item)] = struct{}{}
	}
	var fresh []backend.SMSMessage
	recordedAt := time.Now().UTC()
	for _, item := range items {
		key := smsCacheKey(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		// RecordedAt is the single-clock ordering key: the moment this process
		// first saw the message. Persist it so restarts keep the same order.
		item.RecordedAt = recordedAt
		item.ICCID = iccid
		s.cache = append(s.cache, item)
		fresh = append(fresh, item)
		if s.store != nil {
			direction := "outbound"
			if item.Sender != "" {
				direction = "inbound"
			}
			_ = s.store.InsertSMS(storage.SMSRecord{
				Direction: direction, ProviderID: item.Index, Sender: item.Sender,
				Recipient: item.Recipient, Body: item.Body, ReceivedAt: item.ReceivedAt,
				RecordedAt: recordedAt, ICCID: iccid, ConcatRef: item.ConcatRef,
				PartNumber: item.PartNumber, TotalParts: item.TotalParts,
			})
		}
	}
	sortMessages(s.cache)
	if len(s.cache) > 500 {
		s.cache = s.cache[:500]
	}
	s.loaded = true
	return clone(s.cache), fresh
}

func clone(items []backend.SMSMessage) []backend.SMSMessage {
	if len(items) == 0 {
		return []backend.SMSMessage{}
	}
	return append([]backend.SMSMessage{}, items...)
}

func smsCacheKey(item backend.SMSMessage) string {
	return item.Sender + "\x00" + item.Recipient + "\x00" + item.Body + "\x00" + item.ReceivedAt.UTC().Format(time.RFC3339Nano)
}

// sortTime is the ordering key of a message: RecordedAt (single clock, set by
// this process) with the SMSC/network time as an intra-batch tie-breaker and a
// fallback for legacy records that predate the attribute.
func sortTime(m backend.SMSMessage) time.Time {
	if !m.RecordedAt.IsZero() {
		return m.RecordedAt
	}
	return m.ReceivedAt
}

func sortMessages(items []backend.SMSMessage) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := sortTime(items[i]), sortTime(items[j])
		if a.Equal(b) {
			return items[i].ReceivedAt.After(items[j].ReceivedAt)
		}
		return a.After(b)
	})
}

func (s *Service) recordSent(ctx context.Context, recipient, body string) {
	sentAt := time.Now().UTC()
	message := backend.SMSMessage{
		Index:      -int(sentAt.UnixNano()),
		Recipient:  recipient,
		Body:       body,
		ReceivedAt: sentAt,
		RecordedAt: sentAt,
	}
	if s.devices != nil {
		message.ICCID = s.devices.CurrentICCID(ctx)
	}
	s.mu.Lock()
	s.cache = append(s.cache, message)
	sortMessages(s.cache)
	if len(s.cache) > 500 {
		s.cache = s.cache[:500]
	}
	s.sent = append(s.sent, message)
	sortMessages(s.sent)
	if len(s.sent) > 500 {
		s.sent = s.sent[:500]
	}
	s.mu.Unlock()
	if s.store != nil {
		_ = s.store.InsertSMS(storage.SMSRecord{
			Direction: "outbound", ProviderID: message.Index, Recipient: message.Recipient,
			Body: message.Body, ReceivedAt: message.ReceivedAt, RecordedAt: message.RecordedAt,
			ICCID: message.ICCID,
		})
	}
}

func (s *Service) Read(ctx context.Context, index int) (backend.SMSMessage, error) {
	b, err := s.devices.RequireCapability(domain.CapabilitySMSRead, "read_sms")
	if err != nil {
		return backend.SMSMessage{}, err
	}
	provider, ok := b.(backend.SMSPort)
	if !ok {
		return backend.SMSMessage{}, derrors.CapabilityMissing("sms_read", "read_sms", "backend does not expose message contents")
	}
	release, err := s.runtime.Acquire(ctx, runtime.ResourceAT)
	if err != nil {
		return backend.SMSMessage{}, err
	}
	defer release()
	message, err := provider.ReadSMS(ctx, index)
	if err != nil {
		return backend.SMSMessage{}, err
	}
	return message, nil
}

func (s *Service) Clear(ctx context.Context) error {
	b, err := s.devices.RequireCapability(domain.CapabilitySMSRead, "clear_sms")
	if err != nil {
		return err
	}
	provider, ok := b.(backend.SMSPort)
	if !ok {
		return derrors.CapabilityMissing("sms_read", "clear_sms", "backend does not expose deletion")
	}
	release, err := s.runtime.Acquire(ctx, runtime.ResourceAT)
	if err != nil {
		return err
	}
	defer release()
	if err := provider.DeleteAllSMS(ctx); err != nil {
		return err
	}
	s.ops.Publish("sms.updated", map[string]any{"cleared": true})
	return nil
}

func Reassemble(messages []backend.SMSMessage) []backend.SMSMessage {
	reassembler := smscodec.NewReassembler()
	result := make([]backend.SMSMessage, 0, len(messages))
	for _, message := range messages {
		if message.TotalParts <= 1 || message.ConcatRef == 0 {
			result = append(result, message)
			continue
		}
		complete, body := reassembler.Add(message.Sender, smscodec.ConcatInfo{IsConcat: true, Ref: message.ConcatRef, Total: message.TotalParts, Seq: message.PartNumber}, message.Body)
		if complete {
			message.Body = body
			message.PartNumber = 0
			message.TotalParts = 0
			result = append(result, message)
		}
	}
	reassembler.Cleanup(24 * time.Hour)
	return result
}

func (s *Service) Send(ctx context.Context, recipient, body string) (string, error) {
	b, err := s.devices.RequireCapability(domain.CapabilitySMSSend, "send_sms")
	if err != nil {
		return "", err
	}
	return s.ops.Start(ctx, "sms.send", func(taskCtx context.Context, progress func(int, string)) error {
		release, err := s.runtime.Acquire(taskCtx, runtime.ResourceAT)
		if err != nil {
			return err
		}
		defer release()
		progress(10, "sending")
		if err := b.SendSMS(taskCtx, recipient, body); err != nil {
			return err
		}
		s.recordSent(taskCtx, recipient, body)
		progress(100, "sent")
		s.ops.Publish("sms.updated", map[string]any{"operation": "send"})
		return nil
	}), nil
}
