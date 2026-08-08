//go:build !(linux && arm)

package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/iniwex5/vohive/pkg/logger"
)

// FeishuChannel 通过飞书开放平台 Bot 推送通知，并用 WebSocket 长连接接收命令。
type FeishuChannel struct {
	client    *lark.Client
	appID     string
	appSecret string
	chatIDs   []string
}

var (
	_ Channel         = (*FeishuChannel)(nil)
	_ CommandReceiver = (*FeishuChannel)(nil)
)

// feishuLogger 把飞书 SDK 的日志转发到项目日志管线。
type feishuLogger struct{}

func (l *feishuLogger) Debug(_ context.Context, args ...any) {
	logger.Debug("[notify][feishu] " + fmt.Sprint(args...))
}
func (l *feishuLogger) Info(_ context.Context, args ...any) {
	logger.Info("[notify][feishu] " + fmt.Sprint(args...))
}
func (l *feishuLogger) Warn(_ context.Context, args ...any) {
	logger.Warn("[notify][feishu] " + fmt.Sprint(args...))
}
func (l *feishuLogger) Error(_ context.Context, args ...any) {
	logger.Error("[notify][feishu] " + fmt.Sprint(args...))
}

func NewFeishuChannel(cfg FeishuSettings) (*FeishuChannel, error) {
	if cfg.AppID == "" || cfg.AppSecret == "" {
		return nil, fmt.Errorf("feishu: app_id 与 app_secret 不能为空")
	}
	if len(cfg.ChatIDs) == 0 {
		return nil, fmt.Errorf("feishu: chat_ids 不能为空")
	}

	client := lark.NewClient(cfg.AppID, cfg.AppSecret,
		lark.WithLogLevel(larkcore.LogLevelInfo),
		lark.WithLogger(&feishuLogger{}),
	)
	logger.Info("[notify] 飞书客户端已创建", "app_id", cfg.AppID)

	return &FeishuChannel{
		client:    client,
		appID:     cfg.AppID,
		appSecret: cfg.AppSecret,
		chatIDs:   dedupe(cfg.ChatIDs),
	}, nil
}

func (f *FeishuChannel) Name() string { return "feishu" }

func (f *FeishuChannel) Send(ctx context.Context, msg Message) error {
	text := msg.Text()
	content, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return fmt.Errorf("序列化飞书消息失败: %w", err)
	}

	var lastErr error
	for _, chatID := range f.chatIDs {
		req := larkim.NewCreateMessageReqBuilder().
			ReceiveIdType(larkim.CreateMessageV1ReceiveIDTypeChatId).
			Body(larkim.NewCreateMessageReqBodyBuilder().
				ReceiveId(chatID).
				MsgType(larkim.MsgTypeText).
				Content(string(content)).
				Build()).
			Build()

		resp, err := f.client.Im.Message.Create(ctx, req)
		if err != nil {
			lastErr = err
			logger.Warn("[notify] 飞书发送失败", "chat_id", chatID, "err", err)
			continue
		}
		if !resp.Success() {
			lastErr = fmt.Errorf("飞书 API 错误 %d: %s", resp.Code, resp.Msg)
			logger.Warn("[notify] 飞书发送失败", "chat_id", chatID, "code", resp.Code, "msg", resp.Msg)
		}
	}
	return lastErr
}

// Listen 建立 WebSocket 长连接接收消息事件，直到 ctx 取消。
func (f *FeishuChannel) Listen(ctx context.Context, dispatch Dispatcher) error {
	handler := dispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
			f.handleMessage(ctx, dispatch, event)
			return nil
		})

	wsClient := larkws.NewClient(f.appID, f.appSecret,
		larkws.WithEventHandler(handler),
		larkws.WithLogLevel(larkcore.LogLevelInfo),
		larkws.WithLogger(&feishuLogger{}),
	)

	logger.Info("[notify] 飞书 WebSocket 命令监听已启动")
	// Start 在 ctx 取消时返回，连接随之关闭。
	return wsClient.Start(ctx)
}

func (f *FeishuChannel) handleMessage(ctx context.Context, dispatch Dispatcher, event *larkim.P2MessageReceiveV1) {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return
	}
	message := event.Event.Message
	if message.MessageType == nil || *message.MessageType != larkim.MsgTypeText {
		return
	}
	if message.Content == nil {
		return
	}

	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(*message.Content), &payload); err != nil {
		return
	}

	var messageID string
	if message.MessageId != nil {
		messageID = *message.MessageId
	}
	reply := func(text string) {
		// 回执不能阻塞事件回调，也不能被命令的 ctx 取消。
		go func() {
			sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sendTimeout)
			defer cancel()
			f.reply(sendCtx, messageID, text)
		}()
	}

	cmd, ok := ParseCommand(payload.Text, reply)
	if !ok {
		return
	}
	logger.Info("[notify] 收到飞书命令", "command", cmd.Name)

	if response := dispatch.Dispatch(ctx, cmd); response != "" {
		reply(response)
	}
}

// reply 优先用 reply API 回到原消息线程，失败时退回群聊直发。
func (f *FeishuChannel) reply(ctx context.Context, messageID, text string) {
	content, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return
	}

	if messageID != "" {
		req := larkim.NewReplyMessageReqBuilder().
			MessageId(messageID).
			Body(larkim.NewReplyMessageReqBodyBuilder().
				MsgType(larkim.MsgTypeText).
				Content(string(content)).
				Build()).
			Build()

		resp, err := f.client.Im.Message.Reply(ctx, req)
		if err == nil && resp.Success() {
			return
		}
		logger.Warn("[notify] 飞书回复失败，改为直接发送", "message_id", messageID)
	}

	if err := f.Send(ctx, Message{Title: text}); err != nil {
		logger.Warn("[notify] 飞书回执发送失败", "err", err)
	}
}

func (f *FeishuChannel) Close() error { return nil }

func dedupe(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
