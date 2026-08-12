package notify

import (
	"strings"
	"testing"
)

func TestSplitRunesDoesNotBreakMultibyte(t *testing.T) {
	t.Parallel()

	text := strings.Repeat("验证码", 2000) // 6000 runes > 4096
	chunks := splitRunes(text, telegramMaxMessageLen)
	if len(chunks) != 2 {
		t.Fatalf("chunks=%d, want 2", len(chunks))
	}
	if strings.Join(chunks, "") != text {
		t.Fatal("拼接后与原文不一致")
	}
	for i, chunk := range chunks {
		if n := len([]rune(chunk)); n > telegramMaxMessageLen {
			t.Fatalf("chunk %d 长度 %d 超过上限", i, n)
		}
	}
}

func TestSplitRunesShortTextReturnsSingleChunk(t *testing.T) {
	t.Parallel()

	chunks := splitRunes("hello", telegramMaxMessageLen)
	if len(chunks) != 1 || chunks[0] != "hello" {
		t.Fatalf("chunks=%v", chunks)
	}
}

func TestSanitizeUTF8DropsInvalidRunes(t *testing.T) {
	t.Parallel()

	// 单独的 0xFF 字节解码为 utf8.RuneError，必须被丢弃。
	if got := sanitizeUTF8("ab\xffc"); got != "abc" {
		t.Fatalf("sanitizeUTF8()=%q, want %q", got, "abc")
	}
}

func TestRedactTokenHidesBotToken(t *testing.T) {
	t.Parallel()

	const token = "123456:AAExampleSecret"
	got := redactToken("Post \"https://api.telegram.org/bot"+token+"/getMe\": timeout", token)
	if strings.Contains(got, token) {
		t.Fatalf("错误信息仍包含 token: %q", got)
	}
	if !strings.Contains(got, "<redacted>") {
		t.Fatalf("未替换为占位符: %q", got)
	}
}

func TestRedactTokenEmptyTokenIsNoop(t *testing.T) {
	t.Parallel()

	if got := redactToken("some error", ""); got != "some error" {
		t.Fatalf("redactToken()=%q", got)
	}
}

func TestRedactTelegramURLMasksTokenWithoutChangingEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		message string
		secret  string
		want    string
	}{
		{
			name:    "official API",
			message: `Post "https://api.telegram.org/bot123456:fake-secret/getUpdates": unexpected EOF`,
			secret:  "fake-secret",
			want:    "/bot<redacted>/getUpdates",
		},
		{
			name:    "custom API",
			message: `Post "https://telegram.example.test/bot654321:other-secret/sendMessage": timeout`,
			secret:  "other-secret",
			want:    "/bot<redacted>/sendMessage",
		},
		{
			name:    "unrelated text",
			message: "Failed to get updates, retrying in 3 seconds...",
			want:    "Failed to get updates, retrying in 3 seconds...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactTelegramURL(tt.message)
			if tt.secret != "" && strings.Contains(got, tt.secret) {
				t.Fatalf("redacted message contains secret: %q", got)
			}
			if !strings.Contains(got, tt.want) {
				t.Fatalf("redacted message = %q, want substring %q", got, tt.want)
			}
		})
	}
}
