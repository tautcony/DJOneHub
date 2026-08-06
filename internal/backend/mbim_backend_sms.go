package backend

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/iniwex5/vohive/pkg/logger"
	"github.com/iniwex5/vohive/pkg/smscodec"
)

func (b *MBIMBackend) SendSMS(ctx context.Context, to, body string) error {
	return b.SendSMSWithOptions(ctx, to, body, smscodec.SubmitOptions{})
}

func (b *MBIMBackend) SendSMSWithOptions(ctx context.Context, to, body string, opts smscodec.SubmitOptions) error {
	tpdus, _, err := smscodec.BuildSubmitTPDUsWithOptions(to, body, opts)
	if err != nil {
		return fmt.Errorf("PDU 编码失败: %w", err)
	}
	if len(tpdus) == 0 {
		return fmt.Errorf("PDU 编码结果为空")
	}
	for i, tpdu := range tpdus {
		pdu := append([]byte{0x00}, tpdu...)
		if _, err := b.source.SendSMS(ctx, pdu); err != nil {
			return fmt.Errorf("发送第 %d/%d 段失败: %w", i+1, len(tpdus), err)
		}
	}
	logger.Info("MBIM 短信发送成功", "to", to, "parts", len(tpdus))
	return nil
}

// SetInboundSMSHandler records the inbound SMS consumer. MBIM has no +CMTI
// notification; inbound delivery runs through the consumer-owned polling path.
func (b *MBIMBackend) SetInboundSMSHandler(handler InboundSMSHandler) {}

// ReadSMS 按存储引用读取并解码短信 PDU。PDU 解析失败返回错误而不删除条目。
func (b *MBIMBackend) ReadSMS(ctx context.Context, ref NewSMSRef) (*SMS, error) {
	rec, err := b.source.ReadSMS(ctx, uint32(ref.Index))
	if err != nil {
		return nil, err
	}
	sender, content, timestamp, concat, err := smscodec.DecodeDeliverPDUHex(hex.EncodeToString(rec.PDU))
	if err != nil {
		return nil, fmt.Errorf("短信 %d PDU 解码失败: %w", ref.Index, err)
	}
	return &SMS{
		Index:      ref.Index,
		Sender:     sender,
		Content:    content,
		Timestamp:  timestamp,
		ConcatRef:  concat.Ref,
		PartNumber: concat.Seq,
		TotalParts: concat.Total,
	}, nil
}

func (b *MBIMBackend) DeleteSMS(ctx context.Context, ref NewSMSRef) error {
	return b.source.DeleteSMS(ctx, uint32(ref.Index))
}

// ListSMS 返回全部短信概要：真实存储索引、状态 tag 与解码出的时间戳/内容。
func (b *MBIMBackend) ListSMS(ctx context.Context) ([]SMSSummary, error) {
	recs, err := b.source.ListSMS(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]SMSSummary, 0, len(recs))
	for _, r := range recs {
		summary := SMSSummary{Index: int(r.Index), Tag: int(r.Status)}
		if sender, content, timestamp, concat, decodeErr := smscodec.DecodeDeliverPDUHex(hex.EncodeToString(r.PDU)); decodeErr == nil {
			summary.ReceivedAt = timestamp
			summary.Sender = sender
			summary.Body = content
			summary.ConcatRef = concat.Ref
			summary.PartNumber = concat.Seq
			summary.TotalParts = concat.Total
		}
		out = append(out, summary)
	}
	return out, nil
}

func (b *MBIMBackend) DeleteAllSMS(ctx context.Context) error {
	return b.source.DeleteAllSMS(ctx)
}

func (b *MBIMBackend) GetSMSC(ctx context.Context) (string, error) {
	return b.source.GetSMSC(ctx)
}

func (b *MBIMBackend) SetSMSC(ctx context.Context, smsc string) error {
	return b.source.SetSMSC(ctx, smsc)
}
