package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func testMessage() Message {
	return Message{
		Event: "sms.received",
		Title: "收到新短信",
		Fields: []Field{
			{Key: "号码", Value: "+8613800000000"},
			{Key: "内容", Value: "hello"},
		},
		DeviceLabel: "客厅主卡",
		Timestamp:   time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC),
	}
}

func newTestWebhook(t *testing.T, cfg WebhookSettings) *WebhookChannel {
	t.Helper()
	ch, err := NewWebhookChannel(cfg)
	if err != nil {
		t.Fatalf("创建 WebhookChannel 失败: %v", err)
	}
	t.Cleanup(func() { _ = ch.Close() })
	return ch
}

// TestWebhookSignature 验证 HMAC-SHA256 签名的正确性。
func TestWebhookSignature(t *testing.T) {
	const secret = "test-secret-key"
	var receivedSig string
	var receivedBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSig = r.Header.Get("X-DJOneHub-Signature")
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := newTestWebhook(t, WebhookSettings{URLs: []string{srv.URL}, Secret: secret})
	if err := ch.Send(context.Background(), testMessage()); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(receivedBody)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if receivedSig != expected {
		t.Errorf("签名不匹配\n期望: %s\n实际: %s", expected, receivedSig)
	}
}

// TestWebhookNoSignatureWhenSecretEmpty 验证 secret 为空时不携带签名 header。
func TestWebhookNoSignatureWhenSecretEmpty(t *testing.T) {
	var hasSigHeader bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hasSigHeader = r.Header.Get("X-DJOneHub-Signature") != ""
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := newTestWebhook(t, WebhookSettings{URLs: []string{srv.URL}})
	if err := ch.Send(context.Background(), testMessage()); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}

	if hasSigHeader {
		t.Error("secret 为空时不应携带 X-DJOneHub-Signature header")
	}
}

// TestWebhookPayloadFormat 验证 JSON payload 与默认请求头。
func TestWebhookPayloadFormat(t *testing.T) {
	var receivedBody []byte
	var gotCT, gotUA string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		gotUA = r.Header.Get("User-Agent")
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := newTestWebhook(t, WebhookSettings{URLs: []string{srv.URL}})
	msg := testMessage()
	if err := ch.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}

	if gotCT != "application/json" {
		t.Errorf("Content-Type 不正确: %q", gotCT)
	}
	if gotUA != "DJOneHub-Webhook/1.0" {
		t.Errorf("User-Agent 不正确: %q", gotUA)
	}

	var payload webhookPayload
	if err := json.Unmarshal(receivedBody, &payload); err != nil {
		t.Fatalf("解析 payload 失败: %v", err)
	}
	if payload.Event != msg.Event {
		t.Errorf("event 字段错误: %q", payload.Event)
	}
	if payload.Text != msg.Text() {
		t.Errorf("text 字段错误: %q", payload.Text)
	}
	if payload.Timestamp != msg.Timestamp.Format(time.RFC3339) {
		t.Errorf("timestamp 字段错误: %q", payload.Timestamp)
	}
	if payload.Meta.DeviceLabel != "客厅主卡" {
		t.Errorf("meta.device_label 错误: %q", payload.Meta.DeviceLabel)
	}
	if payload.Meta.Title != "收到新短信" {
		t.Errorf("meta.title 错误: %q", payload.Meta.Title)
	}
	if payload.Meta.Fields["号码"] != "+8613800000000" {
		t.Errorf("meta.fields 错误: %v", payload.Meta.Fields)
	}
}

// TestWebhookRetryOn5xx 验证 5xx 错误触发重试。
func TestWebhookRetryOn5xx(t *testing.T) {
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		if callCount.Add(1) <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := newTestWebhook(t, WebhookSettings{URLs: []string{srv.URL}, RetryMax: 3, TimeoutMs: 5000})
	if err := ch.Send(context.Background(), testMessage()); err != nil {
		t.Fatalf("Send 失败（应在第三次成功）: %v", err)
	}
	if count := callCount.Load(); count != 3 {
		t.Errorf("期望请求 3 次，实际 %d 次", count)
	}
}

// TestWebhookNoRetryOn4xx 验证 4xx 错误不重试。
func TestWebhookNoRetryOn4xx(t *testing.T) {
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		callCount.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	ch := newTestWebhook(t, WebhookSettings{URLs: []string{srv.URL}, RetryMax: 3, TimeoutMs: 5000})
	if err := ch.Send(context.Background(), testMessage()); err == nil {
		t.Fatal("期望返回错误")
	}
	if count := callCount.Load(); count != 1 {
		t.Errorf("4xx 不应重试，期望请求 1 次，实际 %d 次", count)
	}
}

