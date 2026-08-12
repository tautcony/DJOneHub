package modem

import (
	"testing"
	"time"
)

// TestRDYSubscriptionReArm 验证 RDY 订阅的一次性语义: dispatchRDY 关闭当前
// 所有订阅, 之后重新 SubscribeRDY 仍能收到下一次触发。AT 后端的事件适配器
// (internal/backend/at_backend.go Events) 依赖这个契约在每次收到后重新订阅,
// 否则已关闭的 channel 会让 select 空转成忙循环, 且后续模组重启不再上报。
func TestRDYSubscriptionReArm(t *testing.T) {
	m, err := New(Config{ID: "dev-test", DeviceBackend: "qmi"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	first := m.SubscribeRDY()
	m.dispatchRDY()
	select {
	case <-first:
	case <-time.After(time.Second):
		t.Fatal("first subscription was not closed by dispatchRDY")
	}
	// 重新订阅必须保持打开, 直到下一次 dispatch。
	second := m.SubscribeRDY()
	select {
	case <-second:
		t.Fatal("re-armed subscription must stay open until the next dispatch")
	default:
	}
	m.dispatchRDY()
	select {
	case <-second:
	case <-time.After(time.Second):
		t.Fatal("re-armed subscription was not closed by the second dispatch")
	}
}
