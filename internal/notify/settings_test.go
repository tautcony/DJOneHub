package notify

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSettingsNormalizeFillsDefaults(t *testing.T) {
	t.Parallel()

	got := Settings{
		Webhook: WebhookSettings{URLs: []string{" https://a.example/hook ", "  ", ""}, TimeoutMs: 0, RetryMax: -1},
		Feishu:  FeishuSettings{AppID: "  cli_x  ", ChatIDs: []string{" oc_1 ", ""}},
	}.Normalize()

	if len(got.Webhook.URLs) != 1 || got.Webhook.URLs[0] != "https://a.example/hook" {
		t.Fatalf("webhook.urls=%v", got.Webhook.URLs)
	}
	if got.Webhook.TimeoutMs != 5000 {
		t.Fatalf("webhook.timeout_ms=%d, want 5000", got.Webhook.TimeoutMs)
	}
	if got.Webhook.RetryMax != 0 {
		t.Fatalf("webhook.retry_max=%d, 负值应归零", got.Webhook.RetryMax)
	}
	if got.Webhook.TextTemplate != DefaultWebhookTextTemplate {
		t.Fatalf("webhook.text_template=%q", got.Webhook.TextTemplate)
	}
	if got.Feishu.AppID != "cli_x" {
		t.Fatalf("feishu.app_id=%q", got.Feishu.AppID)
	}
	if len(got.Feishu.ChatIDs) != 1 || got.Feishu.ChatIDs[0] != "oc_1" {
		t.Fatalf("feishu.chat_ids=%v", got.Feishu.ChatIDs)
	}
	if got.Pushplus.Channel != "wechat" {
		t.Fatalf("pushplus.channel=%q, want wechat", got.Pushplus.Channel)
	}
}

