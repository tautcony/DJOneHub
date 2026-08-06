package sms

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/application/device"
	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/backend"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/runtime"
	"github.com/iniwex5/vohive/internal/storage"
)

// consumerTestBackend 实现 ModemBackend + SMSPort，用于驱动 SMS 服务的消费者路径。
type consumerTestBackend struct {
	mu       sync.Mutex
	messages map[int]backend.SMSMessage
	deleted  []backend.NewSMSRef
	iccid    string
	handler  backend.InboundSMSHandler
}

func (b *consumerTestBackend) Mode() string { return "fake" }
func (b *consumerTestBackend) Identity(context.Context) (backend.Identity, error) {
	return backend.Identity{IMEI: "123456789012345"}, nil
}
func (b *consumerTestBackend) Radio(context.Context) (backend.RadioState, error) {
	return backend.RadioState{}, nil
}
func (b *consumerTestBackend) SIM(context.Context) (backend.SIMState, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return backend.SIMState{Inserted: true, ICCID: b.iccid}, nil
}
func (b *consumerTestBackend) ListSMS(context.Context) ([]backend.SMSMessage, error) { return nil, nil }
func (b *consumerTestBackend) SendSMS(context.Context, string, string) error         { return nil }
func (b *consumerTestBackend) USSD(context.Context, string) (string, error)          { return "", nil }
func (b *consumerTestBackend) APDU(context.Context, backend.APDURequest) (backend.APDUResponse, error) {
	return backend.APDUResponse{}, nil
}
func (b *consumerTestBackend) Capabilities(context.Context) domain.CapabilitySet {
	return domain.CapabilitySet{domain.CapabilitySMSRead: "", domain.CapabilitySMSSend: ""}
}
func (b *consumerTestBackend) Events(context.Context) (<-chan backend.BackendEvent, error) {
	ch := make(chan backend.BackendEvent)
	close(ch)
	return ch, nil
}
func (b *consumerTestBackend) Close() error { return nil }
func (b *consumerTestBackend) SetInboundSMSHandler(handler backend.InboundSMSHandler) {
	b.mu.Lock()
	b.handler = handler
	b.mu.Unlock()
}
func (b *consumerTestBackend) ReadSMS(ctx context.Context, ref backend.NewSMSRef) (backend.SMSMessage, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	message, ok := b.messages[ref.Index]
	if !ok {
		return backend.SMSMessage{}, context.DeadlineExceeded
	}
	return message, nil
}
func (b *consumerTestBackend) DeleteSMS(ctx context.Context, ref backend.NewSMSRef) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.deleted = append(b.deleted, ref)
	delete(b.messages, ref.Index)
	return nil
}
func (b *consumerTestBackend) DeleteAllSMS(context.Context) error { return nil }

type consumerTestFactory struct{ b *consumerTestBackend }

func (f consumerTestFactory) Open(context.Context, domain.Candidate) (backend.ModemBackend, string, error) {
	return f.b, "fake backend", nil
}

type consumerTestDiscovery struct{}

func (consumerTestDiscovery) Discover(context.Context) ([]domain.Candidate, error) {
	return []domain.Candidate{{Identity: domain.Identity{StableID: "fake-1"}}}, nil
}

