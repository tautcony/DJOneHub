package notify

import (
	"fmt"
	"net/url"
	"strings"
)

// DefaultWebhookTextTemplate 是 Webhook 文本占位符的默认渲染模板。
const DefaultWebhookTextTemplate = "{{device_label}} {{event}}\n{{text}}"

// Settings 是所有远程通知渠道的持久化配置。它存放在 SQLite 的
// notification_channels 命名空间里（与 notification_preferences 同构），
// 因此这里使用 json tag 而不是 mapstructure。
type Settings struct {
	Telegram TelegramSettings `json:"telegram"`
	Feishu   FeishuSettings   `json:"feishu"`
	Webhook  WebhookSettings  `json:"webhook"`
	Bark     BarkSettings     `json:"bark"`
	Email    EmailSettings    `json:"email"`
	Pushplus PushplusSettings `json:"pushplus"`
}

type TelegramSettings struct {
	Enabled  bool   `json:"enabled"`
	BotToken string `json:"bot_token"`
	ChatID   int64  `json:"chat_id"`
	// AdminID 限制只有该用户发来的命令会被执行；0 表示不限制。
	AdminID int64 `json:"admin_id"`
	// BaseURL 是反向代理地址，例如 https://api.telegram.org/bot%s/%s。
	BaseURL string `json:"base_url"`
	// Proxy 是 HTTP 代理地址，例如 http://127.0.0.1:7890。
	Proxy string `json:"proxy"`
}

type FeishuSettings struct {
	Enabled   bool     `json:"enabled"`
	AppID     string   `json:"app_id"`
	AppSecret string   `json:"app_secret"`
	ChatIDs   []string `json:"chat_ids"`
}

type WebhookSettings struct {
	Enabled      bool              `json:"enabled"`
	URLs         []string          `json:"urls"`
	Secret       string            `json:"secret"`
	TimeoutMs    int               `json:"timeout_ms"`
	RetryMax     int               `json:"retry_max"`
	TextTemplate string            `json:"text_template"`
	Headers      map[string]string `json:"headers,omitempty"`
}

type BarkSettings struct {
	Enabled bool     `json:"enabled"`
	URLs    []string `json:"urls"`
	Group   string   `json:"group"`
	Icon    string   `json:"icon"`
	Level   string   `json:"level"`
}

type EmailSettings struct {
	Enabled     bool     `json:"enabled"`
	UseSSL      bool     `json:"use_ssl"`
	SMTPHost    string   `json:"smtp_host"`
	SMTPPort    int      `json:"smtp_port"`
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	FromAddress string   `json:"from_address"`
	ToAddresses []string `json:"to_addresses"`
}

type PushplusSettings struct {
	Enabled bool   `json:"enabled"`
	Token   string `json:"token"`
	Topic   string `json:"topic"`
	Channel string `json:"channel"`
}

// DefaultSettings 返回全部渠道关闭的初始配置。
func DefaultSettings() Settings {
	return Settings{
		Webhook:  WebhookSettings{TimeoutMs: 5000, RetryMax: 3, TextTemplate: DefaultWebhookTextTemplate},
		Bark:     BarkSettings{Group: "DJOneHub", Level: "active"},
		Pushplus: PushplusSettings{Channel: "wechat"},
	}
}

// Normalize 填充缺省值并清洗空白项，使配置可以直接交给渠道构造函数。
func (s Settings) Normalize() Settings {
	defaults := DefaultSettings()

	s.Telegram.BotToken = strings.TrimSpace(s.Telegram.BotToken)
	s.Telegram.BaseURL = strings.TrimSpace(s.Telegram.BaseURL)
	s.Telegram.Proxy = strings.TrimSpace(s.Telegram.Proxy)

	s.Feishu.AppID = strings.TrimSpace(s.Feishu.AppID)
	s.Feishu.AppSecret = strings.TrimSpace(s.Feishu.AppSecret)
	s.Feishu.ChatIDs = trimmedNonEmpty(s.Feishu.ChatIDs)

	s.Webhook.URLs = trimmedNonEmpty(s.Webhook.URLs)
	s.Webhook.Secret = strings.TrimSpace(s.Webhook.Secret)
	if s.Webhook.TimeoutMs <= 0 {
		s.Webhook.TimeoutMs = defaults.Webhook.TimeoutMs
	}
	if s.Webhook.RetryMax < 0 {
		s.Webhook.RetryMax = 0
	}
	if strings.TrimSpace(s.Webhook.TextTemplate) == "" {
		s.Webhook.TextTemplate = defaults.Webhook.TextTemplate
	}

	s.Bark.URLs = trimmedNonEmpty(s.Bark.URLs)
	s.Bark.Group = strings.TrimSpace(s.Bark.Group)
	s.Bark.Icon = strings.TrimSpace(s.Bark.Icon)
	s.Bark.Level = strings.TrimSpace(s.Bark.Level)

	s.Email.SMTPHost = strings.TrimSpace(s.Email.SMTPHost)
	s.Email.Username = strings.TrimSpace(s.Email.Username)
	s.Email.FromAddress = strings.TrimSpace(s.Email.FromAddress)
	s.Email.ToAddresses = trimmedNonEmpty(s.Email.ToAddresses)

	s.Pushplus.Token = strings.TrimSpace(s.Pushplus.Token)
	s.Pushplus.Topic = strings.TrimSpace(s.Pushplus.Topic)
	s.Pushplus.Channel = strings.TrimSpace(s.Pushplus.Channel)
	if s.Pushplus.Channel == "" {
		s.Pushplus.Channel = defaults.Pushplus.Channel
	}

	return s
}

