package backend

import (
	"context"
	"time"
)

// NewSMSRef 标识模组存储中一条新收到的短信条目。Storage 为 AT CPMS 存储名
// （如 "SM"）或 QMI 存储类型（"0"/"1"）；模组未上报时为空字符串。
type NewSMSRef struct {
	Storage string
	Index   int
}

// InboundSMSHandler 接收每一条新短信的存储引用。消费者必须先读取并持久化完整的
// 解码消息，再确认（删除）其存储条目；任何失败都必须保留条目供下次刷新重试。
type InboundSMSHandler func(NewSMSRef)

// SMSInboundPort 是 legacy 后端侧的内收短信消费者注册契约。
// 通知来源不是 +CMTI 的后端（QMI/MBIM）通过记录 handler 并走自身轮询路径交付，
// 维持同样的消费者所有权交付契约。
type SMSInboundPort interface {
	SetInboundSMSHandler(InboundSMSHandler)
}

// SMSProvider 短信收发接口
type SMSProvider interface {
	// SendSMS 发送短信
	// AT 实现：AT+CMGS (PDU 模式)
	// QMI 实现：WMS.SendRawMessage
	SendSMS(ctx context.Context, to, body string) error

	// ReadSMS 按存储引用读取并解码指定短信
	// AT 实现：AT+CMGR（必要时先切换 CPMS 存储）
	// QMI 实现：WMS.RawReadMessage
	ReadSMS(ctx context.Context, ref NewSMSRef) (*SMS, error)

	// DeleteSMS 按存储引用删除指定短信
	// AT 实现：AT+CMGD
	// QMI 实现：WMS.DeleteMessage
	DeleteSMS(ctx context.Context, ref NewSMSRef) error

	// ListSMS 列出所有短信概要（真实存储索引、存储身份与解码后的时间戳）
	// AT 实现：AT+CMGL=4
	// QMI 实现：WMS.ListMessages
	ListSMS(ctx context.Context) ([]SMSSummary, error)

	// DeleteAllSMS 删除所有短信
	// AT 实现：AT+CMGD=1,4
	// QMI 实现：WMS.DeleteMessagesByTag（遍历所有 tag）
	DeleteAllSMS(ctx context.Context) error
}

// SMS 短信消息（统一数据结构）
type SMS struct {
	Index      int
	Sender     string
	Content    string
	Timestamp  time.Time
	ConcatRef  int
	PartNumber int
	TotalParts int
}

// SMSSummary 短信列表概要。Index 是模组的真实存储索引，Storage/Tag 保留存储
// 身份，解码出的 ReceivedAt/Sender/Body 供上层合并与去重。
type SMSSummary struct {
	Index      int
	Tag        int // 0=已读, 1=未读, 2=已发送, 3=未发送
	Storage    string
	ReceivedAt time.Time
	Sender     string
	Body       string
	ConcatRef  int
	PartNumber int
	TotalParts int
}
