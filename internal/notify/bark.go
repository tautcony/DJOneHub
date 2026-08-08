package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vohive/internal/application/notification"
	"github.com/iniwex5/vohive/pkg/logger"
)

// BarkChannel 通过 Bark 的 HTTP 接口推送到 iOS 设备。只出不进。
type BarkChannel struct {
	urls   []string
	group  string
	icon   string
	level  string
	client *http.Client
}

var _ Channel = (*BarkChannel)(nil)

type barkPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Group string `json:"group,omitempty"`
	Icon  string `json:"icon,omitempty"`
	Level string `json:"level,omitempty"`
}

func NewBarkChannel(cfg BarkSettings) (*BarkChannel, error) {
	if len(cfg.URLs) == 0 {
		return nil, fmt.Errorf("bark: urls 不能为空")
	}
	return &BarkChannel{
		urls:   cfg.URLs,
		group:  cfg.Group,
		icon:   cfg.Icon,
		level:  cfg.Level,
		client: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (b *BarkChannel) Name() string { return "bark" }

func (b *BarkChannel) Send(ctx context.Context, msg Message) error {
	if len(b.urls) == 0 {
		return nil
	}

	body, err := json.Marshal(barkPayload{
		Title: barkTitle(msg),
		Body:  msg.Text(),
		Group: b.group,
		Icon:  b.icon,
		Level: b.level,
	})
	if err != nil {
		return fmt.Errorf("序列化 bark payload 失败: %w", err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var lastErr error

	for _, target := range b.urls {
		wg.Add(1)
		go func(targetURL string) {
			defer wg.Done()
			if err := b.post(ctx, targetURL, body); err != nil {
				mu.Lock()
				lastErr = fmt.Errorf("%s: %w", targetURL, err)
				mu.Unlock()
				logger.Warn("[notify] bark 推送失败", "url", targetURL, "err", err)
			}
		}(target)
	}
	wg.Wait()
	return lastErr
}

func (b *BarkChannel) post(ctx context.Context, targetURL string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP 状态码 %d", resp.StatusCode)
	}
	return nil
}

func (b *BarkChannel) Close() error {
	if b.client != nil {
		b.client.CloseIdleConnections()
	}
	return nil
}

// barkTitle 给标题加上事件图标，方便在通知中心一眼区分。
func barkTitle(msg Message) string {
	title := msg.Subject()
	switch msg.Event {
	case notification.EventSMSReceived:
		return "💬 " + title
	case notification.EventCallIncoming:
		return "📞 " + title
	case notification.EventCallMissed:
		return "📵 " + title
	case notification.EventDeviceOffline:
		return "⚠️ " + title
	}
	if strings.TrimSpace(title) == "" {
		return "DJOneHub"
	}
	return title
}
