package runtime

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
)

func TestEDLSessionManagerAllowsOneOperation(t *testing.T) {
	manager := NewEDLSessionManager(nil)
	const location = "usb/1-2"
	manager.Observe(location, device.EDLObservation{State: device.EDLStateDetected})
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- manager.BeginOperation(location, "device_control.nand_backup")
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
			t.Fatalf("BeginOperation() error = %v", err)
		}
		conflicts++
	}
	if acquired != 1 || conflicts != 1 {
		t.Fatalf("acquired=%d conflicts=%d", acquired, conflicts)
	}
}

func TestEDLSessionOperationFreesDeviceAfterEnd(t *testing.T) {
	manager := NewEDLSessionManager(nil)
	const location = "usb/1-2"
	manager.Observe(location, device.EDLObservation{State: device.EDLStateDetected})
	if err := manager.BeginOperation(location, "device_control.nand_backup"); err != nil {
		t.Fatal(err)
	}
	if err := manager.BeginOperation(location, "device_control.reset"); err == nil {
		t.Fatal("second operation acquired the busy device")
	}
	manager.EndOperation(location)
	if err := manager.BeginOperation(location, "device_control.reset"); err != nil {
		t.Fatalf("device was not released after the operation ended: %v", err)
	}
}

func TestEDLSessionBusySessionIsNotEvicted(t *testing.T) {
	manager := NewEDLSessionManager(nil)
	busy := "usb/0"
	if err := manager.BeginOperation(busy, "device_control.adb_shell"); err != nil {
		t.Fatal(err)
	}
	for index := 1; index < maxEDLSessions; index++ {
		location := fmt.Sprintf("usb/%d", index)
		if _, err := manager.Observe(location, device.EDLObservation{State: device.EDLStateDetected}); err != nil {
			t.Fatal(err)
		}
	}
	// 容量已满: 新会话驱逐最旧的空闲会话, busy 会话必须保留。
	if _, err := manager.Observe(fmt.Sprintf("usb/%d", maxEDLSessions), device.EDLObservation{State: device.EDLStateDetected}); err != nil {
		t.Fatalf("idle session was not evictable: %v", err)
	}
	manager.mu.Lock()
	_, busyStillPresent := manager.sessions[busy]
	manager.mu.Unlock()
	if !busyStillPresent {
		t.Fatal("busy session was evicted")
	}
}

func TestEDLSessionAllBusyRejectsNewSession(t *testing.T) {
	manager := NewEDLSessionManager(nil)
	for index := 0; index < maxEDLSessions; index++ {
		location := fmt.Sprintf("usb/%d", index)
		if err := manager.BeginOperation(location, "device_control.nand_backup"); err != nil {
			t.Fatal(err)
		}
	}
	// 全部会话 busy 时, 新会话被拒绝而不是驱逐 busy 会话。
	if _, err := manager.Observe(fmt.Sprintf("usb/%d", maxEDLSessions), device.EDLObservation{State: device.EDLStateDetected}); err == nil {
		t.Fatal("session capacity admitted a new session while every session is busy")
	}
	// 任一 busy 会话结束后, 驱逐恢复可用。
	manager.EndOperation("usb/0")
	if _, err := manager.Observe(fmt.Sprintf("usb/%d", maxEDLSessions), device.EDLObservation{State: device.EDLStateDetected}); err != nil {
		t.Fatalf("session eviction did not recover after an operation ended: %v", err)
	}
}

func TestEDLSessionObservationDoesNotInventFirmwareRevision(t *testing.T) {
	manager := NewEDLSessionManager(nil)
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
	manager := NewEDLSessionManager(nil)
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
