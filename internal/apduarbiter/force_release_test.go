package apduarbiter

import (
	"context"
	"sync"
	"testing"
	"time"
)

// recordingOwner 记录隔离上报的传输所有者桩。
type recordingOwner struct {
	mu     sync.Mutex
	calls  []string
	report chan string
}

func newRecordingOwner() *recordingOwner {
	return &recordingOwner{report: make(chan string, 4)}
}

func (o *recordingOwner) OnTransportQuarantine(reason string) {
	o.mu.Lock()
	o.calls = append(o.calls, reason)
	o.mu.Unlock()
	select {
	case o.report <- reason:
	default:
	}
}

// TestForceReleasedTransportPreservesExclusivityWhileAPDUInFlight: 被强制释放的
// transport 租约的 APDU 在飞行中时，不得授予新的独占租约或 SIM 切换屏障；
// 持有方报告完成（Release）后立即恢复授予。
func TestForceReleasedTransportPreservesExclusivityWhileAPDUInFlight(t *testing.T) {
	arb := New("dev-1", Options{
		MaxLeaseHold:              30 * time.Millisecond,
		TransportRecoveryDeadline: 5 * time.Second,
	})

	holder, err := arb.AcquireTransport(context.Background(), Request{Owner: "holder", Mode: "AT", Class: APDUClassEUICCWrite})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-holder.Forced():
	case <-time.After(time.Second):
		t.Fatal("holder was not notified of force release")
	}

	// APDU 仍在飞行中：独占租约与切换屏障都不得授予。
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if lease, err := arb.AcquireTransport(ctx, Request{Owner: "contender", Mode: "AT", Class: APDUClassEUICCWrite}); err == nil {
		lease.Release()
		t.Fatal("new exclusive transport granted while force-released APDU is in flight")
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel2()
	if barrier, err := arb.BeginBarrier(ctx2, Request{Owner: "switcher", Mode: "AT"}, BarrierPolicy{}); err == nil {
		barrier.Release()
		t.Fatal("SIM-switch barrier granted while force-released APDU is in flight")
	}

	// 持有方报告完成：设备释放，新的独占租约可以授予。
	holder.Release()
	lease, err := arb.AcquireTransport(context.Background(), Request{Owner: "contender", Mode: "AT", Class: APDUClassEUICCWrite})
	if err != nil {
		t.Fatalf("exclusive transport still blocked after in-flight APDU completed: %v", err)
	}
	lease.Release()
	if err := arb.WaitIdle(context.Background()); err != nil {
		t.Fatalf("WaitIdle() error=%v", err)
	}
}

// TestForceReleasedTransportQuarantinesAfterRecoveryDeadline: 飞行中的 APDU 在
// 传输恢复期限内未完成时，传输被标记隔离并上报传输所有者；隔离期间不接受新的
// APDU 工作；所有者确认恢复后重新接受。
func TestForceReleasedTransportQuarantinesAfterRecoveryDeadline(t *testing.T) {
	owner := newRecordingOwner()
	arb := New("dev-1", Options{
		MaxLeaseHold:              30 * time.Millisecond,
		TransportRecoveryDeadline: 100 * time.Millisecond,
	})
	arb.SetTransportOwner(owner)

	holder, err := arb.AcquireTransport(context.Background(), Request{Owner: "holder", Mode: "AT", Class: APDUClassEUICCWrite})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-holder.Forced():
	case <-time.After(time.Second):
		t.Fatal("holder was not notified of force release")
	}
	// 卡死的 APDU：持有方永不报告完成。

	select {
	case reason := <-owner.report:
		if reason != "apdu_transport_recovery_deadline" {
			t.Fatalf("owner report reason = %q", reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("transport owner was not notified of quarantine")
	}
	if !arb.Stats().ActiveTransport {
		t.Fatalf("stats = %+v, want quarantined transport still reported active", arb.Stats())
	}

	// 隔离期间不接受任何新 APDU 工作。
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if lease, err := arb.AcquireTransport(ctx, Request{Owner: "contender", Mode: "AT", Class: APDUClassEUICCWrite}); err == nil {
		lease.Release()
		t.Fatal("APDU work admitted while transport is quarantined")
	}

	// 所有者重新初始化后确认恢复：新工作被接受。
	arb.ConfirmTransportRecovery()
	lease, err := arb.AcquireTransport(context.Background(), Request{Owner: "contender", Mode: "AT", Class: APDUClassEUICCWrite})
	if err != nil {
		t.Fatalf("transport still quarantined after owner confirmed recovery: %v", err)
	}
	lease.Release()
}

// TestLeaseTouchPreventsForceRelease: 持有方通过 Touch 报告进展时，活跃租约不
// 会被 MaxLeaseHold 强制释放。
func TestLeaseTouchPreventsForceRelease(t *testing.T) {
	arb := New("dev-1", Options{MaxLeaseHold: 50 * time.Millisecond})
	lease, err := arb.AcquireTransport(context.Background(), Request{Owner: "holder", Mode: "AT", Class: APDUClassEUICCWrite})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()

	for i := 0; i < 6; i++ {
		time.Sleep(40 * time.Millisecond)
		if !lease.Touch() {
			t.Fatalf("Touch() = false after %d iterations", i+1)
		}
		select {
		case <-lease.Forced():
			t.Fatal("progressing lease was force-released by the watchdog")
		default:
		}
	}
	if arb.Stats().ForcedReleases != 0 {
		t.Fatalf("ForcedReleases = %d, want 0 for a progressing lease", arb.Stats().ForcedReleases)
	}
}