// Validate 只校验已启用的渠道：关闭的渠道允许保留半填写的草稿配置。
func (s Settings) Validate() error {
	if s.Telegram.Enabled {
		if s.Telegram.BotToken == "" {
			return fmt.Errorf("telegram: bot_token 不能为空")
		}
		if s.Telegram.ChatID == 0 {
			return fmt.Errorf("telegram: chat_id 不能为空")
		}
		if err := validateHTTPURL("telegram", s.Telegram.Proxy, true); err != nil {
			return err
		}
	}
	if s.Feishu.Enabled {
		if s.Feishu.AppID == "" || s.Feishu.AppSecret == "" {
			return fmt.Errorf("feishu: app_id 与 app_secret 不能为空")
		}
		if len(s.Feishu.ChatIDs) == 0 {
			return fmt.Errorf("feishu: chat_ids 至少需要一项")
		}
	}
	if s.Webhook.Enabled {
		if len(s.Webhook.URLs) == 0 {
			return fmt.Errorf("webhook: urls 至少需要一项")
		}
		for _, raw := range s.Webhook.URLs {
			if err := validateHTTPURL("webhook", raw, false); err != nil {
				return err
			}
		}
	}
	if s.Bark.Enabled {
		if len(s.Bark.URLs) == 0 {
			return fmt.Errorf("bark: urls 至少需要一项")
		}
		for _, raw := range s.Bark.URLs {
			if err := validateHTTPURL("bark", raw, false); err != nil {
				return err
			}
		}
	}
	if s.Email.Enabled {
		if s.Email.SMTPHost == "" {
			return fmt.Errorf("email: smtp_host 不能为空")
		}
		if s.Email.SMTPPort <= 0 || s.Email.SMTPPort > 65535 {
			return fmt.Errorf("email: smtp_port 必须在 1-65535 之间")
		}
		if s.Email.FromAddress == "" {
			return fmt.Errorf("email: from_address 不能为空")
		}
		if err := validateEmailAddress("from_address", s.Email.FromAddress); err != nil {
			return err
		}
		if len(s.Email.ToAddresses) == 0 {
			return fmt.Errorf("email: to_addresses 至少需要一项")
		}
		for _, address := range s.Email.ToAddresses {
			if err := validateEmailAddress("to_addresses", address); err != nil {
				return err
			}
		}
	}
	if s.Pushplus.Enabled && s.Pushplus.Token == "" {
		return fmt.Errorf("pushplus: token 不能为空")
	}
	return nil
}

// Redacted 返回一份把机密字段替换为占位符的副本，用于 GET 接口回显。
func (s Settings) Redacted() Settings {
	s.Telegram.BotToken = redact(s.Telegram.BotToken)
	s.Feishu.AppSecret = redact(s.Feishu.AppSecret)
	s.Webhook.Secret = redact(s.Webhook.Secret)
	s.Email.Password = redact(s.Email.Password)
	s.Pushplus.Token = redact(s.Pushplus.Token)
	return s
}

// SecretPlaceholder 是回显给前端的机密占位符。前端原样回传时表示"不修改"。
const SecretPlaceholder = "__unchanged__"

func redact(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return SecretPlaceholder
}

// MergeSecrets 把 incoming 中仍是占位符的机密字段还原为 current 的真实值，
// 这样前端提交表单时无需重新输入密钥。
func MergeSecrets(incoming, current Settings) Settings {
	if incoming.Telegram.BotToken == SecretPlaceholder {
		incoming.Telegram.BotToken = current.Telegram.BotToken
	}
	if incoming.Feishu.AppSecret == SecretPlaceholder {
		incoming.Feishu.AppSecret = current.Feishu.AppSecret
	}
	if incoming.Webhook.Secret == SecretPlaceholder {
		incoming.Webhook.Secret = current.Webhook.Secret
	}
	if incoming.Email.Password == SecretPlaceholder {
		incoming.Email.Password = current.Email.Password
	}
	if incoming.Pushplus.Token == SecretPlaceholder {
		incoming.Pushplus.Token = current.Pushplus.Token
	}
	return incoming
}

func validateHTTPURL(channel, raw string, allowEmpty bool) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if allowEmpty {
			return nil
		}
		return fmt.Errorf("%s: url 不能为空", channel)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s: 无效的 url %q: %w", channel, raw, err)
	}
	switch parsed.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return fmt.Errorf("%s: 不支持的 url 协议 %q", channel, parsed.Scheme)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%s: url %q 缺少主机名", channel, raw)
	}
	return nil
}

func trimmedNonEmpty(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, item := range in {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
