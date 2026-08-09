package firmware

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/electricbubble/gadb"
	"github.com/iniwex5/vohive/internal/application/operation"
)

// recordingATExecutor answers a fixed USB composition and records every
// command issued by the service.
type recordingATExecutor struct {
	mu        sync.Mutex
	calls     []string
	restarted chan struct{}
}

func (e *recordingATExecutor) Execute(_ context.Context, command string) (string, error) {
	e.mu.Lock()
	e.calls = append(e.calls, command)
	e.mu.Unlock()
	if command == "AT+CFUN=1,1" {
		select {
		case e.restarted <- struct{}{}:
		default:
		}
	}
	if strings.HasPrefix(command, `AT+QCFG="usbcfg"?`) {
		return `+QCFG: "usbcfg",0x2C7C,0x0125,1,1,1,1,1,0,0` + "\r\nOK\r\n", nil
	}
	return "OK\r\n", nil
}

func (e *recordingATExecutor) snapshot() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.calls)
}

func TestStartADBModeRestartsModemAfterUSBComposition(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		executor := &recordingATExecutor{restarted: make(chan struct{}, 1)}
		service := NewService(executor, operation.NewManager(nil), nil, Config{})
		if id, err := service.StartADBMode(context.Background(), enabled); err != nil || id == "" {
			t.Fatalf("StartADBMode(%v) = %q, %v", enabled, id, err)
		}
		select {
		case <-executor.restarted:
		case <-time.After(2 * time.Second):
			t.Fatalf("StartADBMode(%v) did not restart the modem; calls = %q", enabled, executor.snapshot())
		}
		calls := executor.snapshot()
		writeAt := slices.IndexFunc(calls, func(call string) bool {
			return strings.HasPrefix(call, `AT+QCFG="usbcfg",`)
		})
		if writeAt == -1 {
			t.Fatalf("StartADBMode(%v) calls = %q, want a USB composition write", enabled, calls)
		}
		if !slices.Contains(calls[writeAt+1:], "AT+CFUN=1,1") {
			t.Fatalf("StartADBMode(%v) calls = %q, want the restart after the composition write", enabled, calls)
		}
	}
}

type rebootADBDevice struct {
	mode chan string
}

func (d *rebootADBDevice) Serial() string                                    { return "test-device" }
func (d *rebootADBDevice) State() (gadb.DeviceState, error)                  { return gadb.StateOnline, nil }
func (d *rebootADBDevice) RunShellCommand(string, ...string) (string, error) { return "", nil }
func (d *rebootADBDevice) Reboot(mode string) error                          { d.mode <- mode; return nil }
func (d *rebootADBDevice) OpenShell() (io.ReadWriteCloser, error)            { return nil, nil }

func TestEnterEDLUsesADBRebootService(t *testing.T) {
	device := &rebootADBDevice{mode: make(chan string, 1)}
	service := NewService(nil, operation.NewManager(nil), nil, Config{})
	service.adbList = func() ([]ADBDevice, error) { return []ADBDevice{device}, nil }
	if id, err := service.StartEnterEDL(context.Background(), device.Serial()); err != nil || id == "" {
		t.Fatalf("StartEnterEDL() = %q, %v", id, err)
	}
	select {
	case mode := <-device.mode:
		if mode != "edl" {
			t.Fatalf("Reboot() mode = %q, want edl", mode)
		}
	case <-time.After(time.Second):
		t.Fatal("Reboot() was not called")
	}
}

func TestStartUSBIDDoesNotRequireADBUnlockSerial(t *testing.T) {
	service := NewService(nil, operation.NewManager(nil), nil, Config{})
	id, err := service.StartUSBID(context.Background(), USBIDRequest{VID: "2CA3", PID: "4006"})
	if err != nil || id == "" {
		t.Fatalf("StartUSBID() = %q, %v; want accepted operation without serial confirmation", id, err)
	}
}

func TestStatusDetectsEDLWithoutATChannel(t *testing.T) {
	service := NewService(nil, nil, nil, Config{DetectEDL: func(context.Context) (bool, error) {
		return true, nil
	}})
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Mode != "edl" || !status.Available || status.USBID != "05C6:9008" {
		t.Fatalf("Status() = %+v, want detected EDL device", status)
	}
}

