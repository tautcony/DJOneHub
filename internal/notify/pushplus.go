package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// pushplusEndpoint 使用 HTTPS：上游用的是明文 HTTP，会把 token 暴露在链路上。
const pushplusEndpoint = "https://www.pushplus.plus/send"

// PushplusChannel 通过 pushplus 推送到微信等渠道。只出不进。
type PushplusChannel struct {
	cfg    PushplusSettings
	client *http.Client
}

var _ Channel = (*PushplusChannel)(nil)

func NewPushplusChannel(cfg PushplusSettings) (*PushplusChannel, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("pushplus: token 不能为空")
	}
	return &PushplusChannel{cfg: cfg, client: &http.Client{Timeout: 10 * time.Second}}, nil
}

func (c *PushplusChannel) Name() string { return "pushplus" }

func (c *PushplusChannel) Send(ctx context.Context, msg Message) error {
	payload := map[string]any{
		"token":    c.cfg.Token,
		"title":    msg.Subject(),
		"content":  msg.Text(),
		"template": "txt",
	}
	if c.cfg.Topic != "" {
		payload["topic"] = c.cfg.Topic
	}
	if c.cfg.Channel != "" {
		payload["channel"] = c.cfg.Channel
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化 pushplus payload 失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pushplusEndpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("HTTP 状态码 %d", resp.StatusCode)
	}

	// pushplus 用 HTTP 200 + body 里的 code 表达业务失败，必须解析 body。
	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&result); err != nil {
		// 解析失败不视为投递失败：HTTP 200 已经说明请求被接受。
		return nil
	}
	if result.Code != 200 {
		return fmt.Errorf("pushplus 返回 code=%d: %s", result.Code, result.Msg)
	}
	return nil
}

func (c *PushplusChannel) Close() error {
	if c.client != nil {
		c.client.CloseIdleConnections()
	}
	return nil
}
