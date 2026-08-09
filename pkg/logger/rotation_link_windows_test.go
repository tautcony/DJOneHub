//go:build windows

package logger

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSetupUsesHardLinkForStableLogName(t *testing.T) {
	directory := t.TempDir()
	filename := filepath.Join(directory, "app.log")
	Setup(LogConfig{Filename: filename, Debug: true})
	t.Cleanup(closeCurrentRotator)
	Info("windows stable log entry")

	deadline := time.Now().Add(2 * time.Second)
	for {
		info, err := os.Lstat(filename)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				t.Fatalf("stable log name is a symlink: %s", filename)
			}
			data, readErr := os.ReadFile(filename)
			if readErr == nil && string(data) != "" {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("stable log name was not created: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestUpdateWindowsCurrentLogFollowsRotation(t *testing.T) {
	directory := t.TempDir()
	filename := filepath.Join(directory, "app.log")
	first := filepath.Join(directory, "app-2026-08-08.log")
	second := filepath.Join(directory, "app-2026-08-09.log")
	if err := os.WriteFile(first, []byte("first"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := updateWindowsCurrentLog(filename, first); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filename); err != nil || string(data) != "first" {
		t.Fatalf("initial stable log = %q, err=%v", data, err)
	}
	if err := updateWindowsCurrentLog(filename, second); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filename); err != nil || string(data) != "second" {
		t.Fatalf("rotated stable log = %q, err=%v", data, err)
	}
}
