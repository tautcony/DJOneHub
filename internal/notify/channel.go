package notify

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Message 是渠道无关的通知载荷。Manager 把 notification 包的富事件翻译成
// Message，每个渠道再按自己的展示能力渲染：纯文本渠道用 Text()，卡片式渠道
// （Bark 标题、邮件主题）可以单独取 Title 和 Body。
type Message struct {
	// TraceID correlates delivery diagnostics with the originating EventBus
	// event. It is never rendered by a channel.
	TraceID uint64
	// Event 是产生该通知的事件类型，取值为 notification.Event* 常量。
	Event string
	// Title 是单行摘要，例如 "收到新短信"。
	Title string
	// Body 是正文，可以多行。
	Body string
	// Fields 是有序的键值明细，渲染为 "键  值" 的对齐块。
	Fields []Field
	// DeviceLabel 标识来源设备，为空时表示不区分设备。
	DeviceLabel string
	// Timestamp 是事件发生时间。
	Timestamp time.Time
}

// Field 是通知正文里的一行键值明细。
type Field struct {
	Key   string
	Value string
}

// Text 把 Message 渲染成所有纯文本渠道共用的形态。
func (m Message) Text() string {
	var sb strings.Builder
	sb.WriteString(strings.TrimSpace(m.Title))
	if body := strings.TrimSpace(m.Body); body != "" {
		sb.WriteString("\n")
		sb.WriteString(body)
	}
	for _, field := range m.Fields {
		value := strings.TrimSpace(field.Value)
		if value == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("\n%s  %s", strings.TrimSpace(field.Key), value))
	}
	return strings.TrimSpace(sb.String())
}

// Subject 是邮件主题、Bark 标题一类单行场景使用的标题。
func (m Message) Subject() string {
	title := strings.TrimSpace(m.Title)
	if title == "" {
		title = "通知"
	}
	if label := strings.TrimSpace(m.DeviceLabel); label != "" {
		return fmt.Sprintf("%s - %s", title, label)
	}
	return title
}

// Channel 是单个通知渠道的出站接口。实现必须是并发安全的：Manager 会为每条
// 消息、每个渠道各起一个 goroutine。
type Channel interface {
	// Name 返回稳定的渠道标识，例如 "telegram"、"bark"。
	Name() string
	// Send 投递一条消息。ctx 携带超时；实现不应无限阻塞。
	Send(ctx context.Context, msg Message) error
	// Close 释放渠道持有的资源。
	Close() error
}

// CommandReceiver 由支持接收用户命令的渠道额外实现（Telegram、飞书）。只出不
// 进的渠道（Bark、Webhook、邮件、Pushplus）不实现该接口。
type CommandReceiver interface {
	// Listen 阻塞监听命令直到 ctx 取消，收到的命令交给 dispatch 执行。
	Listen(ctx context.Context, dispatch Dispatcher) error
}

// Dispatcher 执行一条已解析的命令并返回给用户的回复文本。
type Dispatcher interface {
	Dispatch(ctx context.Context, cmd Command) string
}

// Command 是从渠道解析出来的一条用户命令。
type Command struct {
	// Name 是不含前导斜杠的命令名，例如 "send"。
	Name string
	// Args 是空白分隔的参数。
	Args []string
	// Reply 把异步结果回送到发起命令的会话。命令处理器可以在返回后继续调用
	// 它来补发结果；实现必须允许被多次调用且并发安全。
	Reply func(text string)
}

// Arg 返回第 index 个参数，越界时返回空串。
func (c Command) Arg(index int) string {
	if index < 0 || index >= len(c.Args) {
		return ""
	}
	return c.Args[index]
}

// Rest 返回从 index 开始的参数拼成的原始文本，用于短信正文一类的尾部参数。
func (c Command) Rest(index int) string {
	if index < 0 || index >= len(c.Args) {
		return ""
	}
	return strings.Join(c.Args[index:], " ")
}

// ParseCommand 解析 "/send 138xxx 正文" 形式的一行文本。ok 为 false 表示该
// 行不是命令。botSuffix 用于剥离群聊里的 "/send@mybot" 写法。
func ParseCommand(line string, reply func(string)) (Command, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "/") {
		return Command{}, false
	}
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return Command{}, false
	}
	name := strings.TrimPrefix(parts[0], "/")
	if at := strings.Index(name, "@"); at >= 0 {
		name = name[:at]
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return Command{}, false
	}
	return Command{Name: name, Args: parts[1:], Reply: reply}, true
}
