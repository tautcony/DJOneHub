//go:build linux && arm

package notify

import (
	"context"
	"errors"
)

// 飞书官方 SDK 的 ws 包会间接编译所有 service 包。其中 drive/v1
// 把 math.MaxInt64 赋给 int，在 32 位平台溢出。上游 v3.9.10 仍未修复：
// https://github.com/larksuite/oapi-sdk-go/issues/162
// https://github.com/larksuite/oapi-sdk-go/pull/200
// 这里提供一个同签名的占位实现，保证该平台仍可构建其余通知渠道。

// FeishuChannel 在 linux/arm 上不可用。
type FeishuChannel struct{}

var (
	_ Channel         = (*FeishuChannel)(nil)
	_ CommandReceiver = (*FeishuChannel)(nil)
)

var errFeishuUnsupported = errors.New("feishu: 当前平台 (linux/arm) 不支持飞书渠道")

func NewFeishuChannel(_ FeishuSettings) (*FeishuChannel, error) {
	return nil, errFeishuUnsupported
}

func (f *FeishuChannel) Name() string { return "feishu" }

func (f *FeishuChannel) Send(_ context.Context, _ Message) error { return errFeishuUnsupported }

func (f *FeishuChannel) Listen(ctx context.Context, _ Dispatcher) error {
	<-ctx.Done()
	return ctx.Err()
}

func (f *FeishuChannel) Close() error { return nil }
