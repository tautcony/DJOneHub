package notify

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/application/notification"
)

// captureChannel 记录所有投递给它的 Message，用于验证 Sink 扇出。
type captureChannel struct {
	mu     sync.Mutex
	msgs   []Message
	err    error
	closed bool
}

var _ Channel = (*captureChannel)(nil)

func (c *captureChannel) Name() string { return "capture" }

func (c *captureChannel) Send(_ context.Context, msg Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, msg)
	return c.err
}

func (c *captureChannel) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *captureChannel) all() []Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Message(nil), c.msgs...)
}

func (c *captureChannel) last() (Message, bool) {
	msgs := c.all()
	if len(msgs) == 0 {
		return Message{}, false
	}
	return msgs[len(msgs)-1], true
}

// waitMessage 等到 capture 至少收到 want 条消息。Broadcast 是异步的。
func waitMessage(t *testing.T, capture *captureChannel, want int) []Message {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if msgs := capture.all(); len(msgs) >= want {
			return msgs
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("超时：期望 %d 条消息，实际 %d 条", want, len(capture.all()))
	return nil
}

func newTestManager(capture *captureChannel) *Manager {
	return &Manager{
		channels: []Channel{capture},
		ports:    Ports{DeviceLabel: func() string { return "客厅主卡" }},
	}
}

func TestManagerImplementsSink(t *testing.T) {
	var sink notification.Sink = &Manager{}
	if sink == nil {
		t.Fatal("Manager 必须实现 notification.Sink")
	}
}

func TestManagerShowSMSBroadcasts(t *testing.T) {
	capture := &captureChannel{}
	m := newTestManager(capture)

	at := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	m.ShowSMS(notification.SMSMessageEvent{
		Sender:     "+8613800000000",
		Body:       "hello",
		ReceivedAt: at,
	})

	msgs := waitMessage(t, capture, 1)
	msg := msgs[0]
	if msg.Event != notification.EventSMSReceived {
		t.Fatalf("event=%q, want %q", msg.Event, notification.EventSMSReceived)
	}
	if msg.DeviceLabel != "客厅主卡" {
		t.Fatalf("device_label=%q，应由 Ports.DeviceLabel 填充", msg.DeviceLabel)
	}
	want := "收到新短信\n号码  +8613800000000\n时间  2026-04-13 12:00:00\n内容  hello"
	if got := msg.Text(); got != want {
		t.Fatalf("text=%q, want %q", got, want)
	}
}

func TestManagerShowSMSFallsBackToRecordedAt(t *testing.T) {
	capture := &captureChannel{}
	m := newTestManager(capture)

	recorded := time.Date(2026, 4, 13, 8, 30, 0, 0, time.UTC)
	m.ShowSMS(notification.SMSMessageEvent{Sender: "10086", Body: "余额提醒", RecordedAt: recorded})

	msgs := waitMessage(t, capture, 1)
	if !msgs[0].Timestamp.Equal(recorded) {
		t.Fatalf("timestamp=%v, want %v", msgs[0].Timestamp, recorded)
	}
}

func TestManagerShowCallAndMissedCall(t *testing.T) {
	capture := &captureChannel{}
	m := newTestManager(capture)

	started := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	ended := started.Add(30 * time.Second)
	m.ShowCall(notification.CallEvent{Number: "10086", StartedAt: started})
	waitMessage(t, capture, 1)
	m.ShowMissedCall(notification.CallEvent{Number: "10010", StartedAt: started, EndedAt: &ended})

	msgs := waitMessage(t, capture, 2)
	byEvent := map[string]Message{}
	for _, msg := range msgs {
		byEvent[msg.Event] = msg
	}

	incoming, ok := byEvent[notification.EventCallIncoming]
	if !ok {
		t.Fatal("缺少来电消息")
	}
	if want := "来电\n号码  10086\n时间  2026-04-13 12:00:00"; incoming.Text() != want {
		t.Fatalf("来电 text=%q, want %q", incoming.Text(), want)
	}

	missed, ok := byEvent[notification.EventCallMissed]
	if !ok {
		t.Fatal("缺少未接来电消息")
	}
	// 未接来电以结束时间为准。
	if !missed.Timestamp.Equal(ended) {
		t.Fatalf("未接来电 timestamp=%v, want %v", missed.Timestamp, ended)
	}
}

func TestManagerShowOfflineFallsBackToLastError(t *testing.T) {
	capture := &captureChannel{}
	m := newTestManager(capture)

	m.ShowOffline(notification.DeviceOfflineEvent{State: "disconnected", LastError: "usb 断开"})

	msgs := waitMessage(t, capture, 1)
	if want := "设备离线\n状态  disconnected\n原因  usb 断开"; msgs[0].Text() != want {
		t.Fatalf("text=%q, want %q", msgs[0].Text(), want)
	}
}

// 菜单栏模型类更新不应外发，否则远程渠道会被高频事件刷屏。
func TestManagerHighFrequencyUpdatesAreNotBroadcast(t *testing.T) {
	capture := &captureChannel{}
	m := newTestManager(capture)

	m.UpdateNetwork(notification.NetworkUpdateEvent{Mode: "5G", Registered: true})
	m.UpdateCall(notification.CallEvent{Number: "10086"})
	m.HideCall(notification.CallEvent{Number: "10086"})

	time.Sleep(50 * time.Millisecond)
	if msgs := capture.all(); len(msgs) != 0 {
		t.Fatalf("高频更新不应外发，实际收到 %d 条", len(msgs))
	}
}

// 单个渠道失败不能影响其他渠道。
func TestManagerBroadcastIsolatesChannelFailure(t *testing.T) {
	failing := &captureChannel{err: errors.New("boom")}
	ok := &captureChannel{}
	m := &Manager{channels: []Channel{failing, ok}}

	m.Broadcast(Message{Event: "test", Title: "标题"})

	waitMessage(t, ok, 1)
	waitMessage(t, failing, 1)
}

func TestManagerBroadcastSkipsEmptyMessage(t *testing.T) {
	capture := &captureChannel{}
	m := newTestManager(capture)

	m.Broadcast(Message{Event: "test"})

	time.Sleep(50 * time.Millisecond)
	if msgs := capture.all(); len(msgs) != 0 {
		t.Fatalf("空消息不应投递，实际收到 %d 条", len(msgs))
	}
}

func TestManagerTestChannelUnknownName(t *testing.T) {
	m := newTestManager(&captureChannel{})
	if err := m.TestChannel(context.Background(), "telegram", Settings{}); err == nil {
		t.Fatal("未启用的渠道应返回错误")
	}
}

func TestManagerTestChannelSendsProbe(t *testing.T) {
	capture := &captureChannel{}
	m := newTestManager(capture)

	if err := m.TestChannel(context.Background(), "capture", Settings{}); err != nil {
		t.Fatalf("TestChannel() error = %v", err)
	}
	msg, ok := capture.last()
	if !ok {
		t.Fatal("未收到测试消息")
	}
	if msg.Event != "notification.test" {
		t.Fatalf("event=%q", msg.Event)
	}
	if msg.DeviceLabel != "客厅主卡" {
		t.Fatalf("device_label=%q", msg.DeviceLabel)
	}
}

func TestManagerStopClosesChannels(t *testing.T) {
	capture := &captureChannel{}
	m := newTestManager(capture)

	m.Stop()

	capture.mu.Lock()
	defer capture.mu.Unlock()
	if !capture.closed {
		t.Fatal("Stop() 应关闭所有渠道")
	}
}

// 所有渠道均关闭时不应构建出任何渠道。
func TestManagerApplyDisabledSettings(t *testing.T) {
	m := NewManager(DefaultSettings(), Ports{})
	if names := m.ChannelNames(); len(names) != 0 {
		t.Fatalf("默认配置不应启用任何渠道，实际: %v", names)
	}
}

// 单个渠道配置非法只跳过它自己，其余渠道照常构建。
func TestManagerApplySkipsInvalidChannel(t *testing.T) {
	settings := DefaultSettings()
	settings.Webhook.Enabled = true
	settings.Webhook.URLs = []string{"https://example.com/hook"}
	settings.Telegram.Enabled = true // 缺 bot_token，构建必然失败

	m := NewManager(settings, Ports{})
	defer m.Stop()

	names := m.ChannelNames()
	if len(names) != 1 || names[0] != "webhook" {
		t.Fatalf("channels=%v, want [webhook]", names)
	}
}

func TestManagerRecoversTransientChannelInitializationFailure(t *testing.T) {
	var attempts atomic.Int32
	capture := &captureChannel{}
	m := &Manager{
		failures: make(map[string]ChannelRecovery), retryBase: 5 * time.Millisecond,
		retrySweep: 2 * time.Millisecond,
		builderFactory: func(Settings) []channelBuilder {
			return []channelBuilder{{name: "capture", enabled: true, build: func(context.Context) (Channel, error) {
				if attempts.Add(1) == 1 {
					return nil, errors.New("temporary EOF")
				}
				return capture, nil
			}}}
		},
	}
	m.apply(DefaultSettings())
	if recovery := m.Diagnostics().Recovering; len(recovery) != 1 || !recovery[0].Retryable {
		t.Fatalf("initial recovery = %#v", recovery)
	}
	m.Start(context.Background())
	defer m.Stop()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if names := m.ChannelNames(); len(names) == 1 && names[0] == "capture" {
			if len(m.Diagnostics().Recovering) != 0 {
				t.Fatal("recovery state remained after successful rebuild")
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("channel did not recover; attempts=%d", attempts.Load())
}

func TestManagerDoesNotRetryPermanentChannelConfigurationFailure(t *testing.T) {
	var attempts atomic.Int32
	m := &Manager{
		failures: make(map[string]ChannelRecovery), retryBase: 2 * time.Millisecond,
		retrySweep: time.Millisecond,
		builderFactory: func(Settings) []channelBuilder {
			return []channelBuilder{{name: "invalid", enabled: true, build: func(context.Context) (Channel, error) {
				attempts.Add(1)
				return nil, errors.New("bot_token is required")
			}}}
		},
	}
	m.apply(DefaultSettings())
	m.Start(context.Background())
	time.Sleep(25 * time.Millisecond)
	m.Stop()
	if attempts.Load() != 1 {
		t.Fatalf("permanent configuration was retried %d times", attempts.Load())
	}
}

type reconnectingChannel struct {
	listenCalls atomic.Int32
}

func (c *reconnectingChannel) Name() string                        { return "reconnecting" }
func (c *reconnectingChannel) Send(context.Context, Message) error { return nil }
func (c *reconnectingChannel) Close() error                        { return nil }
func (c *reconnectingChannel) Listen(ctx context.Context, _ Dispatcher) error {
	if c.listenCalls.Add(1) == 1 {
		return errors.New("connection reset")
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestManagerReconnectsInterruptedCommandListener(t *testing.T) {
	channel := &reconnectingChannel{}
	m := &Manager{
		channels: []Channel{channel}, failures: make(map[string]ChannelRecovery),
		retryBase: 5 * time.Millisecond, retrySweep: time.Second,
	}
	m.Start(context.Background())
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && channel.listenCalls.Load() < 2 {
		time.Sleep(5 * time.Millisecond)
	}
	m.Stop()
	if channel.listenCalls.Load() < 2 {
		t.Fatalf("listener calls = %d, want reconnect", channel.listenCalls.Load())
	}
}

func TestMessageTextSkipsEmptyFields(t *testing.T) {
	msg := Message{
		Title: "设备状态",
		Body:  "正文",
		Fields: []Field{
			{Key: "IMEI", Value: "123"},
			{Key: "ICCID", Value: "   "},
			{Key: "固件", Value: "v1.0"},
		},
	}
	want := "设备状态\n正文\nIMEI  123\n固件  v1.0"
	if got := msg.Text(); got != want {
		t.Fatalf("Text()=%q, want %q", got, want)
	}
}

func TestMessageSubject(t *testing.T) {
	if got := (Message{Title: "收到新短信", DeviceLabel: "客厅主卡"}).Subject(); got != "收到新短信 - 客厅主卡" {
		t.Fatalf("Subject()=%q", got)
	}
	if got := (Message{Title: "收到新短信"}).Subject(); got != "收到新短信" {
		t.Fatalf("Subject()=%q", got)
	}
	if got := (Message{}).Subject(); got != "通知" {
		t.Fatalf("Subject()=%q", got)
	}
}
