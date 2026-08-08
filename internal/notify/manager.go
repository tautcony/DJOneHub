package notify

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vohive/internal/application/notification"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/pkg/logger"
)

// sendTimeout 限制单个渠道投递一条消息的时间。
const sendTimeout = 30 * time.Second

// Manager 持有所有已启用的远程通知渠道，并实现 notification.Sink，因此它与
// macOS 原生通知在 notification.Service 里并列：同一条事件会同时投给 Swift
// 和用户勾选的每个远程渠道。
//
// Manager 只处理"需要打扰用户"的事件（来电、未接来电、短信、设备离线）。
// 菜单栏模型类的高频更新（设备状态、网络状态）不外发，避免刷屏。
type Manager struct {
	ports Ports

	mu       sync.RWMutex
	settings Settings
	channels []Channel
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// ChannelConfigError indicates that a requested channel cannot be constructed
// from the supplied settings. Callers can map it to an invalid-request status.
type ChannelConfigError struct {
	Channel string
	Err     error
}

func (e *ChannelConfigError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("渠道 %q 配置无效", e.Channel)
	}
	return fmt.Sprintf("渠道 %q 配置无效: %v", e.Channel, e.Err)
}

func (e *ChannelConfigError) Unwrap() error { return e.Err }

// 编译期断言：Manager 必须满足 notification.Sink。
var _ notification.Sink = (*Manager)(nil)

// NewManager 按 settings 构建所有已启用的渠道。构建失败的单个渠道只记录日志
// 并跳过，不影响其余渠道，避免一处配置错误导致整个通知子系统不可用。
func NewManager(settings Settings, ports Ports) *Manager {
	m := &Manager{ports: ports}
	m.apply(settings)
	return m
}

// Start 启动支持命令的渠道的监听循环。不支持命令的渠道无需启动。
func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	m.cancel = cancel
	channels := append([]Channel(nil), m.channels...)
	m.mu.Unlock()

	m.startListeners(runCtx, channels)
}

func (m *Manager) startListeners(ctx context.Context, channels []Channel) {
	dispatcher := NewCommands(m.ports)
	for _, channel := range channels {
		receiver, ok := channel.(CommandReceiver)
		if !ok {
			continue
		}
		m.wg.Add(1)
		go func(name string, receiver CommandReceiver) {
			defer m.wg.Done()
			if err := receiver.Listen(ctx, dispatcher); err != nil && ctx.Err() == nil {
				logger.Warn("[notify] 命令监听退出", "channel", name, "err", err)
			}
		}(channel.Name(), receiver)
	}
}

// Stop 停止命令监听并关闭所有渠道。
func (m *Manager) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	m.cancel = nil
	channels := m.channels
	m.channels = nil
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	m.wg.Wait()
	for _, channel := range channels {
		if err := channel.Close(); err != nil {
			logger.Warn("[notify] 关闭渠道失败", "channel", channel.Name(), "err", err)
		}
	}
}

// UpdateSettings 热更新渠道配置：关闭旧渠道，按新配置重建并重新启动监听。
func (m *Manager) UpdateSettings(ctx context.Context, settings Settings) {
	m.Stop()
	m.apply(settings)

	m.mu.Lock()
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	m.cancel = cancel
	channels := append([]Channel(nil), m.channels...)
	m.mu.Unlock()

	m.startListeners(runCtx, channels)
}

// Settings 返回当前生效的配置。
func (m *Manager) Settings() Settings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.settings
}

// ChannelNames 返回已启用渠道的名称，用于设置页与诊断。
func (m *Manager) ChannelNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.channels))
	for _, channel := range m.channels {
		names = append(names, channel.Name())
	}
	return names
}

// channelBuilder 把一个渠道的名称、启用开关与构造函数绑在一起，供 apply 与
// TestChannel 复用：apply 只在启用时构建，TestChannel 则忽略开关、按需临时构建。
type channelBuilder struct {
	name    string
	enabled bool
	build   func() (Channel, error)
}

// channelBuilders 返回按 settings 构造六个渠道的构建器列表。
func channelBuilders(settings Settings) []channelBuilder {
	return []channelBuilder{
		{"telegram", settings.Telegram.Enabled, func() (Channel, error) { return NewTelegramChannel(settings.Telegram) }},
		{"feishu", settings.Feishu.Enabled, func() (Channel, error) { return NewFeishuChannel(settings.Feishu) }},
		{"webhook", settings.Webhook.Enabled, func() (Channel, error) { return NewWebhookChannel(settings.Webhook) }},
		{"bark", settings.Bark.Enabled, func() (Channel, error) { return NewBarkChannel(settings.Bark) }},
		{"email", settings.Email.Enabled, func() (Channel, error) { return NewEmailChannel(settings.Email) }},
		{"pushplus", settings.Pushplus.Enabled, func() (Channel, error) { return NewPushplusChannel(settings.Pushplus) }},
	}
}

// apply 用 settings 重建渠道列表。调用方必须保证此时没有监听在跑。
func (m *Manager) apply(settings Settings) {
	settings = settings.Normalize()
	channels := make([]Channel, 0, 6)
	for _, b := range channelBuilders(settings) {
		if !b.enabled {
			continue
		}
		channel, err := b.build()
		if err != nil {
			logger.Error("[notify] 初始化渠道失败", "channel", b.name, "err", err)
			continue
		}
		if channel == nil {
			continue
		}
		channels = append(channels, channel)
	}

	m.mu.Lock()
	m.settings = settings
	m.channels = channels
	m.mu.Unlock()

	if len(channels) > 0 {
		logger.Info("[notify] 远程通知渠道已就绪", "channels", strings.Join(namesOf(channels), ","))
	}
}

