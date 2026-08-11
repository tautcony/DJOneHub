package firmware

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/electricbubble/gadb"
	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/transport"
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
	if id, err := service.StartEnterEDLWithMethod(context.Background(), "adb", device.Serial()); err != nil || id == "" {
		t.Fatalf("StartEnterEDLWithMethod() = %q, %v", id, err)
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

func TestStartADBRebootUsesSelectedDevice(t *testing.T) {
	device := &rebootADBDevice{mode: make(chan string, 1)}
	service := NewService(nil, operation.NewManager(nil), nil, Config{})
	service.adbList = func() ([]ADBDevice, error) { return []ADBDevice{device}, nil }
	if id, err := service.StartADBReboot(context.Background(), device.Serial()); err != nil || id == "" {
		t.Fatalf("StartADBReboot() = %q, %v", id, err)
	}
	select {
	case mode := <-device.mode:
		if mode != "" {
			t.Fatalf("Reboot() mode = %q, want normal mode", mode)
		}
	case <-time.After(time.Second):
		t.Fatal("Reboot() was not called")
	}
}

type resetOnlyFirehose struct {
	reset chan struct{}
	read  chan struct{}
}

type successfulBackupFirehose struct {
	reset chan struct{}
}

type failingBackupFirehose struct {
	readErr  error
	resetErr error
	resets   int
}

func (f *failingBackupFirehose) ReadNAND(context.Context, device.Candidate, transport.FirehoseReadRequest) (transport.FirehoseReadResult, error) {
	return transport.FirehoseReadResult{}, f.readErr
}
func (f *failingBackupFirehose) Reset(context.Context, device.Candidate) error {
	f.resets++
	return f.resetErr
}

type reconnectingEDLPort struct {
	locationSeen string
}

func (p *reconnectingEDLPort) EnterEDL(context.Context, device.Candidate) error { return nil }
func (p *reconnectingEDLPort) FindEDL(context.Context, device.Candidate) (device.Candidate, error) {
	return device.Candidate{Identity: device.Identity{PhysicalLocation: "usb/1-2", VendorID: "05c6", ProductID: "9008"}}, nil
}
func (p *reconnectingEDLPort) FindOriginal(_ context.Context, original device.Candidate) (device.Candidate, error) {
	p.locationSeen = original.Identity.PhysicalLocation
	return device.Candidate{Identity: device.Identity{PhysicalLocation: p.locationSeen, VendorID: "2c7c", ProductID: "0125"}}, nil
}
func (p *reconnectingEDLPort) ObserveEDL(context.Context, device.Candidate) (device.EDLObservation, error) {
	return device.EDLObservation{}, nil
}

type coldStartEDLPort struct{}

func (coldStartEDLPort) EnterEDL(context.Context, device.Candidate) error { return nil }
func (coldStartEDLPort) FindEDL(context.Context, device.Candidate) (device.Candidate, error) {
	return device.Candidate{Identity: device.Identity{StableID: "edl/test", PhysicalLocation: "usb/1-2", VendorID: "05c6", ProductID: "9008"}}, nil
}
func (coldStartEDLPort) FindOriginal(context.Context, device.Candidate) (device.Candidate, error) {
	return device.Candidate{}, nil
}
func (coldStartEDLPort) ObserveEDL(context.Context, device.Candidate) (device.EDLObservation, error) {
	return device.EDLObservation{State: device.EDLStateSaharaIdentified, Protocol: "sahara", Source: "usb", SerialNumber: "12345678", HardwareID: "0102030405060708", PKHash: "aabbccdd", SBLVersion: "00000001"}, nil
}

func (f *successfulBackupFirehose) ReadNAND(context.Context, device.Candidate, transport.FirehoseReadRequest) (transport.FirehoseReadResult, error) {
	return transport.FirehoseReadResult{OutputPath: "/tmp/backup.bin", Bytes: 4096, Valid: true}, nil
}

func (f *successfulBackupFirehose) Reset(context.Context, device.Candidate) error {
	close(f.reset)
	return nil
}

func TestSuccessfulBackupDoesNotReset(t *testing.T) {
	firehose := &successfulBackupFirehose{reset: make(chan struct{})}
	service := NewService(nil, nil, nil, Config{})
	err := service.backupWithFirehose(context.Background(), firehose, "/tmp/backup.bin", "", edlInvocation{}, func(int, string) {}, nil)
	if err != nil {
		t.Fatalf("backupWithFirehose() error = %v", err)
	}
	select {
	case <-firehose.reset:
		t.Fatal("successful backup called Reset")
	default:
	}
}

func TestFailedBackupAttemptsOneCleanupReset(t *testing.T) {
	firehose := &failingBackupFirehose{readErr: context.Canceled, resetErr: errors.New("reset failed")}
	service := NewService(nil, nil, nil, Config{})
	err := service.backupWithFirehose(context.Background(), firehose, "/tmp/backup.bin", "", edlInvocation{}, func(int, string) {}, nil)
	if firehose.resets != 1 {
		t.Fatalf("cleanup resets=%d, want 1", firehose.resets)
	}
	var structured *derrors.Error
	if !errors.As(err, &structured) || structured.Code != derrors.OperationCancelled || structured.Details["reconnect_required"] != true {
		t.Fatalf("backup error=%+v", err)
	}
}

func (f *resetOnlyFirehose) ReadNAND(context.Context, device.Candidate, transport.FirehoseReadRequest) (transport.FirehoseReadResult, error) {
	close(f.read)
	return transport.FirehoseReadResult{}, nil
}

func (f *resetOnlyFirehose) Reset(context.Context, device.Candidate) error {
	close(f.reset)
	return nil
}

func TestStartResetDoesNotReadNAND(t *testing.T) {
	firehose := &resetOnlyFirehose{reset: make(chan struct{}), read: make(chan struct{})}
	ops := operation.NewManager(nil)
	service := NewService(nil, ops, nil, Config{Firehose: firehose})
	id, err := service.StartReset(context.Background())
	if err != nil || id == "" {
		t.Fatalf("StartReset() = %q, %v", id, err)
	}
	select {
	case <-firehose.reset:
	case <-time.After(time.Second):
		t.Fatal("Firehose reset was not called")
	}
	select {
	case <-firehose.read:
		t.Fatal("standalone reset called ReadNAND")
	default:
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status, ok := ops.Get(id)
		if ok && status.State == operation.Succeeded {
			if status.Type != "device_control.reset" {
				t.Fatalf("operation type = %q", status.Type)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("reset operation did not succeed")
}

func TestStartResetVerifiesSameLocationReconnectFromColdEDL(t *testing.T) {
	port := &reconnectingEDLPort{}
	firehose := &resetOnlyFirehose{reset: make(chan struct{}), read: make(chan struct{})}
	ops := operation.NewManager(nil)
	service := NewService(nil, ops, nil, Config{Firehose: firehose, EDLPort: port})
	id, err := service.StartReset(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status, ok := ops.Get(id)
		if ok && status.State == operation.Succeeded {
			if port.locationSeen != "usb/1-2" {
				t.Fatalf("reconnect location=%q", port.locationSeen)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("reset operation did not verify reconnect")
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
	service.lastFirmware.Value = "cached-normal-mode-revision"
	service.lastFirmware.Source = "AT+QGMR"
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Mode != "edl" || !status.Available || status.USBID != "05C6:9008" {
		t.Fatalf("Status() = %+v, want detected EDL device", status)
	}
	if status.Firmware != "" || status.FirmwareVersionSource != "" {
		t.Fatalf("EDL status exposed cached firmware: %+v", status)
	}
}

func TestStatusObservesColdStartEDLWithoutNormalModeCache(t *testing.T) {
	service := NewService(nil, nil, nil, Config{
		DetectEDL: func(context.Context) (bool, error) { return true, nil },
		EDLPort:   coldStartEDLPort{},
	})
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.EDL == nil || status.EDL.State != device.EDLStateSaharaIdentified || status.EDL.SBLVersion != "00000001" {
		t.Fatalf("EDL observation=%+v", status.EDL)
	}
	if status.EDL.ObservedAt.IsZero() {
		t.Fatal("cold-start EDL observation has no timestamp")
	}
	if status.EDL.SerialNumber != "****5678" || status.EDL.HardwareID != "****0708" || status.EDL.PKHash != "****ccdd" {
		t.Fatalf("public identifiers are not masked: %+v", status.EDL)
	}
	if status.Firmware != "" {
		t.Fatalf("cold-start EDL invented firmware=%q", status.Firmware)
	}
}

func TestDetectedEDLPlaceholderDoesNotSuppressLiveObservation(t *testing.T) {
	service := NewService(nil, nil, nil, Config{
		DetectEDL: func(context.Context) (bool, error) { return true, nil },
		EDLPort:   coldStartEDLPort{},
	})
	service.lastNormalCandidate = device.Candidate{Identity: device.Identity{PhysicalLocation: "usb/1-2"}}
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.EDL == nil || status.EDL.State != device.EDLStateSaharaIdentified || status.EDL.SBLVersion == "" {
		t.Fatalf("live observation was suppressed: %+v", status.EDL)
	}
}

func TestReusableEDLObservationRequiresFreshProtocolFacts(t *testing.T) {
	now := time.Now()
	if reusableEDLObservation(device.EDLObservation{State: device.EDLStateDetected, ObservedAt: now}, now) {
		t.Fatal("detected placeholder was reusable")
	}
	// recovery 观察按 spec 不复用: 每次过期后都必须重新探测。
	if reusableEDLObservation(device.EDLObservation{State: device.EDLStateRecoveryRequired, ObservedAt: now}, now) {
		t.Fatal("recovery observation was reusable")
	}
	if reusableEDLObservation(device.EDLObservation{State: device.EDLStateSaharaIdentified, ObservedAt: now.Add(-time.Minute)}, now) {
		t.Fatal("stale Sahara observation was reusable")
	}
	if !reusableEDLObservation(device.EDLObservation{State: device.EDLStateSaharaIdentified, ObservedAt: now}, now) {
		t.Fatal("fresh Sahara observation was not reusable")
	}
	// 操作记录的状态在更长的窗口内复用, 防止轮询立即擦掉操作结论。
	if !reusableEDLObservation(device.EDLObservation{State: device.EDLStateBackupSucceeded, ObservedAt: now}, now) {
		t.Fatal("fresh operation state was not reusable")
	}
	if reusableEDLObservation(device.EDLObservation{State: device.EDLStateBackupSucceeded, ObservedAt: now.Add(-time.Minute)}, now) {
		t.Fatal("stale operation state was reusable")
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

func TestValidateEDLInvocationExplainsMissingPythonPackages(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is unavailable")
	}
	script := filepath.Join(t.TempDir(), "edl.py")
	if err := os.WriteFile(script, []byte("import djonehub_missing_edl_dependency\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = validateEDLInvocation(context.Background(), edlInvocation{command: python, prefix: []string{script}, dir: filepath.Dir(script)})
	if err == nil || !strings.Contains(err.Error(), "select the uv runner") {
		t.Fatalf("validateEDLInvocation() error = %v", err)
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