// TestWebhookMultiURLParallel 验证多 URL 并行推送。
func TestWebhookMultiURLParallel(t *testing.T) {
	counters := make([]atomic.Int32, 3)
	urls := make([]string, 0, len(counters))
	for i := range counters {
		counter := &counters[i]
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.ReadAll(r.Body)
			counter.Add(1)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		urls = append(urls, srv.URL)
	}

	ch := newTestWebhook(t, WebhookSettings{URLs: urls})
	if err := ch.Send(context.Background(), testMessage()); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}
	for i := range counters {
		if got := counters[i].Load(); got != 1 {
			t.Errorf("URL%d 期望收到 1 次请求，实际 %d", i+1, got)
		}
	}
}

// TestWebhookCustomHeaders 验证用户自定义头被正确注入。
func TestWebhookCustomHeaders(t *testing.T) {
	var gotAuth, gotAPIKey, gotUA string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("X-Api-Key")
		gotUA = r.Header.Get("User-Agent")
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := newTestWebhook(t, WebhookSettings{
		URLs: []string{srv.URL},
		Headers: map[string]string{
			"Authorization": "Bearer token123",
			"X-Api-Key":     "secret-key",
			"User-Agent":    "Custom-Agent/9.9", // 自定义 UA 允许覆盖默认
		},
	})
	if err := ch.Send(context.Background(), testMessage()); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}

	if gotAuth != "Bearer token123" {
		t.Errorf("Authorization 头错误: %q", gotAuth)
	}
	if gotAPIKey != "secret-key" {
		t.Errorf("X-Api-Key 头错误: %q", gotAPIKey)
	}
	if gotUA != "Custom-Agent/9.9" {
		t.Errorf("User-Agent 应可被自定义覆盖，实际: %q", gotUA)
	}
}

// TestWebhookCustomHeadersCannotOverrideProtected 验证受保护的系统头不可被覆盖。
func TestWebhookCustomHeadersCannotOverrideProtected(t *testing.T) {
	const secret = "test-secret-key"
	var gotCT, gotSig string
	var gotBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		gotSig = r.Header.Get("X-DJOneHub-Signature")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := newTestWebhook(t, WebhookSettings{
		URLs:   []string{srv.URL},
		Secret: secret,
		Headers: map[string]string{
			"Content-Type":         "text/plain",    // 尝试覆盖
			"content-type":         "text/xml",      // 大小写变体也应被拦截
			"X-DJOneHub-Signature": "sha256=forged", // 尝试伪造签名
			"":                     "ignored",       // 空 key 应被丢弃
		},
	})
	if err := ch.Send(context.Background(), testMessage()); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}

	if gotCT != "application/json" {
		t.Errorf("Content-Type 不应被覆盖，实际: %q", gotCT)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(gotBody)
	if expected := "sha256=" + hex.EncodeToString(mac.Sum(nil)); gotSig != expected {
		t.Errorf("签名应由系统计算而非被伪造\n期望: %s\n实际: %s", expected, gotSig)
	}
}

func TestWebhookTextTemplateRendersPlaceholders(t *testing.T) {
	var payload webhookPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &payload)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := newTestWebhook(t, WebhookSettings{
		URLs:         []string{srv.URL},
		TextTemplate: "[{{device_label}}] {{title}} 来自 {{号码}}",
	})
	if err := ch.Send(context.Background(), testMessage()); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}

	if want := "[客厅主卡] 收到新短信 来自 +8613800000000"; payload.Text != want {
		t.Fatalf("text=%q, want %q", payload.Text, want)
	}
}

func TestWebhookTemplateEmptyFallsBackToMessageText(t *testing.T) {
	var payload webhookPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &payload)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := newTestWebhook(t, WebhookSettings{URLs: []string{srv.URL}, TextTemplate: ""})
	msg := testMessage()
	if err := ch.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}
	if payload.Text != msg.Text() {
		t.Fatalf("text=%q, want %q", payload.Text, msg.Text())
	}
}

func TestWebhookTemplateKeepsUnknownPlaceholder(t *testing.T) {
	var payload webhookPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &payload)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := newTestWebhook(t, WebhookSettings{
		URLs:         []string{srv.URL},
		TextTemplate: "[{{unknown_key}}] {{title}}",
	})
	if err := ch.Send(context.Background(), testMessage()); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}
	if want := "[{{unknown_key}}] 收到新短信"; payload.Text != want {
		t.Fatalf("text=%q, want %q", payload.Text, want)
	}
}

func TestWebhookSendCancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := newTestWebhook(t, WebhookSettings{URLs: []string{srv.URL}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := ch.Send(ctx, testMessage()); err == nil {
		t.Fatal("ctx 已取消时应返回错误")
	}
}
