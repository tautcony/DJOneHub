//go:build windows

package native

import "testing"

func TestTrimWindowsNotificationText(t *testing.T) {
	if got := trimWindowsNotificationText(" a\n b\t", 20); got != "a  b" {
		t.Fatalf("normalized text = %q, want %q", got, "a  b")
	}
	if got := trimWindowsNotificationText("123456", 5); got != "1234…" {
		t.Fatalf("truncated text = %q, want %q", got, "1234…")
	}
}