func TestEDLInvocationSupportsPythonAndUV(t *testing.T) {
	edlDirectory := t.TempDir()
	edlScript := filepath.Join(edlDirectory, "edl.py")
	if err := os.WriteFile(edlScript, []byte("#!/usr/bin/env python3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(edlDirectory, "pyproject.toml"), []byte("[project.scripts]\nedl = \"edlclient.edl:run\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	python, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(nil, nil, nil, Config{EDLPython: python})

	pythonInvocation, err := service.edlInvocation(edlDirectory, "python")
	if err != nil {
		t.Fatal(err)
	}
	if pythonInvocation.command != python || pythonInvocation.dir != edlDirectory || !slices.Equal(pythonInvocation.prefix, []string{edlScript}) {
		t.Fatalf("Python invocation = %+v", pythonInvocation)
	}

	binDirectory := t.TempDir()
	uvName := "uv"
	uvContents := []byte("#!/bin/sh\n")
	if runtime.GOOS == "windows" {
		uvName = "uv.cmd"
		uvContents = []byte("@echo off\r\n")
	}
	uvPath := filepath.Join(binDirectory, uvName)
	if err := os.WriteFile(uvPath, uvContents, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))
	uvInvocation, err := service.edlInvocation(edlDirectory, "uv")
	if err != nil {
		t.Fatal(err)
	}
	if uvInvocation.command != uvPath || uvInvocation.dir != edlDirectory || !slices.Equal(uvInvocation.prefix, []string{"run", "edl"}) {
		t.Fatalf("uv invocation = %+v", uvInvocation)
	}
}

func TestCleanTerminalOutputRemovesEscapeSequences(t *testing.T) {
	input := "\x1b[2K\rProgress: |###| 42.0% Read\x1b[?25l\rDone\n"
	want := "\rProgress: |###| 42.0% Read\rDone\n"
	if got := cleanTerminalOutput(input); got != want {
		t.Fatalf("cleanTerminalOutput() = %q, want %q", got, want)
	}
}

func TestEDLArgsLetsEDLDetectLoaderByDefault(t *testing.T) {
	args := edlArgs("")
	if slices.ContainsFunc(args, func(arg string) bool { return len(arg) >= 9 && arg[:9] == "--loader=" }) {
		t.Fatalf("edlArgs(\"\") = %q, want no loader argument", args)
	}
	if !slices.Contains(args, "--memory=NAND") {
		t.Fatalf("edlArgs(\"\") = %q, want NAND memory argument", args)
	}
}

func TestEDLArgsKeepsExplicitLoaderOverride(t *testing.T) {
	args := edlArgs("/tmp/firehose.mbn")
	if !slices.Contains(args, "--loader=/tmp/firehose.mbn") {
		t.Fatalf("edlArgs(loader) = %q, want explicit loader argument", args)
	}
}

func TestADBUnlockCodeMatchesUnlockUtility(t *testing.T) {
	if got, want := adbUnlockCode("3a6c06f6"), "giU31d2RnUcM6Bap"; got != want {
		t.Fatalf("adbUnlockCode() = %q, want %q", got, want)
	}
}

func TestParseUSBConfigPreservesCurrentComposition(t *testing.T) {
	config, ok := parseUSBConfig("+QCFG: \"usbcfg\",0x2C7C,0x0125,1,1,1,1,1,0,0\r\nOK\r\n")
	if !ok {
		t.Fatal("parseUSBConfig() returned false")
	}
	if config.vid != "0x2C7C" || config.pid != "0x0125" {
		t.Fatalf("USB ID = %s:%s", config.vid, config.pid)
	}
	if got := config.withID("0x2CA3", "0x4006"); got != `AT+QCFG="usbcfg",0x2CA3,0x4006,1,1,1,1,1,0,0` {
		t.Fatalf("withID() = %q", got)
	}
	if got, err := config.withADB(true); err != nil || got != `AT+QCFG="usbcfg",0x2C7C,0x0125,1,1,1,1,1,1,0` {
		t.Fatalf("withADB(true) = %q, %v", got, err)
	}
	if enabled, known := config.adbEnabled(); !known || enabled {
		t.Fatalf("adbEnabled() = %v, %v; want false, true", enabled, known)
	}
	fields := config.fieldsForStatus()
	if len(fields) != 7 {
		t.Fatalf("fieldsForStatus() returned %d fields, want 7", len(fields))
	}
	if fields[5].Key != "adb" || fields[5].Value != "0" {
		t.Fatalf("ADB field = %#v, want key adb and value 0", fields[5])
	}
	if fields[6].Key != "uac" || fields[6].Value != "0" {
		t.Fatalf("UAC field = %#v, want key uac and value 0", fields[6])
	}
}

func TestUSBConfigFieldsForStatus(t *testing.T) {
	config, ok := parseUSBConfig(`+QCFG: "usbcfg",0x2CA3,0x4006,1,1,1,1,1,1,0`)
	if !ok {
		t.Fatal("parseUSBConfig() returned false")
	}
	fields := config.fieldsForStatus()
	want := []struct {
		key, value string
	}{
		{"diag", "1"}, {"nmea", "1"}, {"at", "1"}, {"modem", "1"},
		{"usbnet", "1"}, {"adb", "1"}, {"uac", "0"},
	}
	for index, item := range want {
		if fields[index].Index != index+1 || fields[index].Key != item.key || fields[index].Value != item.value {
			t.Fatalf("field %d = %#v, want index=%d key=%q value=%q", index, fields[index], index+1, item.key, item.value)
		}
	}
}

func TestNormalizeUSBID(t *testing.T) {
	for input, want := range map[string]string{"2ca3": "0x2CA3", "0X0125": "0x0125", "f": "0x000F"} {
		got, err := normalizeUSBID(input)
		if err != nil || got != want {
			t.Fatalf("normalizeUSBID(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := normalizeUSBID("10000"); err == nil {
		t.Fatal("normalizeUSBID accepted a five-digit value")
	}
}