// Broadcast 把一条消息并行投递给所有已启用渠道。单个渠道失败只记录日志。
func (m *Manager) Broadcast(msg Message) {
	if strings.TrimSpace(msg.Title) == "" && strings.TrimSpace(msg.Body) == "" {
		return
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	if msg.DeviceLabel == "" && m.ports.DeviceLabel != nil {
		msg.DeviceLabel = m.ports.DeviceLabel()
	}

	m.mu.RLock()
	channels := append([]Channel(nil), m.channels...)
	m.mu.RUnlock()
	if len(channels) == 0 {
		return
	}

	for _, channel := range channels {
		go func(channel Channel) {
			ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
			defer cancel()
			if err := channel.Send(ctx, msg); err != nil {
				logger.Warn("[notify] 渠道投递失败", "channel", channel.Name(), "event", msg.Event, "err", err)
			}
		}(channel)
	}
}

// ---------- notification.Sink 实现 ----------

// UpdateDeviceStatus 是菜单栏模型更新，不外发。
func (m *Manager) UpdateDeviceStatus(domain.Snapshot) {}

// UpdateNetwork 是菜单栏模型更新，不外发。
func (m *Manager) UpdateNetwork(notification.NetworkUpdateEvent) {}

// UpdateCall 是通话卡片的中途状态更新，不外发以免刷屏。
func (m *Manager) UpdateCall(notification.CallEvent) {}

// HideCall 用于收起原生通话卡片，远程渠道无对应语义。
func (m *Manager) HideCall(notification.CallEvent) {}

func (m *Manager) ShowCall(call notification.CallEvent) {
	m.Broadcast(Message{
		Event: notification.EventCallIncoming,
		Title: "来电",
		Fields: []Field{
			{Key: "号码", Value: orDash(call.Number)},
			{Key: "时间", Value: formatTime(call.StartedAt)},
		},
		Timestamp: call.StartedAt,
	})
}

func (m *Manager) ShowMissedCall(call notification.CallEvent) {
	at := call.StartedAt
	if call.EndedAt != nil {
		at = *call.EndedAt
	}
	m.Broadcast(Message{
		Event: notification.EventCallMissed,
		Title: "未接来电",
		Fields: []Field{
			{Key: "号码", Value: orDash(call.Number)},
			{Key: "时间", Value: formatTime(at)},
		},
		Timestamp: at,
	})
}

func (m *Manager) ShowSMS(message notification.SMSMessageEvent) {
	at := message.ReceivedAt
	if at.IsZero() {
		at = message.RecordedAt
	}
	m.Broadcast(Message{
		Event: notification.EventSMSReceived,
		Title: "收到新短信",
		Fields: []Field{
			{Key: "号码", Value: orDash(message.Sender)},
			{Key: "时间", Value: formatTime(at)},
			{Key: "内容", Value: strings.TrimSpace(message.Body)},
		},
		Timestamp: at,
	})
}

func (m *Manager) ShowOffline(event notification.DeviceOfflineEvent) {
	reason := strings.TrimSpace(event.Reason)
	if reason == "" {
		reason = strings.TrimSpace(event.LastError)
	}
	m.Broadcast(Message{
		Event: notification.EventDeviceOffline,
		Title: "设备离线",
		Fields: []Field{
			{Key: "状态", Value: orDash(event.State)},
			{Key: "原因", Value: orDash(reason)},
		},
		Timestamp: time.Now(),
	})
}

// TestChannel 向单个渠道发送一条测试消息，供设置页的"测试"按钮使用。
// 与真实投递不同，测试不要求渠道处于启用状态：已启用且正在运行的渠道直接用
// 实例测试；未启用或尚未保存的渠道则按 probe（合并已保存密钥后）临时构建后
// 测试，方便用户在开启前先验证配置是否正确。
func (m *Manager) TestChannel(ctx context.Context, name string, probe Settings) error {
	// 1) 已启用且在运行的渠道：直接用活动实例，避免重复构建/握手。
	m.mu.RLock()
	var live Channel
	for _, channel := range m.channels {
		if channel.Name() == name {
			live = channel
			break
		}
	}
	current := m.settings
	m.mu.RUnlock()
	if live != nil {
		return m.sendTest(ctx, live)
	}

	// 2) 未启用/未保存的渠道：用传入配置（缺失密钥用已保存值还原）临时构建。
	settings := MergeSecrets(probe, current).Normalize()
	var target Channel
	for _, b := range channelBuilders(settings) {
		if b.name != name {
			continue
		}
		channel, err := b.build()
		if err != nil {
			return &ChannelConfigError{Channel: name, Err: err}
		}
		target = channel
		break
	}
	if target == nil {
		return &ChannelConfigError{Channel: name, Err: fmt.Errorf("未知渠道")}
	}
	defer target.Close()
	return m.sendTest(ctx, target)
}

// sendTest 向指定渠道投递一条测试消息。
func (m *Manager) sendTest(ctx context.Context, channel Channel) error {
	label := ""
	if m.ports.DeviceLabel != nil {
		label = m.ports.DeviceLabel()
	}
	return channel.Send(ctx, Message{
		Event:       "notification.test",
		Title:       "DJOneHub 测试通知",
		Body:        "如果你看到这条消息，说明该通知渠道配置正确。",
		DeviceLabel: label,
		Timestamp:   time.Now(),
	})
}

func namesOf(channels []Channel) []string {
	names := make([]string, 0, len(channels))
	for _, channel := range channels {
		names = append(names, channel.Name())
	}
	return names
}
