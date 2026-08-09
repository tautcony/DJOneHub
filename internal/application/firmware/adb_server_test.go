package firmware

import (
	"net"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"
)

func adbServerListening() bool {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:5037", 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// TestListADBDevicesReportsMissingADB verifies that a missing adb executable
// is reported as such without attempting to dial the adb server.
func TestListADBDevicesReportsMissingADB(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := listADBDevices("")
	if err == nil || !strings.Contains(err.Error(), "adb executable not found") {
		t.Fatalf("listADBDevices() = %v, want adb-not-found error", err)
	}
	if strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("listADBDevices() dialed despite missing adb: %v", err)
	}
}

// TestListADBDevicesStartsServerOnRefusedConnection verifies that when no
// adb server is listening, listADBDevices attempts `adb start-server` and
// throttles repeated attempts so a polling status doesn't spawn a process
// on every tick.
func TestListADBDevicesStartsServerOnRefusedConnection(t *testing.T) {
	if adbServerListening() {
		t.Skip("an adb server is already running on 127.0.0.1:5037")
	}
	marker := filepath.Join(t.TempDir(), "starts.txt")
	script := filepath.Join(t.TempDir(), "fake-adb")
	content := "#!/bin/sh\n" + "echo start >> " + marker + "\n" + "exit 1\n"
	if goruntime.GOOS == "windows" {
		script += ".cmd"
		content = "@echo off\r\necho start >> \"" + marker + "\"\r\nexit /b 1\r\n"
	}
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}

	_, firstErr := listADBDevices(script)
	if firstErr == nil {
		t.Fatal("listADBDevices() succeeded without an adb server")
	}
	if !strings.Contains(firstErr.Error(), "could not be started") {
		t.Fatalf("first error %q should mention the failed start attempt", firstErr)
	}

	_, secondErr := listADBDevices(script)
	if secondErr == nil {
		t.Fatal("listADBDevices() succeeded without an adb server")
	}
	if strings.Contains(secondErr.Error(), "could not be started") {
		t.Fatalf("second error %q should not re-run the throttled start", secondErr)
	}

	starts, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(starts), "start"); count != 1 {
		t.Fatalf("adb start-server ran %d times, want exactly 1 (throttled)", count)
	}
}
