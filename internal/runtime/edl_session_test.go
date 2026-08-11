package runtime

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
)

func TestEDLSessionManagerAllowsOneLease(t *testing.T) {
	manager := NewEDLSessionManager(nil, time.Minute)
	const location = "usb/1-2"
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := manager.Acquire(location)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	var acquired, conflicts int
	for err := range results {
		if err == nil {
			acquired++
			continue
		}
		var structured *derrors.Error
		if !errors.As(err, &structured) || structured.Code != derrors.DeviceSessionConflict {
			t.Fatalf("Acquire() error = %v", err)
		}
		conflicts++
	}
	if acquired != 1 || conflicts != 1 {
		t.Fatalf("acquired=%d conflicts=%d", acquired, conflicts)
	}
}

func TestEDLSessionLeaseExpiresAndCanBeReacquired(t *testing.T) {
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	manager := NewEDLSessionManager(nil, time.Second)
	manager.now = func() time.Time { return now }
	token, _, err := manager.Acquire("usb/1-2")
	if err != nil || token == "" {
		t.Fatalf("Acquire() = %q, %v", token, err)
	}
	now = now.Add(2 * time.Second)
	second, snapshot, err := manager.Acquire("usb/1-2")
	if err != nil || second == "" || second == token {
		t.Fatalf("Acquire() after expiry = %q, %+v, %v", second, snapshot, err)
	}
}

func TestEDLSessionActiveOperationPinsExpiredLease(t *testing.T) {
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	manager := NewEDLSessionManager(nil, time.Second)
	manager.now = func() time.Time { return now }
	token, _, err := manager.Acquire("usb/1-2")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.BeginOperation("usb/1-2", token, "device_control.nand_backup"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if _, _, err := manager.Acquire("usb/1-2"); err == nil {
		t.Fatal("active operation allowed the expired lease to be stolen")
	}
	manager.EndOperation("usb/1-2", token)
	if _, _, err := manager.Acquire("usb/1-2"); err != nil {
		t.Fatalf("lease was not released after the operation ended: %v", err)
	}
}

func TestEDLSessionObservationDoesNotInventFirmwareRevision(t *testing.T) {
	manager := NewEDLSessionManager(nil, time.Minute)
	snapshot, err := manager.Observe("usb/1-2", device.EDLObservation{
		State:      device.EDLStateSaharaIdentified,
		Protocol:   "sahara",
		HardwareID: "masked-hwid",
		SBLVersion: "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Observation.State != device.EDLStateSaharaIdentified || snapshot.Observation.SBLVersion != "1" {
		t.Fatalf("Observation = %+v", snapshot.Observation)
	}
}

func TestEDLSessionHistoryIsBounded(t *testing.T) {
	manager := NewEDLSessionManager(nil, time.Minute)
	for index := 0; index < maxEDLSessions+3; index++ {
		location := fmt.Sprintf("usb/%d", index)
		if _, err := manager.Observe(location, device.EDLObservation{State: device.EDLStateDetected}); err != nil {
			t.Fatal(err)
		}
	}
	manager.mu.Lock()
	count := len(manager.sessions)
	manager.mu.Unlock()
	if count != maxEDLSessions {
		t.Fatalf("session count=%d, want %d", count, maxEDLSessions)
	}
}
