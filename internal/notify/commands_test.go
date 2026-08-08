package notify

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		line     string
		wantOK   bool
		wantName string
		wantArgs []string
	}{
		{"普通命令", "/status", true, "status", nil},
		{"带参数", "/send 138 测试 短信", true, "send", []string{"138", "测试", "短信"}},
		{"群聊 @bot 后缀", "/send@my_bot 138 hi", true, "send", []string{"138", "hi"}},
		{"大写归一化", "/STATUS", true, "status", nil},
		{"前后空白", "  /status  ", true, "status", nil},
		{"非命令", "hello", false, "", nil},
		{"仅斜杠", "/", false, "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, ok := ParseCommand(tt.line, nil)
			if ok != tt.wantOK {
				t.Fatalf("ok=%v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if cmd.Name != tt.wantName {
				t.Fatalf("name=%q, want %q", cmd.Name, tt.wantName)
			}
			if strings.Join(cmd.Args, "|") != strings.Join(tt.wantArgs, "|") {
				t.Fatalf("args=%v, want %v", cmd.Args, tt.wantArgs)
			}
		})
	}
}

func TestCommandRest(t *testing.T) {
	t.Parallel()

	cmd := Command{Args: []string{"138", "验证码", "123456"}}
	if got := cmd.Rest(1); got != "验证码 123456" {
		t.Fatalf("Rest(1)=%q", got)
	}
	if got := cmd.Rest(9); got != "" {
		t.Fatalf("Rest(9)=%q, 越界应返回空串", got)
	}
	if got := cmd.Arg(0); got != "138" {
		t.Fatalf("Arg(0)=%q", got)
	}
}

func TestDispatchUnknownCommand(t *testing.T) {
	t.Parallel()

	got := NewCommands(Ports{}).Dispatch(context.Background(), Command{Name: "badcmd"})
	want := "未知命令 /badcmd\n提示  发送 /help 查看全部命令"
	if got != want {
		t.Fatalf("Dispatch()=%q, want %q", got, want)
	}
}

// 未绑定的 Port 必须返回"不可用"，而不是 panic。
func TestDispatchWithoutPortsIsSafe(t *testing.T) {
	t.Parallel()

	c := NewCommands(Ports{})
	for _, name := range []string{"status", "sms", "esim", "call", "network"} {
		got := c.Dispatch(context.Background(), Command{Name: name})
		if !strings.Contains(got, "不可用") {
			t.Fatalf("%s 未返回不可用提示: %q", name, got)
		}
	}
}

func TestDispatchStatus(t *testing.T) {
	t.Parallel()

	c := NewCommands(Ports{
		DeviceStatus: func(context.Context) (DeviceStatus, error) {
			return DeviceStatus{
				State: "ready", Healthy: true, IMEI: "86000", ICCID: "8986012345678901234",
				LocalPhone: "13800000000", Firmware: "v1.2.3", SIMInserted: true,
				Operator: "中国移动", NetworkMode: "5G", Registered: true, SignalDBM: -75,
			}, nil
		},
	})

	got := c.Dispatch(context.Background(), Command{Name: "status"})
	for _, want := range []string{"设备状态", "ready / 正常", "5G / 已注册", "-75 dBm"} {
		if !strings.Contains(got, want) {
			t.Fatalf("status 输出缺少 %q:\n%s", want, got)
		}
	}
}

func TestDispatchStatusPropagatesError(t *testing.T) {
	t.Parallel()

	c := NewCommands(Ports{
		DeviceStatus: func(context.Context) (DeviceStatus, error) {
			return DeviceStatus{}, errors.New("设备未连接")
		},
	})
	got := c.Dispatch(context.Background(), Command{Name: "status"})
	if got != "设备状态 / 失败\n原因  设备未连接" {
		t.Fatalf("Dispatch()=%q", got)
	}
}

func TestDispatchSendRequiresTwoArgs(t *testing.T) {
	t.Parallel()

	c := NewCommands(Ports{
		SendSMS: func(context.Context, string, string) (string, error) { return "", nil },
	})
	got := c.Dispatch(context.Background(), Command{Name: "send", Args: []string{"138"}})
	if !strings.Contains(got, "参数错误") {
		t.Fatalf("Dispatch()=%q", got)
	}
}

