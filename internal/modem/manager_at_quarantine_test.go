package modem

import (
	"strings"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/config"
)

// timeoutCommandRequest 构造一个固定命令参数的请求。
func timeoutCommandRequest(cmd string, timeout time.Duration) commandRequest {
	return commandRequest{
		cmd:      cmd,
		timeout:  timeout,
		respChan: make(chan string, 1),
		errChan:  make(chan error, 1),
	}
}

// TestQuarantineConsumesLateResponseBeforeNextCommand: 命令超时后，迟到的
// 终结响应或数据行被隔离逻辑消费，绝不会泄漏到下一个命令的 fullResponse。
func TestQuarantineConsumesLateResponseBeforeNextCommand(t *testing.T) {
	m := newRunningTestManager(t)
	m.port = &timeoutSerialPort{}

	// 命令 1 超时（无响应）。
	req1 := timeoutCommandRequest("AT+SLOW", 5*time.Millisecond)
	m.handleCommand(req1)
	if err := <-req1.errChan; err == nil || err.Error() != "命令执行超时" {
		t.Fatalf("req1 error = %v, want 命令执行超时", err)
	}
	if !m.isQuarantined() {
		t.Fatal("command stream was not quarantined after timeout")
	}

	// 超时命令的残余数据在隔离期间到达，被隔离逻辑消费。
	quarantineDone := make(chan struct{})
	go func() {
		defer close(quarantineDone)
		m.waitForQuarantineRecovery()
	}()
	m.rxChan <- rxMsg{Data: "AT+SLOW: residual data"}
	m.rxChan <- rxMsg{Data: "OK"} // 迟到但终于到达的终结响应
	select {
	case <-quarantineDone:
	case <-time.After(time.Second):
		t.Fatal("quarantine did not clear after terminal response")
	}
	if m.isQuarantined() {
		t.Fatal("quarantine still active after terminal response")
	}

	// 命令 2 只观测到自己的响应：残余数据没有进入 fullResponse。
	done := make(chan string, 1)
	go func() {
		req2 := timeoutCommandRequest("AT+FAST", 5*time.Second)
		go func() { m.rxChan <- rxMsg{Data: "OK"} }()
		resp, err := func() (string, error) {
			m.handleCommand(req2)
			select {
			case resp := <-req2.respChan:
				return resp, nil
			case err := <-req2.errChan:
				return "", err
			}
		}()
		if err != nil {
			done <- "error: " + err.Error()
			return
		}
		done <- resp
	}()
	select {
	case resp := <-done:
		if resp != "" {
			t.Fatalf("req2 fullResponse = %q, want empty (no leakage)", resp)
		}
	case <-time.After(time.Second):
		t.Fatal("req2 did not complete")
	}
}

// TestQuarantineDispatchesURCs: 隔离窗口内的 URC 照常分发，不被丢弃也不归因于
// 被隔离的命令。
func TestQuarantineDispatchesURCs(t *testing.T) {
	m := newRunningTestManager(t)
	m.port = &timeoutSerialPort{}

	req1 := timeoutCommandRequest("AT+SLOW", 5*time.Millisecond)
	m.handleCommand(req1)
	<-req1.errChan
	if !m.isQuarantined() {
		t.Fatal("command stream was not quarantined after timeout")
	}

	quarantineDone := make(chan struct{})
	go func() {
		defer close(quarantineDone)
		m.waitForQuarantineRecovery()
	}()
	// 隔离期间到达 +CMTI 与 +CUSD URC。
	m.rxChan <- rxMsg{Data: `+CMTI: "SM",3`}
	m.rxChan <- rxMsg{Data: `+CUSD: 0,"余额 10 元",15`}
	select {
	case result := <-m.ussdChan:
		if !strings.Contains(result.Text, "余额") {
			t.Fatalf("USSD result = %#v, want delivered during quarantine", result)
		}
	case <-time.After(time.Second):
		t.Fatal("+CUSD URC was not delivered during quarantine")
	}
	// +CMTI 在无消费者时只保留日志，不产生命令。
	select {
	case req := <-m.cmdChan:
		t.Fatalf("+CMTI during quarantine issued command %s", req.cmd)
	default:
	}
	m.rxChan <- rxMsg{Data: "OK"}
	select {
	case <-quarantineDone:
	case <-time.After(time.Second):
		t.Fatal("quarantine did not clear")
	}
}