func newConsumerTestService(t *testing.T, b *consumerTestBackend) (*Service, *runtime.Runtime) {
	t.Helper()
	r, err := runtime.New(runtime.Config{Discovery: consumerTestDiscovery{}, Backends: consumerTestFactory{b: b}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Rescan(context.Background()); err != nil {
		t.Fatal(err)
	}
	store, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "djonehub.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	devices := device.NewService(r)
	ops := operation.NewManager(r.Events())
	service := NewService(devices, ops, r, store)
	return service, r
}

func inboundMessage(index int, sender, body string, received time.Time, parts ...int) backend.SMSMessage {
	message := backend.SMSMessage{
		Index: index, Sender: sender, Body: body, ReceivedAt: received, Storage: "SM",
		ConcatRef: 77, PartNumber: 1, TotalParts: 1,
	}
	if len(parts) == 3 {
		message.ConcatRef = parts[0]
		message.PartNumber = parts[1]
		message.TotalParts = parts[2]
	}
	return message
}

func TestConsumerRequiresStableSIMIdentity(t *testing.T) {
	backendInstance := &consumerTestBackend{
		messages: map[int]backend.SMSMessage{5: inboundMessage(5, "+100", "hello", time.Now())},
		iccid:    "",
	}
	service, _ := newConsumerTestService(t, backendInstance)
	service.inboundRef(backend.NewSMSRef{Storage: "SM", Index: 5})
	if len(backendInstance.deleted) != 0 {
		t.Fatalf("entry deleted without a SIM identity: %#v", backendInstance.deleted)
	}
	if _, ok := backendInstance.messages[5]; !ok {
		t.Fatal("entry was removed from modem storage despite missing SIM identity")
	}
}

func TestConsumerPersistsThenAcknowledgesSingleMessage(t *testing.T) {
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	backendInstance := &consumerTestBackend{
		messages: map[int]backend.SMSMessage{5: inboundMessage(5, "+100", "hello", now)},
		iccid:    "8901000000000000000",
	}
	service, r := newConsumerTestService(t, backendInstance)

	_, events, unsubscribe := r.Events().Subscribe(8)
	defer unsubscribe()

	service.inboundRef(backend.NewSMSRef{Storage: "SM", Index: 5})

	if len(backendInstance.deleted) != 1 || backendInstance.deleted[0].Index != 5 || backendInstance.deleted[0].Storage != "SM" {
		t.Fatalf("deleted = %#v, want ref {SM 5}", backendInstance.deleted)
	}
	waitForEvent(t, events, "sms.received")
	records, err := service.store.ListSMS("inbound")
	if err != nil || len(records) != 1 {
		t.Fatalf("stored inbound records = %d, err = %v", len(records), err)
	}
	if records[0].Body != "hello" || records[0].ICCID != "8901000000000000000" {
		t.Fatalf("stored record = %+v", records[0])
	}
}

func TestConsumerConflictIgnoredInsertStillAcknowledges(t *testing.T) {
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	backendInstance := &consumerTestBackend{
		messages: map[int]backend.SMSMessage{5: inboundMessage(5, "+100", "hello", now)},
		iccid:    "8901000000000000000",
	}
	service, _ := newConsumerTestService(t, backendInstance)
	// 相同内容已存在于存储：INSERT OR IGNORE 返回 0 行影响，仍视为持久化完成。
	if err := service.store.InsertSMS(storage.SMSRecord{
		Direction: "inbound", ProviderID: 5, Sender: "+100", Body: "hello",
		ReceivedAt: now, RecordedAt: now, ICCID: "8901000000000000000",
	}); err != nil {
		t.Fatal(err)
	}
	service.inboundRef(backend.NewSMSRef{Storage: "SM", Index: 5})
	if len(backendInstance.deleted) != 1 {
		t.Fatalf("deleted = %#v, want the entry acknowledged despite ignored insert", backendInstance.deleted)
	}
}

func TestConsumerFailedPersistenceRetainsEntry(t *testing.T) {
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	backendInstance := &consumerTestBackend{
		messages: map[int]backend.SMSMessage{5: inboundMessage(5, "+100", "hello", now)},
		iccid:    "8901000000000000000",
	}
	service, _ := newConsumerTestService(t, backendInstance)
	// 无 store 的服务无法持久化：条目必须保留。
	service.store = nil
	service.inboundRef(backend.NewSMSRef{Storage: "SM", Index: 5})
	if len(backendInstance.deleted) != 0 {
		t.Fatalf("entry deleted despite failed persistence: %#v", backendInstance.deleted)
	}
}

func TestConsumerFailedReadRetainsEntry(t *testing.T) {
	backendInstance := &consumerTestBackend{
		messages: map[int]backend.SMSMessage{}, // index 9 missing
		iccid:    "8901000000000000000",
	}
	service, _ := newConsumerTestService(t, backendInstance)
	service.inboundRef(backend.NewSMSRef{Storage: "SM", Index: 9})
	if len(backendInstance.deleted) != 0 {
		t.Fatalf("entry deleted despite failed read: %#v", backendInstance.deleted)
	}
}

func TestConsumerMultipartRetainsSegmentsThenAcknowledgesAllRefs(t *testing.T) {
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	backendInstance := &consumerTestBackend{
		messages: map[int]backend.SMSMessage{
			1: inboundMessage(1, "+100", "partA", now, 42, 1, 2),
			2: inboundMessage(2, "+100", "partB", now, 42, 2, 2),
		},
		iccid: "8901000000000000000",
	}
	service, _ := newConsumerTestService(t, backendInstance)

	// 第一个分片到达：不完整，保留在模组存储中。
	service.inboundRef(backend.NewSMSRef{Storage: "SM", Index: 1})
	if len(backendInstance.deleted) != 0 {
		t.Fatalf("segment 1 deleted before completion: %#v", backendInstance.deleted)
	}

	// 重复读取同一分片是幂等的：不产生第二条投递、不重复跟踪。
	service.inboundRef(backend.NewSMSRef{Storage: "SM", Index: 1})

	// 第二个分片到达：完整消息持久化后才确认删除全部组件引用。
	service.inboundRef(backend.NewSMSRef{Storage: "SM", Index: 2})

	if len(backendInstance.deleted) != 2 {
		t.Fatalf("deleted = %#v, want both component refs", backendInstance.deleted)
	}
	indexes := map[int]bool{backendInstance.deleted[0].Index: true, backendInstance.deleted[1].Index: true}
	if !indexes[1] || !indexes[2] {
		t.Fatalf("deleted refs = %#v, want indexes 1 and 2", backendInstance.deleted)
	}
	records, err := service.store.ListSMS("inbound")
	if err != nil || len(records) != 1 {
		t.Fatalf("stored inbound records = %d, err = %v", len(records), err)
	}
	if records[0].Body != "partApartB" {
		t.Fatalf("reassembled body = %q, want partApartB", records[0].Body)
	}
}

func TestRefreshBaselineDoesNotRepublishRetainedMessages(t *testing.T) {
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	backendInstance := &consumerTestBackend{
		messages: map[int]backend.SMSMessage{
			1: inboundMessage(1, "+100", "crash-between-persist-and-ack", now),
		},
		iccid: "8901000000000000000",
	}
	service, r := newConsumerTestService(t, backendInstance)
	// 模拟崩溃前已持久化：先由消费者写入存储，再模拟重启后从模组重读。
	service.inboundRef(backend.NewSMSRef{Storage: "SM", Index: 1})
	delete(backendInstance.messages, 1)

	// 重启：新服务从存储恢复缓存，模组中仍保留同一条目。
	restored := NewService(service.devices, service.ops, r, service.store)
	backendInstance.messages[1] = inboundMessage(1, "+100", "crash-between-persist-and-ack", now)

	_, events, unsubscribe := r.Events().Subscribe(8)
	defer unsubscribe()

	items, err := restored.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// 基线刷新不得把已持久化条目当作新消息发布。
	if event := nextEventOfType(events, "sms.received", 300*time.Millisecond); event != nil {
		t.Fatalf("baseline refresh re-published retained message: %#v", event)
	}
	_ = items
}

// nextEventOfType 从既有订阅通道读取下一个指定类型的已发布事件，超时返回 nil。
func nextEventOfType(events <-chan runtime.Event, eventType string, timeout time.Duration) *runtime.Event {
	deadline := time.After(timeout)
	for {
		select {
		case event := <-events:
			if event.Type == eventType {
				return &event
			}
		case <-deadline:
			return nil
		}
	}
}

// waitForEvent 等待指定类型事件发布，超时则测试失败。
func waitForEvent(t *testing.T, events <-chan runtime.Event, eventType string) {
	t.Helper()
	if event := nextEventOfType(events, eventType, time.Second); event == nil {
		t.Fatalf("%s was not published", eventType)
	}
}

func TestMultipartReassemblesAcrossRefreshCycles(t *testing.T) {
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	backendInstance := &consumerTestBackend{
		messages: map[int]backend.SMSMessage{
			1: inboundMessage(1, "+100", "first", now, 7, 1, 3),
		},
		iccid: "8901000000000000000",
	}
	service, _ := newConsumerTestService(t, backendInstance)
	service.inboundRef(backend.NewSMSRef{Storage: "SM", Index: 1})
	if len(backendInstance.deleted) != 0 {
		t.Fatalf("segment deleted before completion")
	}

	// 模拟一次刷新周期（空列表，但持久化重组器保留状态）。
	if _, err := service.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 第二个刷新周期里剩余分片到达。
	backendInstance.messages[2] = inboundMessage(2, "+100", "second", now, 7, 2, 3)
	service.inboundRef(backend.NewSMSRef{Storage: "SM", Index: 2})
	if len(backendInstance.deleted) != 0 {
		t.Fatalf("message not yet complete, entry must be retained")
	}
	backendInstance.messages[3] = inboundMessage(3, "+100", "third", now, 7, 3, 3)
	service.inboundRef(backend.NewSMSRef{Storage: "SM", Index: 3})

	if len(backendInstance.deleted) != 3 {
		t.Fatalf("deleted = %#v, want all three refs after completion", backendInstance.deleted)
	}
	records, err := service.store.ListSMS("inbound")
	if err != nil || len(records) != 1 || records[0].Body != "firstsecondthird" {
		t.Fatalf("reassembled record = %+v, err = %v", records, err)
	}
}

func TestConsumerUnregistersOnStop(t *testing.T) {
	backendInstance := &consumerTestBackend{iccid: "8901000000000000000"}
	service, _ := newConsumerTestService(t, backendInstance)
	service.Stop()
	backendInstance.mu.Lock()
	handler := backendInstance.handler
	backendInstance.mu.Unlock()
	if handler != nil {
		t.Fatal("consumer was not unregistered on Stop")
	}
}
