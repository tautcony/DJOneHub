package modem

import (
	"errors"
	"testing"
	"time"
)

// TestQueryOperatorIssuesOnlyPureQuery 验证轮询路径只发出 AT+COPS?:
// 重复轮询绝不改写用户的格式选择 (无 AT+COPS=3,2), 且解析 modem 报告的格式。
func TestQueryOperatorIssuesOnlyPureQuery(t *testing.T) {
	var issued []string
	exec := func(cmd string, timeout time.Duration) (string, error) {
		issued = append(issued, cmd)
		return "\r\n+COPS: 0,2,\"46011\",7\r\n\r\nOK\r\n", nil
	}

	got, err := queryOperator(exec)
	if err != nil {
		t.Fatalf("queryOperator() error = %v", err)
	}
	if got != "中国电信" {
		t.Fatalf("queryOperator() = %q, want 中国电信", got)
	}

	// 第二次轮询: 仍只发 AT+COPS?, 格式选择保持不变。
	if _, err := queryOperator(exec); err != nil {
		t.Fatalf("queryOperator() second poll error = %v", err)
	}
	for i, cmd := range issued {
		if cmd != "AT+COPS?" {
			t.Fatalf("issued[%d] = %q, want only AT+COPS? (no format-setting command)", i, cmd)
		}
	}
	if len(issued) != 2 {
		t.Fatalf("issued %d commands, want 2", len(issued))
	}
}

// TestQueryOperatorPropagatesExecutorError 验证 executor 错误原样返回。
func TestQueryOperatorPropagatesExecutorError(t *testing.T) {
	sentinel := errors.New("at port gone")
	exec := func(cmd string, timeout time.Duration) (string, error) {
		return "", sentinel
	}
	if _, err := queryOperator(exec); !errors.Is(err, sentinel) {
		t.Fatalf("queryOperator() error = %v, want %v", err, sentinel)
	}
}