// /send 先同步回"已受理"，异步 operation 完成后再通过 Reply 补发结果。
func TestDispatchSendRepliesAsynchronously(t *testing.T) {
	t.Parallel()

	replies := make(chan string, 1)
	c := NewCommands(Ports{
		SendSMS: func(_ context.Context, recipient, body string) (string, error) {
			if recipient != "138" || body != "验证码 123456" {
				t.Errorf("SendSMS(%q, %q) 参数不符", recipient, body)
			}
			return "op-1", nil
		},
		AwaitOperation: func(context.Context, string) (bool, string) { return true, "" },
	})

	got := c.Dispatch(context.Background(), Command{
		Name:  "send",
		Args:  []string{"138", "验证码", "123456"},
		Reply: func(text string) { replies <- text },
	})
	if !strings.Contains(got, "已受理") {
		t.Fatalf("同步回复=%q，应为已受理", got)
	}

	select {
	case reply := <-replies:
		if !strings.Contains(reply, "成功") {
			t.Fatalf("异步回执=%q", reply)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("未收到异步回执")
	}
}

func TestDispatchSMSListEmpty(t *testing.T) {
	t.Parallel()

	c := NewCommands(Ports{
		ListSMS: func(context.Context, int) ([]SMSRecord, error) { return nil, nil },
	})
	if got := c.Dispatch(context.Background(), Command{Name: "sms"}); !strings.Contains(got, "暂无短信") {
		t.Fatalf("Dispatch()=%q", got)
	}
}

func TestDispatchSMSRejectsBadLimit(t *testing.T) {
	t.Parallel()

	c := NewCommands(Ports{
		ListSMS: func(context.Context, int) ([]SMSRecord, error) { return nil, nil },
	})
	if got := c.Dispatch(context.Background(), Command{Name: "sms", Args: []string{"-1"}}); !strings.Contains(got, "参数错误") {
		t.Fatalf("Dispatch()=%q", got)
	}
}

func TestResolveProfile(t *testing.T) {
	t.Parallel()

	profiles := []ESIMProfile{
		{ICCID: "8986011111111111111", Name: "移动"},
		{ICCID: "8986022222222222222", Name: "联通"},
	}

	if got, ok := resolveProfile(profiles, "2"); !ok || got.Name != "联通" {
		t.Fatalf("按序号匹配失败: %+v ok=%v", got, ok)
	}
	if got, ok := resolveProfile(profiles, "8986011111111111111"); !ok || got.Name != "移动" {
		t.Fatalf("按完整 ICCID 匹配失败: %+v ok=%v", got, ok)
	}
	if got, ok := resolveProfile(profiles, "2222"); !ok || got.Name != "联通" {
		t.Fatalf("按 ICCID 后缀匹配失败: %+v ok=%v", got, ok)
	}
	if _, ok := resolveProfile(profiles, "9"); ok {
		t.Fatal("越界序号不应匹配")
	}
	// 后缀短于 4 位时不做模糊匹配，避免误切卡。
	if _, ok := resolveProfile(profiles, "222"); ok {
		t.Fatal("过短的后缀不应匹配")
	}
}

func TestMaskICCID(t *testing.T) {
	t.Parallel()

	if got := maskICCID("8986011111111111111"); got != "898601...1111" {
		t.Fatalf("maskICCID()=%q", got)
	}
	if got := maskICCID("12345"); got != "12345" {
		t.Fatalf("短 ICCID 不应遮蔽: %q", got)
	}
	if got := maskICCID(""); got != "--" {
		t.Fatalf("maskICCID(\"\")=%q", got)
	}
}

func TestSortSMSByTimeDesc(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	records := []SMSRecord{
		{Body: "旧", At: base.Add(-time.Hour)},
		{Body: "新", At: base},
		{Body: "中", At: base.Add(-30 * time.Minute)},
	}
	SortSMSByTimeDesc(records)
	if records[0].Body != "新" || records[2].Body != "旧" {
		t.Fatalf("排序结果: %+v", records)
	}
}

func TestHelpListsAllCommands(t *testing.T) {
	t.Parallel()

	c := NewCommands(Ports{})
	got := c.Dispatch(context.Background(), Command{Name: "help"})
	for _, name := range c.order {
		if !strings.Contains(got, "/"+name) {
			t.Fatalf("/help 缺少命令 %q:\n%s", name, got)
		}
	}
}
