package notify

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/iniwex5/vohive/pkg/logger"
)

// telegramMaxMessageLen 是 Telegram 单条消息的字符上限，超出会被 API 拒绝。
const telegramMaxMessageLen = 4096

// TelegramChannel 通过 Telegram Bot 推送通知并接收命令。
type TelegramChannel struct {
	api      *tgbotapi.BotAPI
	chatID   int64
	adminID  int64
	stopOnce sync.Once
}

// DiscoverTelegramChatIDs reads recent updates and returns distinct chat IDs.
// It is intended for first-time setup, before the command listener is enabled.
func DiscoverTelegramChatIDs(cfg TelegramSettings) ([]int64, error) {
	if cfg.ChatID == 0 {
		cfg.ChatID = 1 // constructor only needs a non-zero placeholder
	}
	channel, err := NewTelegramChannel(cfg)
	if err != nil {
		return nil, err
	}
	defer channel.Close()
	updates, err := channel.api.GetUpdates(tgbotapi.UpdateConfig{Limit: 100, Timeout: 0})
	if err != nil {
		return nil, err
	}
	seen := make(map[int64]struct{})
	ids := make([]int64, 0)
	for _, update := range updates {
		if update.Message == nil || update.Message.Chat == nil {
			continue
		}
		id := update.Message.Chat.ID
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

var (
	_ Channel         = (*TelegramChannel)(nil)
	_ CommandReceiver = (*TelegramChannel)(nil)
)

func NewTelegramChannel(cfg TelegramSettings) (*TelegramChannel, error) {
	return NewTelegramChannelContext(context.Background(), cfg)
}

// NewTelegramChannelContext makes the startup getMe handshake cancellable so
// manager shutdown and settings replacement never wait for a dead network.
func NewTelegramChannelContext(ctx context.Context, cfg TelegramSettings) (*TelegramChannel, error) {
	if cfg.BotToken == "" {
		return nil, fmt.Errorf("telegram: bot_token 不能为空")
	}
	if cfg.ChatID == 0 {
		return nil, fmt.Errorf("telegram: chat_id 不能为空")
	}

	endpoint := tgbotapi.APIEndpoint
	if cfg.BaseURL != "" {
		endpoint = cfg.BaseURL
		if !strings.Contains(endpoint, "bot%s/%s") {
			endpoint = strings.TrimSuffix(endpoint, "/") + "/bot%s/%s"
		}
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.Proxy != "" {
		proxyURL, err := url.Parse(cfg.Proxy)
		if err != nil {
			return nil, fmt.Errorf("telegram: 解析代理地址失败: %w", err)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}

	// 长轮询超时 60s，客户端超时必须显著大于它，否则每轮都会被自己掐断。
	client := &http.Client{Transport: transport, Timeout: 120 * time.Second}
	bot, err := tgbotapi.NewBotAPIWithClient(cfg.BotToken, endpoint, startupHTTPClient{ctx: ctx, client: client})
	if err != nil {
		// 错误信息可能回显 token，必须脱敏后再往上抛。
		return nil, fmt.Errorf("telegram: 创建 bot 失败: %s", redactToken(err.Error(), cfg.BotToken))
	}
	// Only the startup handshake uses the manager context. Normal sends retain
	// the channel client and its own transport timeout for the channel lifetime.
	bot.Client = client

	logger.Info("[notify] telegram 已授权", "username", bot.Self.UserName)
	return &TelegramChannel{api: bot, chatID: cfg.ChatID, adminID: cfg.AdminID}, nil
}

type startupHTTPClient struct {
	ctx    context.Context
	client *http.Client
}

func (c startupHTTPClient) Do(request *http.Request) (*http.Response, error) {
	return c.client.Do(request.WithContext(c.ctx))
}

func (t *TelegramChannel) Name() string { return "telegram" }

func (t *TelegramChannel) Send(ctx context.Context, msg Message) error {
	return t.sendText(ctx, t.chatID, msg.Text())
}

func (t *TelegramChannel) sendText(ctx context.Context, chatID int64, text string) error {
	for _, chunk := range splitRunes(sanitizeUTF8(text), telegramMaxMessageLen) {
		if err := ctx.Err(); err != nil {
			return err
		}
		// 用 HTML 模式但先转义原文，避免短信里的 "<#>" 被当成标签解析。
		message := tgbotapi.NewMessage(chatID, html.EscapeString(chunk))
		message.ParseMode = tgbotapi.ModeHTML
		if _, err := t.api.Send(message); err != nil {
			return err
		}
	}
	return nil
}

// Listen 运行 long-polling 循环直到 ctx 取消。
func (t *TelegramChannel) Listen(ctx context.Context, dispatch Dispatcher) error {
	config := tgbotapi.NewUpdate(0)
	config.Timeout = 60
	updates := t.api.GetUpdatesChan(config)
	logger.Info("[notify] telegram 命令监听已启动")
	for {
		select {
		case <-ctx.Done():
			return nil
		case update, ok := <-updates:
			if !ok {
				return nil
			}
			t.handleUpdate(ctx, dispatch, update)
		}
	}
}

func (t *TelegramChannel) handleUpdate(ctx context.Context, dispatch Dispatcher, update tgbotapi.Update) {
	if update.Message == nil || !update.Message.IsCommand() {
		return
	}
	// 只接受配置的会话；adminID 非零时进一步限制到具体用户。
	if update.Message.Chat.ID != t.chatID {
		return
	}
	if t.adminID != 0 && (update.Message.From == nil || update.Message.From.ID != t.adminID) {
		return
	}

	chatID := update.Message.Chat.ID
	reply := func(text string) {
		// 回执不能阻塞轮询循环，也不能被命令的 ctx 取消。
		go func() {
			sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sendTimeout)
			defer cancel()
			if err := t.sendText(sendCtx, chatID, text); err != nil {
				logger.Warn("[notify] telegram 回执失败", "err", err)
			}
		}()
	}

	cmd := Command{
		Name:  strings.ToLower(update.Message.Command()),
		Args:  strings.Fields(update.Message.CommandArguments()),
		Reply: reply,
	}
	logger.Info("[notify] 收到 telegram 命令", "command", cmd.Name)

	if response := dispatch.Dispatch(ctx, cmd); response != "" {
		reply(response)
	}
}

func (t *TelegramChannel) Close() error {
	t.stopReceiving()
	return nil
}

func (t *TelegramChannel) stopReceiving() {
	if t.api != nil {
		t.stopOnce.Do(t.api.StopReceivingUpdates)
	}
}

// sanitizeUTF8 丢弃非法 UTF-8 字符，否则 Telegram API 会整条拒绝。
func sanitizeUTF8(text string) string {
	return strings.Map(func(r rune) rune {
		if r == utf8.RuneError {
			return -1
		}
		return r
	}, text)
}

// splitRunes 按字符数（非字节数）切分，避免截断多字节字符。
func splitRunes(text string, limit int) []string {
	runes := []rune(text)
	if len(runes) <= limit {
		return []string{text}
	}
	chunks := make([]string, 0, (len(runes)+limit-1)/limit)
	for start := 0; start < len(runes); start += limit {
		end := start + limit
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[start:end]))
	}
	return chunks
}

func redactToken(text, token string) string {
	if token == "" {
		return text
	}
	return strings.ReplaceAll(text, token, "<redacted>")
}