// 关闭的渠道允许保留半填写的草稿，不参与校验。
func TestSettingsValidateSkipsDisabledChannels(t *testing.T) {
	t.Parallel()

	s := Settings{
		Telegram: TelegramSettings{Enabled: false, BotToken: ""},
		Email:    EmailSettings{Enabled: false, SMTPHost: ""},
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestSettingsValidateEnabledChannels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		settings Settings
		wantErr  bool
	}{
		{"telegram 缺 token", Settings{Telegram: TelegramSettings{Enabled: true, ChatID: 1}}, true},
		{"telegram 缺 chat_id", Settings{Telegram: TelegramSettings{Enabled: true, BotToken: "t"}}, true},
		{"telegram 合法", Settings{Telegram: TelegramSettings{Enabled: true, BotToken: "t", ChatID: 1}}, false},
		{"telegram 代理非法", Settings{Telegram: TelegramSettings{Enabled: true, BotToken: "t", ChatID: 1, Proxy: "ftp://p"}}, true},
		{"feishu 缺 chat_ids", Settings{Feishu: FeishuSettings{Enabled: true, AppID: "a", AppSecret: "s"}}, true},
		{"webhook 缺 urls", Settings{Webhook: WebhookSettings{Enabled: true}}, true},
		{"webhook 协议非法", Settings{Webhook: WebhookSettings{Enabled: true, URLs: []string{"ftp://a.example"}}}, true},
		{"webhook 缺主机名", Settings{Webhook: WebhookSettings{Enabled: true, URLs: []string{"https://"}}}, true},
		{"bark 合法", Settings{Bark: BarkSettings{Enabled: true, URLs: []string{"https://api.day.app/key"}}}, false},
		{"email 端口越界", Settings{Email: EmailSettings{Enabled: true, SMTPHost: "h", SMTPPort: 70000, FromAddress: "a@b", ToAddresses: []string{"c@d"}}}, true},
		{"email 合法", Settings{Email: EmailSettings{Enabled: true, SMTPHost: "h", SMTPPort: 465, FromAddress: "a@b", ToAddresses: []string{"c@d"}}}, false},
		{"pushplus 缺 token", Settings{Pushplus: PushplusSettings{Enabled: true}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.settings.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestSettingsRedactedMasksSecrets(t *testing.T) {
	t.Parallel()

	s := Settings{
		Telegram: TelegramSettings{BotToken: "real-token", ChatID: 42},
		Feishu:   FeishuSettings{AppID: "cli_x", AppSecret: "real-secret"},
		Webhook:  WebhookSettings{Secret: "real-hmac"},
		Email:    EmailSettings{Password: "real-pass", Username: "u@example.com"},
		Pushplus: PushplusSettings{Token: "real-pushplus"},
	}
	got := s.Redacted()

	for name, value := range map[string]string{
		"telegram.bot_token": got.Telegram.BotToken,
		"feishu.app_secret":  got.Feishu.AppSecret,
		"webhook.secret":     got.Webhook.Secret,
		"email.password":     got.Email.Password,
		"pushplus.token":     got.Pushplus.Token,
	} {
		if value != SecretPlaceholder {
			t.Errorf("%s=%q, want %q", name, value, SecretPlaceholder)
		}
	}

	// 非机密字段必须原样回显，否则设置页会丢数据。
	if got.Telegram.ChatID != 42 || got.Feishu.AppID != "cli_x" || got.Email.Username != "u@example.com" {
		t.Fatalf("非机密字段被误改: %+v", got)
	}

	// 序列化后不得残留任何真实密钥。
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, secret := range []string{"real-token", "real-secret", "real-hmac", "real-pass", "real-pushplus"} {
		if strings.Contains(string(blob), secret) {
			t.Fatalf("脱敏后的 JSON 仍包含 %q: %s", secret, blob)
		}
	}
}

func TestSettingsRedactedKeepsEmptySecretsEmpty(t *testing.T) {
	t.Parallel()

	// 空密钥必须保持为空，否则前端会误以为已配置。
	if got := (Settings{}).Redacted(); got.Telegram.BotToken != "" || got.Webhook.Secret != "" {
		t.Fatalf("空密钥不应被替换为占位符: %+v", got)
	}
}

func TestMergeSecretsRestoresPlaceholders(t *testing.T) {
	t.Parallel()

	current := Settings{
		Telegram: TelegramSettings{BotToken: "old-token"},
		Webhook:  WebhookSettings{Secret: "old-hmac"},
		Email:    EmailSettings{Password: "old-pass"},
	}
	incoming := Settings{
		Telegram: TelegramSettings{BotToken: SecretPlaceholder},
		Webhook:  WebhookSettings{Secret: "new-hmac"}, // 用户确实改了
		Email:    EmailSettings{Password: SecretPlaceholder},
	}

	got := MergeSecrets(incoming, current)
	if got.Telegram.BotToken != "old-token" {
		t.Fatalf("telegram.bot_token=%q, 占位符应还原为旧值", got.Telegram.BotToken)
	}
	if got.Webhook.Secret != "new-hmac" {
		t.Fatalf("webhook.secret=%q, 显式新值不应被覆盖", got.Webhook.Secret)
	}
	if got.Email.Password != "old-pass" {
		t.Fatalf("email.password=%q", got.Email.Password)
	}
}

// 用户清空密钥（提交空串）时必须真正清空，而不是被还原成旧值。
func TestMergeSecretsAllowsClearing(t *testing.T) {
	t.Parallel()

	got := MergeSecrets(
		Settings{Telegram: TelegramSettings{BotToken: ""}},
		Settings{Telegram: TelegramSettings{BotToken: "old-token"}},
	)
	if got.Telegram.BotToken != "" {
		t.Fatalf("telegram.bot_token=%q, 空串应表示清空", got.Telegram.BotToken)
	}
}

func TestSettingsJSONRoundTrip(t *testing.T) {
	t.Parallel()

	want := DefaultSettings()
	want.Bark.Enabled = true
	want.Bark.URLs = []string{"https://api.day.app/key"}

	blob, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Settings
	if err := json.Unmarshal(blob, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Bark.URLs[0] != want.Bark.URLs[0] || !got.Bark.Enabled || got.Webhook.TextTemplate != want.Webhook.TextTemplate {
		t.Fatalf("round trip 丢失字段: %+v", got)
	}
}
