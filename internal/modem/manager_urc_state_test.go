package modem

import (
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/config"
)

func newURCTestManager(t *testing.T) *Manager {
	t.Helper()
	m, err := New(config.DeviceConfig{
		ID:            "urc-state-test",
		DeviceBackend: "at",
		ATPort:        "/dev/ttyUSB-test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	m.running = true
	m.healthy = true
	return m
}

func TestHandleCommandKeepsMatchingStatusResponseOutOfURCPath(t *testing.T) {
	m := newURCTestManager(t)
	m.port = &timeoutSerialPort{}
	ready := m.SubscribeRDY()
	req := commandRequest{
		cmd:      "AT+CPIN?",
		timeout:  time.Second,
		respChan: make(chan string, 1),
		errChan:  make(chan error, 1),
	}
	go func() {
		m.rxChan <- rxMsg{Data: "+CPIN: READY"}
		m.rxChan <- rxMsg{Data: "OK"}
	}()

	m.handleCommand(req)
	select {
	case response := <-req.respChan:
		if response != "+CPIN: READY" {
			t.Fatalf("response = %q, want +CPIN: READY", response)
		}
	case err := <-req.errChan:
		t.Fatalf("handleCommand() error = %v", err)
	}
	select {
	case <-ready:
		t.Fatal("matching CPIN query response was dispatched as a ready URC")
	default:
	}
}

func TestHandleCommandStillDispatchesUnrelatedURC(t *testing.T) {
	m := newURCTestManager(t)
	m.port = &timeoutSerialPort{}
	received := make(chan string, 1)
	m.SetNewSMSHandler(func(storage, index string) { received <- storage + ":" + index })
	req := commandRequest{
		cmd:      "AT+CEREG?",
		timeout:  time.Second,
		respChan: make(chan string, 1),
		errChan:  make(chan error, 1),
	}
	go func() {
		m.rxChan <- rxMsg{Data: `+CMTI: "ME",7`}
		m.rxChan <- rxMsg{Data: "+CEREG: 0,5"}
		m.rxChan <- rxMsg{Data: "OK"}
	}()

	m.handleCommand(req)
	select {
	case response := <-req.respChan:
		if response != "+CEREG: 0,5" {
			t.Fatalf("response = %q, want +CEREG: 0,5", response)
		}
	case err := <-req.errChan:
		t.Fatalf("handleCommand() error = %v", err)
	}
	select {
	case ref := <-received:
		if ref != "ME:7" {
			t.Fatalf("SMS ref = %q, want ME:7", ref)
		}
	case <-time.After(time.Second):
		t.Fatal("unrelated +CMTI URC was not dispatched")
	}
}

func TestSuccessfulQueriesSeedStateBaselines(t *testing.T) {
	m := newURCTestManager(t)
	go func() {
		req := <-m.cmdChan
		if req.cmd != "AT+QSIMSTAT?" {
			t.Errorf("command = %q, want AT+QSIMSTAT?", req.cmd)
		}
		req.respChan <- "+QSIMSTAT: 1,1"
		req = <-m.cmdChan
		if req.cmd != "AT+CEREG?" {
			t.Errorf("command = %q, want AT+CEREG?", req.cmd)
		}
		req.respChan <- "+CEREG: 0,5"
	}()

	inserted, err := m.QuerySIMInserted()
	if err != nil || !inserted {
		t.Fatalf("QuerySIMInserted() = %v, %v; want true, nil", inserted, err)
	}
	stat, _, _, _, err := m.QueryRegistration()
	if err != nil || stat != 5 {
		t.Fatalf("QueryRegistration() stat/error = %d/%v, want 5/nil", stat, err)
	}
	if m.acceptStateURC(m.formatURC("+QSIMSTAT: 1,1")) {
		t.Fatal("stable QSIMSTAT URC accepted after query seeded the baseline")
	}
	if m.acceptStateURC(m.formatURC("+CEREG: 0,5")) {
		t.Fatal("stable CEREG URC accepted after query seeded the baseline")
	}
}

func TestQuerySIMInsertedUsesHandledQSIMSTATResponseWithoutCPINFallback(t *testing.T) {
	m := newURCTestManager(t)
	m.port = &timeoutSerialPort{}
	commands := make(chan string, 2)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		req := <-m.cmdChan
		commands <- req.cmd
		m.rxChan <- rxMsg{Data: "+QSIMSTAT: 1,1"}
		m.rxChan <- rxMsg{Data: "OK"}
		m.handleCommand(req)
	}()

	inserted, err := m.QuerySIMInserted()
	if err != nil || !inserted {
		t.Fatalf("QuerySIMInserted() = %v, %v; want true, nil", inserted, err)
	}
	<-workerDone
	select {
	case command := <-commands:
		if command != "AT+QSIMSTAT?" {
			t.Fatalf("command = %q, want AT+QSIMSTAT?", command)
		}
	default:
		t.Fatal("QSIMSTAT command was not observed")
	}
	select {
	case fallback := <-m.cmdChan:
		t.Fatalf("unexpected fallback command: %s", fallback.cmd)
	default:
	}
}

func TestStateURCDeduplicatesAndAcceptsRealTransitions(t *testing.T) {
	m := newURCTestManager(t)
	updates := make(chan bool, 2)
	m.SetSIMStatusHandler(func(inserted *bool, _ string) {
		if inserted != nil {
			updates <- *inserted
		}
	})

	m.handleURC("+QSIMSTAT: 1,1")
	select {
	case inserted := <-updates:
		if !inserted {
			t.Fatal("first SIM state = false, want true")
		}
	case <-time.After(time.Second):
		t.Fatal("first SIM state was not dispatched")
	}

	m.handleURC("+QSIMSTAT: 1,1")
	select {
	case duplicate := <-updates:
		t.Fatalf("duplicate SIM state dispatched: %v", duplicate)
	case <-time.After(30 * time.Millisecond):
	}

	m.handleURC("+QSIMSTAT: 1,0")
	select {
	case inserted := <-updates:
		if inserted {
			t.Fatal("transition SIM state = true, want false")
		}
	case <-time.After(time.Second):
		t.Fatal("real SIM transition was not dispatched")
	}
}

func TestRepeatedCPINReadyDoesNotRetriggerReady(t *testing.T) {
	m := newURCTestManager(t)
	first := m.SubscribeRDY()
	m.handleURC("+CPIN: READY")
	select {
	case <-first:
	case <-time.After(time.Second):
		t.Fatal("first CPIN READY did not dispatch ready")
	}

	duplicate := m.SubscribeRDY()
	m.handleURC("+CPIN: READY")
	select {
	case <-duplicate:
		t.Fatal("duplicate CPIN READY retriggered ready")
	case <-time.After(30 * time.Millisecond):
	}
}
