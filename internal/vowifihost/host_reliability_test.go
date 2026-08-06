package vowifihost

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/backend"
)

// flakyEnablePort fails the first Enable and records the child contexts and
// Disable calls, so tests can verify fail cleans up cancel/port across
// retries.
type flakyEnablePort struct {
	mu         sync.Mutex
	enableCtxs []context.Context
	disables   int
	failFirst  bool
}

func (p *flakyEnablePort) Enable(ctx context.Context) error {
	p.mu.Lock()
	p.enableCtxs = append(p.enableCtxs, ctx)
	fail := p.failFirst
	p.failFirst = false
	p.mu.Unlock()
	if fail {
		return errors.New("enable failed")
	}
	return nil
}
func (p *flakyEnablePort) Disable(context.Context) error {
	p.mu.Lock()
	p.disables++
	p.mu.Unlock()
	return nil
}
func (p *flakyEnablePort) Reconnect(context.Context) error { return nil }
func (p *flakyEnablePort) Status(context.Context) (map[string]any, error) {
	return map[string]any{"state": "connected"}, nil
}

type singlePortFactory struct{ port backend.VoWiFiPort }

func (f singlePortFactory) Open(context.Context) (backend.VoWiFiPort, error) { return f.port, nil }

// TestFailedEnableCleansUpCancelAndPortAcrossRetries verifies the fail path
// cancels the stored child context and closes any opened port before setting
// Failed, so repeated failed enables cannot leak modem ports or event
// consumers, and a retry starts from a clean state.
func TestFailedEnableCleansUpCancelAndPortAcrossRetries(t *testing.T) {
	port := &flakyEnablePort{failFirst: true}
	host := New(singlePortFactory{port: port}, nil)

	if err := host.Enable(context.Background()); err == nil {
		t.Fatal("first Enable must fail")
	}
	if host.State() != Failed {
		t.Fatalf("state after failed enable = %s, want failed", host.State())
	}
	port.mu.Lock()
	firstCtx := port.enableCtxs[0]
	disables := port.disables
	port.mu.Unlock()
	if disables != 1 {
		t.Fatalf("port disables = %d, want 1 (fail must close the opened port)", disables)
	}
	if err := firstCtx.Err(); err != context.Canceled {
		t.Fatalf("stored child context not cancelled after fail: %v", err)
	}

	// The retry succeeds from a clean state: a fresh child context and no
	// repeated cleanup.
	if err := host.Enable(context.Background()); err != nil {
		t.Fatalf("retry Enable: %v", err)
	}
	if host.State() != Connected {
		t.Fatalf("state after retry = %s, want connected", host.State())
	}
	port.mu.Lock()
	defer port.mu.Unlock()
	if len(port.enableCtxs) != 2 {
		t.Fatalf("enable contexts = %d, want 2", len(port.enableCtxs))
	}
	if err := port.enableCtxs[1].Err(); err != nil {
		t.Fatalf("retry child context already cancelled: %v", err)
	}
	if port.disables != 1 {
		t.Fatalf("port disables after retry = %d, want 1 (no cleanup on success)", port.disables)
	}
}

// serialPort tracks the maximum number of concurrent port operations, so
// tests can assert transitions never interleave on the same port.
type serialPort struct {
	mu        sync.Mutex
	active    int
	maxActive int
	enableIn  chan struct{} // Enable blocks until release
	enableOut chan struct{}
}

func (p *serialPort) begin() {
	p.mu.Lock()
	p.active++
	if p.active > p.maxActive {
		p.maxActive = p.active
	}
	p.mu.Unlock()
}

func (p *serialPort) end() {
	p.mu.Lock()
	p.active--
	p.mu.Unlock()
}

func (p *serialPort) Enable(ctx context.Context) error {
	p.begin()
	defer p.end()
	close(p.enableIn)
	<-p.enableOut
	return nil
}
func (p *serialPort) Disable(context.Context) error { return nil }
func (p *serialPort) Reconnect(ctx context.Context) error {
	p.begin()
	defer p.end()
	return nil
}
func (p *serialPort) Status(context.Context) (map[string]any, error) {
	return map[string]any{"state": "connected"}, nil
}

// TestTransitionsCannotInterleaveOnTheSamePort verifies recovery (and any
// other transition) cannot interleave with a user Enable/Disable in progress
// on the port: all transitions are serialized through the transition lock.
func TestTransitionsCannotInterleaveOnTheSamePort(t *testing.T) {
	port := &serialPort{enableIn: make(chan struct{}), enableOut: make(chan struct{})}
	host := New(singlePortFactory{port: port}, nil)
	host.recoverDelay = 10 * time.Millisecond

	enableDone := make(chan error, 1)
	go func() { enableDone <- host.Enable(context.Background()) }()
	select {
	case <-port.enableIn:
	case <-time.After(2 * time.Second):
		t.Fatal("Enable never reached the port")
	}

	// While Enable is in progress, recovery is triggered; it must wait for
	// the transition lock instead of interleaving on the port.
	recoverDone := make(chan struct{})
	go func() {
		host.TriggerRecovery()
		close(recoverDone)
	}()
	time.Sleep(50 * time.Millisecond)
	port.mu.Lock()
	maxDuringEnable := port.maxActive
	port.mu.Unlock()
	if maxDuringEnable != 1 {
		t.Fatalf("max concurrent port ops = %d, want 1", maxDuringEnable)
	}

	close(port.enableOut)
	select {
	case err := <-enableDone:
		if err != nil {
			t.Fatalf("Enable: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Enable never completed")
	}
	select {
	case <-recoverDone:
	case <-time.After(2 * time.Second):
		t.Fatal("recovery never ran after Enable completed")
	}
	port.mu.Lock()
	defer port.mu.Unlock()
	if port.maxActive != 1 {
		t.Fatalf("max concurrent port ops = %d, want 1", port.maxActive)
	}
}