// TestQuarantineRecoversTransportAfterDeadline: 隔离期限内未观测到终结响应时，
// 管理器关闭传输并触发控制面恢复（重连由运行时完成）。
func TestQuarantineRecoversTransportAfterDeadline(t *testing.T) {
	m := newRunningTestManager(t)
	m.port = &timeoutSerialPort{}
	disconnected := make(chan string, 1)
	m.SetOnDisconnectWithReason(func(reason string) { disconnected <- reason })

	req1 := timeoutCommandRequest("AT+STUCK", 5*time.Millisecond)
	m.handleCommand(req1)
	<-req1.errChan
	if !m.isQuarantined() {
		t.Fatal("command stream was not quarantined after timeout")
	}
	// 缩短恢复期限，避免等待默认的 10s。
	m.atQuarantineMu.Lock()
	m.atQuarantineDeadline = time.Now().Add(100 * time.Millisecond)
	m.atQuarantineMu.Unlock()

	recovered := make(chan struct{})
	go func() {
		defer close(recovered)
		m.waitForQuarantineRecovery()
	}()
	select {
	case reason := <-disconnected:
		if reason != "at_quarantine_recovery" {
			t.Fatalf("disconnect reason = %q, want at_quarantine_recovery", reason)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("transport recovery was not triggered after quarantine deadline")
	}
	<-recovered
	if !m.port.(*timeoutSerialPort).closed.Load() {
		t.Fatal("serial port was not closed after quarantine recovery")
	}
}

// TestURCContainingPromptCharacterDispatchedNotTerminating: 包含 > 的 URC/USSD
// 行在 URC 判定优先于提示符分支后，被分发为 URC 而不是终止运行中的命令。
func TestURCContainingPromptCharacterDispatchedNotTerminating(t *testing.T) {
	m := newRunningTestManager(t)
	m.port = &timeoutSerialPort{}

	req := timeoutCommandRequest("AT+CUSD=1,\"*100#\",15", time.Second)
	done := make(chan string, 1)
	go func() {
		m.handleCommand(req)
		select {
		case resp := <-req.respChan:
			done <- "resp:" + resp
		case err := <-req.errChan:
			done <- "err:" + err.Error()
		}
	}()

	m.rxChan <- rxMsg{Data: `+CUSD: 0,"价格 > 100 元",15`}
	select {
	case result := <-m.ussdChan:
		if !strings.Contains(result.Text, ">") {
			t.Fatalf("USSD text = %q, want the >-containing line delivered", result.Text)
		}
	case <-time.After(time.Second):
		t.Fatal("URC with > was not dispatched")
	}

	m.rxChan <- rxMsg{Data: "OK"}
	select {
	case outcome := <-done:
		if !strings.HasPrefix(outcome, "resp:") {
			t.Fatalf("command outcome = %q, want clean OK completion", outcome)
		}
	case <-time.After(time.Second):
		t.Fatal("command did not complete after the >-containing URC")
	}
}

// TestWatchdogThresholdConfigurable: 阈值可通过配置调整，低于阈值不触发恢复。
func TestWatchdogThresholdConfigurable(t *testing.T) {
	// 阈值为 2：第二次普通命令超时才触发控制面恢复。
	m, err := New(config.DeviceConfig{
		ID: "dev-at", ATPort: "/dev/ttyUSB6", DeviceBackend: "at",
		ATTimeoutWatchdogThreshold: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	m.port = &timeoutSerialPort{}
	m.running = true
	m.healthy = true
	disconnected := make(chan string, 1)
	m.SetOnDisconnectWithReason(func(reason string) { disconnected <- reason })

	for i := 1; i <= 2; i++ {
		req := timeoutCommandRequest("AT+PING", 5*time.Millisecond)
		m.handleCommand(req)
		<-req.errChan
	}
	select {
	case reason := <-disconnected:
		if reason != "at_timeout_threshold" {
			t.Fatalf("reason = %q, want at_timeout_threshold", reason)
		}
	case <-time.After(time.Second):
		t.Fatal("watchdog did not trip at configured threshold 2")
	}
}

// TestWatchdogExcludesLongRunningCommands: 长耗时命令（超时 > 30s）的超时不计入
// 连续超时计数。
func TestWatchdogExcludesLongRunningCommands(t *testing.T) {
	m := newRunningTestManager(t)
	m.atTimeoutWatchdogThreshold = 1 // 阈值 1：一次普通超时就触发
	for i := 0; i < 3; i++ {
		streak, tripped := m.recordATTimeout(timeoutCommandRequest("ATD13800138000;", 60*time.Second))
		if tripped {
			t.Fatalf("long-running command %d tripped the watchdog", i)
		}
		if streak != 0 {
			t.Fatalf("long-running command %d streak = %d, want 0", i, streak)
		}
	}
	streak, tripped := m.recordATTimeout(timeoutCommandRequest("AT+CSQ", 3*time.Second))
	if streak != 1 || !tripped {
		t.Fatalf("normal command streak = %d tripped = %v, want 1, true", streak, tripped)
	}
}
