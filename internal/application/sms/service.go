package sms

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
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
	"github.com/iniwex5/vohive/pkg/smscodec"
)

// Service 拥有内收短信的消费者交付：+CMTI 引用按精确存储位置读取，完整消息
// 持久化后才确认删除。进程重启后的投递是尽力而为：内存重组缓存与未完成分片
// 引用在重启后丢失，残留的模组条目由启动基线刷新去重或由后续刷新重新读取。
type Service struct {
	devices *device.Service
	ops     *operation.Manager
	runtime *runtime.Runtime
	mu      sync.RWMutex
	cache   []backend.SMSMessage
	sent    []backend.SMSMessage
	store   *storage.SQLiteStore
	loaded  bool

	// reassembler 跨刷新周期持久保留长短信分片（task 2.7），由
	// reassemblerMu 保护；超时条目由 TTL 清理。
	reassembler   *smscodec.Reassembler
	reassemblerMu sync.Mutex

	// 内收短信消费者状态（task 2.3）：按 (SIM, sender, ref, total) 分组跟踪
	// 未完成分片在模组存储中的引用，完整消息持久化后逐一确认删除。
	consumerMu sync.Mutex
	pending    map[smsGroupKey]map[smsRefKey]struct{}

	stopOnce sync.Once
	stopped  chan struct{}

	// stopMu guards the cancel/done pair created by Start, following the
	// notification service's Stop pattern so shutdown can join this worker
	// before storage closes underneath it.
	stopMu sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewService(devices *device.Service, ops *operation.Manager, runtime *runtime.Runtime, store ...*storage.SQLiteStore) *Service {
	service := &Service{
		devices:     devices,
		ops:         ops,
		runtime:     runtime,
		reassembler: smscodec.NewReassembler(),
		pending:     make(map[smsGroupKey]map[smsRefKey]struct{}),
		stopped:     make(chan struct{}),
	}
	if len(store) == 0 || store[0] == nil {
		return service
	}
	service.store = store[0]
	// 有界分页内部迭代: 存储层返回有界页, 应用服务逐页取全, 公共契约不变
	// (design D16)。
	records, err := listAllSMS(store[0], "outbound")
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
		inbound, err := listAllSMS(store[0], "inbound")
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

// listAllSMS 逐页迭代存储层的有界列表, 返回该方向的全部记录。
func listAllSMS(store *storage.SQLiteStore, direction string) ([]storage.SMSRecord, error) {
	var all []storage.SMSRecord
	offset := 0
	for {
		page, err := store.ListSMS(direction, 0, offset)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) == 0 {
			return all, nil
		}
		if len(page) < storage.SMSListDefaultLimit {
			return all, nil
		}
		offset += len(page)
	}
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
		err := store.InsertSMS(storage.SMSRecord{
			Direction: "outbound", ProviderID: message.Index, Recipient: message.Recipient,
			Body: message.Body, ReceivedAt: message.ReceivedAt, RecordedAt: message.ReceivedAt,
		})
		if errors.Is(err, storage.ErrSMSIdentityMissing) {
			// 旧格式历史没有 SIM 身份, 不归入共享空身份键 (design D16)。
			continue
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// Start runs the periodic refresh that drives sms.received events, replacing
// the legacy notifier's 3-second polling. It stores the cancel/done pair that
// Stop uses to join this worker.
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
	go func() {
		defer close(done)
		s.poller(runCtx)
	}()
}

// Stop cancels the poller, waits for it to join within the deadline, and
// unregisters the inbound SMS consumer before the backend shuts down, so a
// stopped service never receives +CMTI notifications. Repeated calls are
// safe; on deadline expiry the poller is still cancelled but the caller
// receives the context error.
func (s *Service) Stop(ctx context.Context) error {
	s.stopMu.Lock()
	cancel := s.cancel
	done := s.done
	s.cancel = nil
	s.done = nil
	s.stopMu.Unlock()
	if cancel == nil {
		// Never started (or already stopped): unregister the consumer anyway.
		s.unregisterConsumer()
		return nil
	}
	cancel()
	s.stopOnce.Do(func() { close(s.stopped) })
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.unregisterConsumer()
	return nil
}

// unregisterConsumer detaches the +CMTI hook so no inbound delivery reaches a
// stopped service.
func (s *Service) unregisterConsumer() {
	if s.runtime == nil {
		return
	}
	if b, err := s.runtime.Backend(); err == nil {
		b.SetInboundSMSHandler(nil)
	}
}

func (s *Service) poller(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	select {
	case <-ctx.Done():
		return
	case <-s.stopped:
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
		case <-s.stopped:
			return
		case <-ticker.C:
		}
	}
}

// registerConsumer wires this service as the inbound SMS consumer of the
// current backend. Registration is idempotent and re-run on every refresh so a
// reconnected backend (new manager, new +CMTI hook) is always covered.
func (s *Service) registerConsumer(b backend.ModemBackend) {
	b.SetInboundSMSHandler(s.inboundRef)
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
	// The first refresh establishes the consumer so +CMTI notifications are
	// delivered to this service instead of being retained unread.
	s.registerConsumer(b)
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
	items = s.reassemble(items)
	s.mu.RLock()
	prevCount, loaded := len(s.cache), s.loaded
	s.mu.RUnlock()
	merged, fresh := s.merge(items, s.devices.CurrentICCID(ctx))
	// The first load only establishes the baseline: retained modem entries
	// whose message already exists in storage are deduplicated and never
	// re-published or re-notified as fresh; subsequent refreshes publish the
	// incremental messages that were not in the cache before.
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
		// 无法解码出内容的条目（如列表阶段 PDU 解析失败）不得写入存储或缓存，
		// 条目保留在模组存储中等待刷新重试。
		if strings.TrimSpace(item.Body) == "" {
			continue
		}
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
			// 身份缺失时拒绝写入 (storage.ErrSMSIdentityMissing): 消息保留在
			// 缓存与模组存储中, 下次刷新重试身份获取, 绝不归入空身份键。
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
		// 身份缺失时不落库 (storage.ErrSMSIdentityMissing): 消息保留在发送
		// 缓存, 不归入共享空身份键 (design D16)。
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
	message, err := provider.ReadSMS(ctx, backend.NewSMSRef{Index: index, Storage: s.storageFor(index)})
	if err != nil {
		return backend.SMSMessage{}, err
	}
	return message, nil
}

// storageFor 返回缓存条目记录的模组存储身份，供按索引读取/删除时精确定位。
func (s *Service) storageFor(index int) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.cache {
		if item.Index == index {
			return item.Storage
		}
	}
	return ""
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

// reassemble feeds listed messages through the service's persistent,
// mutex-protected reassembler so multipart SMS reassemble across refresh
// cycles instead of losing state every poll. Stale fragments are swept by TTL.
func (s *Service) reassemble(messages []backend.SMSMessage) []backend.SMSMessage {
	s.reassemblerMu.Lock()
	defer s.reassemblerMu.Unlock()
	result := make([]backend.SMSMessage, 0, len(messages))
	for _, message := range messages {
		if message.TotalParts <= 1 || message.ConcatRef == 0 {
			result = append(result, message)
			continue
		}
		complete, body := s.reassembler.Add(message.Sender, smscodec.ConcatInfo{IsConcat: true, Ref: message.ConcatRef, Total: message.TotalParts, Seq: message.PartNumber}, message.Body)
		if complete {
			message.Body = body
			message.PartNumber = 0
			message.TotalParts = 0
			result = append(result, message)
		}
	}
	s.reassembler.Cleanup(24 * time.Hour)
	return result
}

// smsGroupKey 标识一条长短信的分组（SIM 身份 + 发送方 + 引用号 + 总分片数，
// 总分片数防止 8 位引用号回绕后分组碰撞）。
type smsGroupKey struct {
	SIM    string
	Sender string
	Ref    int
	Total  int
}

// smsRefKey 标识模组存储中的一个短信条目（task 2.3 的幂等跟踪键）。
type smsRefKey struct {
	Storage string
	Index   int
}

// inboundRef 是注册到后端的 +CMTI 消费者。它按精确引用读取并解码短信，要求
// 非空且稳定的 SIM 身份，未完成的长短信分片保留在模组存储中并按
// (SIM, storage, index) 幂等跟踪；完整消息被持久化（包括 INSERT OR IGNORE
// 冲突忽略视为已持久化）后才确认删除全部组件引用。任何失败都保留所有条目，
// 由后续刷新重试。EventBus 发布是最佳努力，不构成持久化确认门槛。
func (s *Service) inboundRef(ref backend.NewSMSRef) {
	s.consumerMu.Lock()
	defer s.consumerMu.Unlock()
	select {
	case <-s.stopped:
		return
	default:
	}

	b, err := s.devices.RequireCapability(domain.CapabilitySMSRead, "inbound_sms")
	if err != nil {
		return // backend unavailable: retain the entry
	}
	provider, ok := b.(backend.SMSPort)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	release, err := s.runtime.Acquire(ctx, runtime.ResourceAT)
	if err != nil {
		return
	}
	defer release()

	iccid := strings.TrimSpace(s.devices.CurrentICCID(ctx))
	if iccid == "" {
		// 无稳定 SIM 身份无法盖章记录：保留条目等待下次刷新。
		return
	}

	message, err := provider.ReadSMS(ctx, ref)
	if err != nil {
		return // read/decode failure: retain for retry
	}
	message.ICCID = iccid
	message.RecordedAt = time.Now().UTC()

	refs := []backend.NewSMSRef{ref}
	if message.TotalParts > 1 && message.ConcatRef != 0 {
		group := smsGroupKey{SIM: iccid, Sender: message.Sender, Ref: message.ConcatRef, Total: message.TotalParts}
		complete, body := s.reassembleSegment(group, message, ref)
		if !complete {
			return // 分片未完成：保留条目，等待后续分片
		}
		message.Body = body
		message.PartNumber = 0
		message.TotalParts = 0
		refs = s.takePendingRefs(group)
		if len(refs) == 0 {
			refs = []backend.NewSMSRef{ref}
		}
	}

	if !s.persistDelivered(ctx, iccid, message) {
		return // 未持久化：保留全部条目供刷新重试
	}
	// 持久化完成后的最佳努力通知；不作为确认门槛。
	if s.recordDelivered(message) {
		s.ops.Publish(notification.EventSMSReceived, toSMSMessageEvent(message))
		s.ops.Publish("sms.updated", map[string]any{"count": len(s.cache)})
	}
	// 确认：仅在所有组件引用都完成持久化后删除。
	for _, component := range refs {
		if err := provider.DeleteSMS(ctx, component); err != nil {
			logger.Warn("[sms] 确认删除短信条目失败，下次刷新重试", "index", component.Index, "storage", component.Storage, "err", err)
		}
	}
}

// reassembleSegment 将一条分片喂入持久化重组器并幂等跟踪其存储引用；返回分组
// 是否已完整。
func (s *Service) reassembleSegment(group smsGroupKey, message backend.SMSMessage, ref backend.NewSMSRef) (complete bool, body string) {
	s.reassemblerMu.Lock()
	defer s.reassemblerMu.Unlock()
	key := smsRefKey{Storage: ref.Storage, Index: ref.Index}
	if s.pending[group] == nil {
		s.pending[group] = make(map[smsRefKey]struct{})
	}
	s.pending[group][key] = struct{}{}
	complete, body = s.reassembler.Add(group.Sender, smscodec.ConcatInfo{IsConcat: true, Ref: group.Ref, Total: group.Total, Seq: message.PartNumber}, message.Body)
	// 完整分组的全部组件引用在持久化后由 takePendingRefs 统一取走并确认删除。
	return complete, body
}

// takePendingRefs 取走并清空一个分组的全部组件引用。
func (s *Service) takePendingRefs(group smsGroupKey) []backend.NewSMSRef {
	s.reassemblerMu.Lock()
	defer s.reassemblerMu.Unlock()
	refs := make([]backend.NewSMSRef, 0, len(s.pending[group]))
	for key := range s.pending[group] {
		refs = append(refs, backend.NewSMSRef{Storage: key.Storage, Index: key.Index})
	}
	delete(s.pending, group)
	return refs
}

// persistDelivered 持久化一条已交付消息；INSERT OR IGNORE 冲突忽略（rows
// affected = 0，相同消息已存在）同样视为持久化完成。
func (s *Service) persistDelivered(ctx context.Context, iccid string, message backend.SMSMessage) bool {
	if s.store == nil {
		return false
	}
	direction := "outbound"
	if message.Sender != "" {
		direction = "inbound"
	}
	if err := s.store.InsertSMS(storage.SMSRecord{
		Direction: direction, ProviderID: message.Index, Sender: message.Sender,
		Recipient: message.Recipient, Body: message.Body, ReceivedAt: message.ReceivedAt,
		RecordedAt: message.RecordedAt, ICCID: iccid, ConcatRef: message.ConcatRef,
		PartNumber: message.PartNumber, TotalParts: message.TotalParts,
	}); err != nil {
		logger.Warn("[sms] 持久化内收短信失败，保留条目", "index", message.Index, "err", err)
		return false
	}
	return true
}

// recordDelivered 将已交付消息并入缓存（按内容键去重），返回是否为新消息；
// 重复投递不会产生第二条已完成的投递。
func (s *Service) recordDelivered(message backend.SMSMessage) bool {
	key := smsCacheKey(message)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.cache {
		if smsCacheKey(item) == key {
			return false
		}
	}
	s.cache = append(s.cache, message)
	sortMessages(s.cache)
	if len(s.cache) > 500 {
		s.cache = s.cache[:500]
	}
	return true
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
	})
}
