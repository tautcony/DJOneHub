package runtime

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
)

// slowDiscovery blocks each Discover call until released, letting the test
// hold a scan in flight while a second rescan arrives.
type slowDiscovery struct {
	active    atomic.Int32
	entered   chan struct{}
	release   chan struct{}
	callCount atomic.Int32
}

func (d *slowDiscovery) Discover(context.Context) ([]device.Candidate, error) {
	d.callCount.Add(1)
	if d.entered != nil {
		d.entered <- struct{}{}
	}
	<-d.release
	return nil, nil
}

// noBackends 不提供后端工厂, 但扫描在无候选时不会走到 Open。
type noBackends struct{}

func (noBackends) Open(context.Context, device.Candidate) (backend.ModemBackend, string, error) {
	return nil, "", derrors.New(derrors.BackendUnavailable, "no backends", true, nil)
}

// TestRescanSerializesWithInFlightScan 验证 HTTP rescan 与轮询扫描共享同一
// 生命周期锁: 第一个扫描未完成时, 第二个扫描排队等待而不是交错执行。
func TestRescanSerializesWithInFlightScan(t *testing.T) {
	discovery := &slowDiscovery{entered: make(chan struct{}, 4), release: make(chan struct{})}
	r, err := New(Config{Discovery: discovery, Backends: noBackends{}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	firstDone := make(chan error, 1)
	go func() { firstDone <- r.scan(context.Background()) }()
	<-discovery.entered // 第一个扫描已进入 Discover 并阻塞

	secondDone := make(chan error, 1)
	go func() { secondDone <- r.scan(context.Background()) }()

	select {
	case <-secondDone:
		t.Fatal("second scan completed while the first scan was in flight; scans are not serialized")
	case <-time.After(50 * time.Millisecond):
	}

	close(discovery.release)
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("first scan error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("first scan did not finish after release")
	}
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("second scan error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("second scan did not finish after the first scan released")
	}
	if got := discovery.callCount.Load(); got != 2 {
		t.Fatalf("Discover call count = %d, want 2 (serialized)", got)
	}
}

// TestRescanRejectedAfterStop 验证关闭后的 rescan 被拒绝, 不会重新安装后端。
func TestRescanRejectedAfterStop(t *testing.T) {
	discovery := &slowDiscovery{release: make(chan struct{})}
	close(discovery.release)
	r, err := New(Config{Discovery: discovery, Backends: noBackends{}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	r.Start(context.Background())
	r.Stop()

	err = r.Rescan(context.Background())
	if err == nil {
		t.Fatal("Rescan() after Stop succeeded, want rejection")
	}
	var structured *derrors.Error
	if !errorsAs(err, &structured) || structured.Code != derrors.Unavailable {
		t.Fatalf("Rescan() after Stop error = %v, want %s", err, derrors.Unavailable)
	}
}

func errorsAs(err error, target **derrors.Error) bool {
	for err != nil {
		if typed, ok := err.(*derrors.Error); ok {
			*target = typed
			return true
		}
		type unwrapper interface{ Unwrap() error }
		next, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = next.Unwrap()
	}
	return false
}
