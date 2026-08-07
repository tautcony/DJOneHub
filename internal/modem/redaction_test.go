package modem

import "testing"

func TestMaskIdentity(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "990000860099326", want: "990********9326"},
		{in: "89860012345678901234", want: "898*************1234"},
		{in: "short", want: "****"},
		{in: "", want: "****"},
		{in: "  ", want: "****"},
	}
	for _, tt := range tests {
		if got := maskIdentity(tt.in); got != tt.want {
			t.Fatalf("maskIdentity(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFilterSensitiveLogFields(t *testing.T) {
	fields := []any{"n", 0, "dcs", 15, "text", "余额查询", "number", "01012345678", "raw", "AT+CLIP: 01012345678", "state", "READY"}

	// 默认关闭: 敏感字段被移除, 其余字段保留。
	LogMessageContent = false
	out := filterSensitiveLogFields(fields)
	joined := flatten(out)
	for _, want := range []string{"n", "dcs", "state", "READY"} {
		if !containsAny(joined, want) {
			t.Fatalf("filtered fields %v missing %q", joined, want)
		}
	}
	for _, banned := range []string{"text", "number", "raw", "余额查询", "01012345678"} {
		if containsAny(joined, banned) {
			t.Fatalf("filtered fields %v still contain %q", joined, banned)
		}
	}

	// 显式开启: 全部字段保留。
	LogMessageContent = true
	out = filterSensitiveLogFields(fields)
	if len(out) != len(fields) {
		t.Fatalf("with LogMessageContent on, fields = %v, want all %d", out, len(fields))
	}
	LogMessageContent = false
}

func flatten(fields []any) []string {
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if s, ok := field.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func containsAny(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
