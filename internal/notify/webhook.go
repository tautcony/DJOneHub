package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vohive/pkg/logger"
)

// webhookPayload 是推送给下游的 JSON 结构。
type webhookPayload struct {
	Event     string             `json:"event"`
	Timestamp string             `json:"timestamp"`
	Text      string             `json:"text"`
	Meta      webhookPayloadMeta `json:"meta"`
}

type webhookPayloadMeta struct {
	DeviceLabel string            `json:"device_label,omitempty"`
	Event       string            `json:"event"`
	Timestamp   string            `json:"timestamp"`
	Title       string            `json:"title,omitempty"`
	Fields      map[string]string `json:"fields,omitempty"`
}

// WebhookChannel 通过 HTTP POST 把通知推送到用户配置的 URL 列表。
// 该渠道只出不进，不实现 CommandReceiver。
type WebhookChannel struct {
	urls         []string
	secret       string
	textTemplate string
	headers      map[string]string
	client       *http.Client
	retryMax     int
}

var _ Channel = (*WebhookChannel)(nil)

// protectedWebhookHeaders 是系统强制设置、不允许被自定义头覆盖的请求头。
var protectedWebhookHeaders = map[string]struct{}{
	"content-type":         {},
	"x-djonehub-signature": {},
}

// 字段名（"号码"、"内容"）本身就是占位符键，因此必须接受非 ASCII 字母。
var webhookPlaceholderPattern = regexp.MustCompile(`\{\{\s*([\p{L}\p{N}_]+)\s*\}\}`)

func NewWebhookChannel(cfg WebhookSettings) (*WebhookChannel, error) {
	if len(cfg.URLs) == 0 {
		return nil, fmt.Errorf("webhook: urls 不能为空")
	}
	timeoutMs := cfg.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 5000
	}
	retryMax := cfg.RetryMax
	if retryMax < 0 {
		retryMax = 0
	}
	return &WebhookChannel{
		urls:         cfg.URLs,
		secret:       cfg.Secret,
		textTemplate: cfg.TextTemplate,
		headers:      sanitizeWebhookHeaders(cfg.Headers),
		retryMax:     retryMax,
		client:       &http.Client{Timeout: time.Duration(timeoutMs) * time.Millisecond},
	}, nil
}

func (w *WebhookChannel) Name() string { return "webhook" }

func (w *WebhookChannel) Send(ctx context.Context, msg Message) error {
	if len(w.urls) == 0 {
		return nil
	}

	text := msg.Text()
	if strings.TrimSpace(w.textTemplate) != "" {
		if rendered := w.renderText(msg); strings.TrimSpace(rendered) != "" {
			text = rendered
		}
	}

	timestamp := msg.Timestamp.Format(time.RFC3339)
	fields := make(map[string]string, len(msg.Fields))
	for _, field := range msg.Fields {
		fields[field.Key] = field.Value
	}
	body, err := json.Marshal(webhookPayload{
		Event:     msg.Event,
		Timestamp: timestamp,
		Text:      text,
		Meta: webhookPayloadMeta{
			DeviceLabel: msg.DeviceLabel,
			Event:       msg.Event,
			Timestamp:   timestamp,
			Title:       msg.Title,
			Fields:      fields,
		},
	})
	if err != nil {
		return fmt.Errorf("序列化 webhook payload 失败: %w", err)
	}

	// 所有 URL 共用同一份 body，签名只算一次。
	signature := w.computeSignature(body)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var lastErr error

	for _, target := range w.urls {
		wg.Add(1)
		go func(targetURL string) {
			defer wg.Done()
			if err := w.postWithRetry(ctx, targetURL, body, signature); err != nil {
				mu.Lock()
				lastErr = fmt.Errorf("%s: %w", targetURL, err)
				mu.Unlock()
				logger.Warn("[notify] webhook 推送失败", "url", targetURL, "err", err)
			}
		}(target)
	}
	wg.Wait()
	return lastErr
}

func (w *WebhookChannel) Close() error {
	if w.client != nil {
		w.client.CloseIdleConnections()
	}
	return nil
}

func (w *WebhookChannel) renderText(msg Message) string {
	values := map[string]string{
		"text":         msg.Text(),
		"title":        strings.TrimSpace(msg.Title),
		"body":         strings.TrimSpace(msg.Body),
		"event":        strings.TrimSpace(msg.Event),
		"timestamp":    msg.Timestamp.Format(time.RFC3339),
		"device_label": strings.TrimSpace(msg.DeviceLabel),
	}
	for _, field := range msg.Fields {
		values[fieldPlaceholderKey(field.Key)] = strings.TrimSpace(field.Value)
	}
	return webhookPlaceholderPattern.ReplaceAllStringFunc(w.textTemplate, func(token string) string {
		matches := webhookPlaceholderPattern.FindStringSubmatch(token)
		if len(matches) != 2 {
			return token
		}
		if value, ok := values[matches[1]]; ok {
			return value
		}
		return token
	})
}

// postWithRetry 对 5xx 和网络错误执行指数退避重试；4xx 直接放弃。
func (w *WebhookChannel) postWithRetry(ctx context.Context, targetURL string, body []byte, signature string) error {
	var lastErr error
	for attempt := 0; attempt <= w.retryMax; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		statusCode, err := w.doPost(ctx, targetURL, body, signature)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastErr = err
			continue
		}
		if statusCode >= 200 && statusCode < 300 {
			return nil
		}
		if statusCode >= 400 && statusCode < 500 {
			return fmt.Errorf("webhook 返回 %d，不重试", statusCode)
		}
		lastErr = fmt.Errorf("webhook 返回 %d", statusCode)
	}
	return fmt.Errorf("webhook 推送失败（已重试 %d 次）: %w", w.retryMax, lastErr)
}

func (w *WebhookChannel) doPost(ctx context.Context, targetURL string, body []byte, signature string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("创建 HTTP 请求失败: %w", err)
	}

	// 先注入自定义头，随后由系统头覆盖（受保护的系统头始终生效）。
	for k, v := range w.headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "DJOneHub-Webhook/1.0")
	}
	if signature != "" {
		req.Header.Set("X-DJOneHub-Signature", signature)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	// 读取并丢弃 body，确保连接可复用。
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

// computeSignature 用 HMAC-SHA256 对请求体签名；secret 为空则不签名。
func (w *WebhookChannel) computeSignature(body []byte) string {
	if w.secret == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(w.secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// sanitizeWebhookHeaders 丢弃空 key 与受保护的系统头。
func sanitizeWebhookHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		if _, protected := protectedWebhookHeaders[strings.ToLower(key)]; protected {
			logger.Warn("[notify] webhook 自定义头与系统头冲突，已忽略", "header", key)
			continue
		}
		out[key] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// fieldPlaceholderKey 把字段名归一化成占位符可用的形式（小写、空白转下划线）。
func fieldPlaceholderKey(key string) string {
	return strings.ToLower(strings.Join(strings.Fields(key), "_"))
}
