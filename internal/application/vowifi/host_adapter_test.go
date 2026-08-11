package vowifi

import (
	"context"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/application/operation"
)

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
