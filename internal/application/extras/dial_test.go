package extras

import (
	"context"
	"errors"
	"strings"
	"testing"

	derrors "github.com/iniwex5/vohive/internal/domain/errors"
)

// TestDialRejectsInvalidNumbers 验证拨号号码在触达后端前即被字符集与长度校验
// 拦截, 非法输入一律归类为 InvalidRequest 而不是落入通用 500。
func TestDialRejectsInvalidNumbers(t *testing.T) {
	service := NewService(nil, nil, nil, nil)
	for _, number := range []string{
		"",
		"   ",
		"abc",
		"10000;",                // 命令注入尝试 (ATD10000; 以外)
		"123\nAT+CHUP",          // 换行注入尝试
		strings.Repeat("1", 33), // 超出长度上限
	} {
		if err := service.Dial(context.Background(), number); err == nil {
			t.Errorf("Dial(%q): expected validation error", number)
			continue
		} else if target := new(derrors.Error); !errors.As(err, &target) || target.Code != derrors.InvalidRequest {
			t.Errorf("Dial(%q): expected InvalidRequest, got %v", number, err)
		}
	}
}
