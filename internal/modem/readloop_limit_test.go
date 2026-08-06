package modem

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.bug.st/serial"
)

// scriptedSerialPort 按脚本顺序返回读取块，用于驱动 readLoop 的边界测试。
// 块跨多次 Read 调用切片返回（Read 缓冲区小于块长度）。
type scriptedSerialPort struct {
	chunks   [][]byte
	pos      int
	chunkPos int
	closed   atomic.Bool
	timeout  error
}

func (p *scriptedSerialPort) SetMode(*serial.Mode) error { return nil }
func (p *scriptedSerialPort) Read(b []byte) (int, error) {
	if p.pos >= len(p.chunks) {
		if p.timeout != nil {
			return 0, p.timeout
		}
		return 0, errors.New("timeout")
	}
	chunk := p.chunks[p.pos]
	remaining := chunk[p.chunkPos:]
	n := copy(b, remaining)
	p.chunkPos += n
	if p.chunkPos >= len(chunk) {
		p.pos++
		p.chunkPos = 0
	}
	return n, nil
}
func (p *scriptedSerialPort) Write(b []byte) (int, error) { return len(b), nil }
func (p *scriptedSerialPort) Drain() error                { return nil }
func (p *scriptedSerialPort) ResetInputBuffer() error     { return nil }
func (p *scriptedSerialPort) ResetOutputBuffer() error    { return nil }
func (p *scriptedSerialPort) SetDTR(bool) error           { return nil }
func (p *scriptedSerialPort) SetRTS(bool) error           { return nil }
func (p *scriptedSerialPort) GetModemStatusBits() (*serial.ModemStatusBits, error) {
	return nil, nil
}
func (p *scriptedSerialPort) SetReadTimeout(time.Duration) error { return nil }
func (p *scriptedSerialPort) Break(time.Duration) error          { return nil }
func (p *scriptedSerialPort) Close() error {
	p.closed.Store(true)
	return nil
}

// TestReadLoopCapsOverLongLines: 超过 4 KB 的行被截断且丢弃超限字节，超过
// 上限的字节绝不进入 rxChan（从而不会进入任何命令的 fullResponse）。
func TestReadLoopCapsOverLongLines(t *testing.T) {
	port := &scriptedSerialPort{timeout: errors.New("timeout")}
	port.chunks = [][]byte{
		[]byte(strings.Repeat("A", maxATLineBytes) + strings.Repeat("B", 512) + "\nOK\r\n"),
	}
	m := newRunningTestManager(t)
	m.port = port
	go m.readLoop()

	var lines []string
	deadline := time.After(time.Second)
	for len(lines) < 2 {
		select {
		case msg := <-m.rxChan:
			if msg.Err != nil {
				t.Fatalf("rxChan error: %v", msg.Err)
			}
			lines = append(lines, msg.Data)
		case <-deadline:
			t.Fatalf("timed out waiting for lines, got %#v", lines)
		}
	}
	if lines[0] != strings.Repeat("A", maxATLineBytes) {
		t.Fatalf("over-long line = %d bytes, want exactly %d and no dropped tail", len(lines[0]), maxATLineBytes)
	}
	if strings.Contains(lines[0], "B") {
		t.Fatal("dropped over-limit bytes leaked into the line")
	}
	if lines[1] != "OK" {
		t.Fatalf("second line = %q, want OK", lines[1])
	}
}

// TestReadLoopTreatsSustainedOverLimitInputAsMisbehavior: 持续无换行的超限
// 输入被判定为设备异常并触发控制面恢复。
func TestReadLoopTreatsSustainedOverLimitInputAsMisbehavior(t *testing.T) {
	port := &scriptedSerialPort{timeout: errors.New("timeout")}
	port.chunks = [][]byte{
		[]byte(strings.Repeat("A", maxATLineBytes) + strings.Repeat("B", maxATOverLimitBytes+1)),
	}
	m := newRunningTestManager(t)
	m.port = port
	disconnected := make(chan string, 1)
	m.SetOnDisconnectWithReason(func(reason string) { disconnected <- reason })

	go m.readLoop()

	select {
	case reason := <-disconnected:
		if reason != "at_over_limit_input" {
			t.Fatalf("disconnect reason = %q, want at_over_limit_input", reason)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("sustained over-limit input did not trip device-misbehavior recovery")
	}
	if !port.closed.Load() {
		t.Fatal("serial port was not closed after over-limit recovery")
	}
}
