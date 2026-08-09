package firmware

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"testing"
)

// memorySettingsStore is an in-memory storage.ValueStore for tests.
type memorySettingsStore struct {
	mu      sync.Mutex
	encoded string
}

func (m *memorySettingsStore) Read(value any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.encoded == "" {
		return nil
	}
	return json.Unmarshal([]byte(m.encoded), value)
}

func (m *memorySettingsStore) Write(value any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	m.encoded = string(encoded)
	return nil
}

func TestADBCommandPrecedence(t *testing.T) {
	store := &memorySettingsStore{}
	if err := store.Write(&Settings{ADBCommand: "/saved/adb"}); err != nil {
		t.Fatal(err)
	}

	// The value saved from the UI wins over the default.
	service := NewService(nil, nil, nil, Config{Store: store})
	command, source := service.adbCommandConfig()
	if command != "/saved/adb" || source != "saved" {
		t.Fatalf("adbCommandConfig() = %q, %q; want /saved/adb, saved", command, source)
	}

	// DJONEHUB_ADB_COMMAND wins over the saved value.
	service = NewService(nil, nil, nil, Config{Store: store, ADBCommand: "/env/adb"})
	command, source = service.adbCommandConfig()
	if command != "/env/adb" || source != "env" {
		t.Fatalf("adbCommandConfig() = %q, %q; want /env/adb, env", command, source)
	}

	// Nothing configured: the default "adb" from PATH.
	service = NewService(nil, nil, nil, Config{})
	command, source = service.adbCommandConfig()
	if command != "" || source != "default" {
		t.Fatalf("adbCommandConfig() = %q, %q; want empty, default", command, source)
	}
}

func TestSetADBCommand(t *testing.T) {
	store := &memorySettingsStore{}
	service := NewService(nil, nil, nil, Config{Store: store})

	executable := filepath.Join(t.TempDir(), "adb")
	contents := []byte("#!/bin/sh\n")
	if goruntime.GOOS == "windows" {
		executable += ".cmd"
		contents = []byte("@echo off\r\n")
	}
	if err := os.WriteFile(executable, contents, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := service.SetADBCommand(context.Background(), executable); err != nil {
		t.Fatalf("SetADBCommand() error: %v", err)
	}
	command, source := service.adbCommandConfig()
	if command != executable || source != "saved" {
		t.Fatalf("adbCommandConfig() = %q, %q; want %q, saved", command, source, executable)
	}

	// A new service reads the persisted value back.
	reloaded := NewService(nil, nil, nil, Config{Store: store})
	if command, _ := reloaded.adbCommandConfig(); command != executable {
		t.Fatalf("reloaded adbCommandConfig() = %q, want %q", command, executable)
	}

	// An empty command clears the saved value.
	if err := service.SetADBCommand(context.Background(), ""); err != nil {
		t.Fatalf("SetADBCommand(clear) error: %v", err)
	}
	if command, source := service.adbCommandConfig(); command != "" || source != "default" {
		t.Fatalf("adbCommandConfig() after clear = %q, %q; want empty, default", command, source)
	}

	// A command that does not resolve to an executable is rejected.
	if err := service.SetADBCommand(context.Background(), "definitely-not-a-real-command-xyz"); err == nil {
		t.Fatal("SetADBCommand() with an unknown command should fail")
	}

	// Without a store the setting cannot be persisted.
	noStore := NewService(nil, nil, nil, Config{})
	if err := noStore.SetADBCommand(context.Background(), executable); err == nil {
		t.Fatal("SetADBCommand() without a store should fail")
	}
}

func TestADBStatusExposesCommand(t *testing.T) {
	store := &memorySettingsStore{}
	if err := store.Write(&Settings{ADBCommand: "/saved/adb"}); err != nil {
		t.Fatal(err)
	}
	service := NewService(nil, nil, nil, Config{Store: store})
	service.adbList = func() ([]ADBDevice, error) { return nil, errors.New("no adb server") }
	status := service.adbStatus()
	if status.Command != "/saved/adb" || status.CommandSource != "saved" {
		t.Fatalf("adbStatus() command = %q, %q; want /saved/adb, saved", status.Command, status.CommandSource)
	}
	if status.Error == "" {
		t.Fatal("adbStatus() should report the failing device list")
	}
}

func TestLinuxPicker(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("linux picker only")
	}

	// No graphical helper on PATH: capability error mentioning zenity.
	t.Setenv("PATH", t.TempDir())
	_, err := linuxPicker(context.Background(), "pick a folder", true)
	if err == nil || !strings.Contains(err.Error(), "zenity") {
		t.Fatalf("linuxPicker() without helpers = %v, want capability error mentioning zenity", err)
	}

	// A fake zenity answering with a path.
	bin := t.TempDir()
	fake := filepath.Join(bin, "zenity")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho /tmp/picked-dir\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	got, err := linuxPicker(context.Background(), "pick a folder", true)
	if err != nil || got != "/tmp/picked-dir" {
		t.Fatalf("linuxPicker() = %q, %v; want /tmp/picked-dir, nil", got, err)
	}

	// A cancelled dialog (non-zero exit) is reported as cancelled.
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := linuxPicker(context.Background(), "pick a file", false); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("linuxPicker() on cancel = %v, want cancelled error", err)
	}
}
