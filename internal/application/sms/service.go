package sms

import (
	"context"
	"regexp"
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
	"github.com/iniwex5/vohive/pkg/smscodec"
)

type Service struct {
	devices *device.Service
	ops     *operation.Manager
	runtime *runtime.Runtime
	mu      sync.RWMutex
	cache   []backend.SMSMessage
	loaded  bool
}

func NewService(devices *device.Service, ops *operation.Manager, runtime *runtime.Runtime) *Service {
	return &Service{devices: devices, ops: ops, runtime: runtime}
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

// Refresh reads module storage and merges it into the process-local inbox.
// The cache is intentionally not persisted and is not cleared by Clear.
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
	for index := range items {
		items[index].Code = VerificationCode(items[index].Body)
	}
	s.mu.RLock()
	prevCount, loaded := len(s.cache), s.loaded
	s.mu.RUnlock()
	merged, fresh := s.merge(items)
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
	return notification.SMSMessageEvent{Index: message.Index, Sender: message.Sender, Recipient: message.Recipient, Body: message.Body, Code: message.Code, ReceivedAt: message.ReceivedAt}
}

func (s *Service) merge(items []backend.SMSMessage) ([]backend.SMSMessage, []backend.SMSMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := make(map[string]struct{}, len(s.cache)+len(items))
	for _, item := range s.cache {
		seen[smsCacheKey(item)] = struct{}{}
	}
	var fresh []backend.SMSMessage
	for _, item := range items {
		if item.Code == "" {
			item.Code = VerificationCode(item.Body)
		}
		key := smsCacheKey(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		s.cache = append(s.cache, item)
		fresh = append(fresh, item)
	}
	sort.SliceStable(s.cache, func(i, j int) bool {
		return s.cache[i].ReceivedAt.After(s.cache[j].ReceivedAt)
	})
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
	message.Code = VerificationCode(message.Body)
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

var codePattern = regexp.MustCompile(`\b\d{4,8}\b`)

func VerificationCode(body string) string { return codePattern.FindString(body) }

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
		progress(100, "sent")
		s.ops.Publish("sms.updated", map[string]any{"operation": "send"})
		return nil
	}), nil
}
