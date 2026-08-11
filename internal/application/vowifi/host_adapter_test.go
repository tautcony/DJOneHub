package vowifi

import (
	"context"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/domain/device"
)

type directATTransport struct{}

func (directATTransport) Command(string, time.Duration) (string, error) { return "OK\r\n", nil }
func (directATTransport) Close() error                                  { return nil }

func TestHostAdapterIsSwitching(t *testing.T) {
	ops := operation.NewManager(nil)
	adapter := &hostAdapter{ops: ops}

	if adapter.IsSwitching("main") {
		t.Fatal("IsSwitching() with no operations = true, want false")
	}

	block := make(chan struct{})
	id, err := ops.Start(context.Background(), "esim.enable", func(context.Context, string, func(int, string)) error {
		<-block
		return nil
	})
	if err != nil {
		t.Fatalf("start esim.enable: %v", err)
	}
	defer ops.Cancel(id)

	if !adapter.IsSwitching("main") {
		t.Fatal("IsSwitching() during esim.enable = false, want true")
	}

	// 其他类型操作（如 vowifi.enable）不算切卡。
	blockOther := make(chan struct{})
	idOther, err := ops.Start(context.Background(), "vowifi.enable", func(context.Context, string, func(int, string)) error {
		<-blockOther
		return nil
	})
	if err != nil {
		t.Fatalf("start vowifi.enable: %v", err)
	}
	defer ops.Cancel(idOther)
	if !adapter.IsSwitching("main") {
		t.Fatal("IsSwitching() with concurrent vowifi.enable = false, want true (esim.enable still active)")
	}

	close(block)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		// vowifi.enable 仍在运行，但 esim.enable 已结束。
		if !adapter.IsSwitching("main") {
			close(blockOther)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(blockOther)
	t.Fatal("IsSwitching() still true after esim.enable completed")
}

func TestHostAdapterIsSwitchingNilOps(t *testing.T) {
	adapter := &hostAdapter{}
	if adapter.IsSwitching("main") {
		t.Fatal("IsSwitching() with nil ops = true, want false")
	}
}

func TestUnwrapLegacyBackendAcceptsDirectCommandBackend(t *testing.T) {
	direct := backend.NewCommandBackend(directATTransport{}, device.Identity{StableID: "direct-usb-at"})
	db, manager := unwrapLegacyBackend(direct)
	if db == nil {
		t.Fatal("unwrapLegacyBackend() backend = nil for CommandBackend")
	}
	if manager != nil {
		t.Fatal("unwrapLegacyBackend() manager != nil for direct USB backend")
	}
	if _, err := newVoWiFiModemInterface(db, nil, "main"); err != nil {
		t.Fatalf("newVoWiFiModemInterface(): %v", err)
	}
}
