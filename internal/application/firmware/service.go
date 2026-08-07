package firmware

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/electricbubble/gadb"
	"github.com/iniwex5/vohive/internal/application/operation"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/runtime"
	"github.com/iniwex5/vohive/internal/storage"
)

type ATExecutor interface {
	Execute(context.Context, string) (string, error)
}

type ADBDevice interface {
	Serial() string
	State() (gadb.DeviceState, error)
	RunShellCommand(string, ...string) (string, error)
	Reboot(string) error
	OpenShell() (io.ReadWriteCloser, error)
}

type ADBLister func() ([]ADBDevice, error)

type Config struct {
	EDLCommand string
	EDLScript  string
	EDLPython  string
	DetectEDL  func(context.Context) (bool, error)
	ADBCommand string
	// Store persists user-tunable settings (e.g. the adb command). A nil
	// store leaves settings in their default state.
	Store storage.ValueStore
}

// Settings is the JSON document persisted in the firmware settings namespace.
type Settings struct {
	ADBCommand string `json:"adb_command,omitempty"`
}

func ConfigFromEnvironment() Config {
	return Config{
		EDLCommand: strings.TrimSpace(os.Getenv("DJONEHUB_EDL_COMMAND")),
		EDLScript:  strings.TrimSpace(os.Getenv("DJI_FW_EDL_SCRIPT")),
		EDLPython:  strings.TrimSpace(os.Getenv("DJI_FW_EDL_PYTHON")),
		ADBCommand: strings.TrimSpace(os.Getenv("DJONEHUB_ADB_COMMAND")),
	}
}

type Service struct {
	at       ATExecutor
	ops      *operation.Manager
	runtime  *runtime.Runtime
	config   Config
	settings Settings
	adbList  ADBLister
	mu       sync.Mutex
	// statusCache 是固件状态短 TTL 缓存 (design D17): 读取不重复运行
	// AT + ADB 探测序列。
	statusCacheMu sync.Mutex
	cachedStatus  *Status
	cachedAt      time.Time
}

// firmwareStatusCacheTTL 是固件状态缓存的存活时间。
const firmwareStatusCacheTTL = 1500 * time.Millisecond

func NewService(at ATExecutor, ops *operation.Manager, rt *runtime.Runtime, config Config) *Service {
	service := &Service{at: at, ops: ops, runtime: rt, config: config}
	if config.Store != nil {
		_ = config.Store.Read(&service.settings)
	}
	service.adbList = func() ([]ADBDevice, error) {
		command, _ := service.adbCommandConfig()
		return listADBDevices(command)
	}
	return service
}

// adbCommandConfig returns the adb command used to start the local adb
// server together with its origin. An environment-configured command wins
// over the value saved from the UI; an empty command means `adb` from PATH.
func (s *Service) adbCommandConfig() (command, source string) {
	if command = strings.TrimSpace(s.config.ADBCommand); command != "" {
		return command, "env"
	}
	s.mu.Lock()
	command = strings.TrimSpace(s.settings.ADBCommand)
	s.mu.Unlock()
	if command != "" {
		return command, "saved"
	}
	return "", "default"
}

// ADBCommandConfig exposes the effective adb command and its origin to the
// HTTP layer so the UI can render where the value came from.
func (s *Service) ADBCommandConfig() (command, source string) {
	return s.adbCommandConfig()
}

// SetADBCommand persists the adb command used to start the local adb server.
// An empty command clears the saved value; DJONEHUB_ADB_COMMAND still wins
// while it is set. A non-empty command must resolve to an executable.
func (s *Service) SetADBCommand(ctx context.Context, command string) error {
	command = strings.TrimSpace(command)
	if s.config.Store == nil {
		return derrors.New(derrors.CapabilityNotSupported, "firmware settings are unavailable", false, nil)
	}
	if command != "" {
		if _, err := exec.LookPath(command); err != nil {
			return derrors.New(derrors.InvalidRequest, "adb command not found", false, map[string]any{"command": command})
		}
	}
	s.mu.Lock()
	s.settings.ADBCommand = command
	err := s.config.Store.Write(&s.settings)
	s.mu.Unlock()
	if err != nil {
		return derrors.New(derrors.Internal, "unable to save the adb command", true, map[string]any{"cause": err.Error()})
	}
	return nil
}

type Status struct {
	Available       bool             `json:"available"`
	Manufacturer    string           `json:"manufacturer,omitempty"`
	Model           string           `json:"model,omitempty"`
	Firmware        string           `json:"firmware,omitempty"`
	ADBKeySerial    string           `json:"adb_key_serial,omitempty"`
	USBConfig       string           `json:"usb_config,omitempty"`
	USBConfigFields []USBConfigField `json:"usb_config_fields,omitempty"`
	USBID           string           `json:"usb_id,omitempty"`
	USBVID          string           `json:"usb_vid,omitempty"`
	USBPID          string           `json:"usb_pid,omitempty"`
	Mode            string           `json:"mode"`
	ModeReason      string           `json:"mode_reason,omitempty"`
	ADB             ADBStatus        `json:"adb"`
	Backup          BackupStatus     `json:"backup"`
}

type USBConfigField struct {
	Index int    `json:"index"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

type ADBStatus struct {
	Enabled         bool              `json:"enabled"`
	EnabledKnown    bool              `json:"enabled_known"`
	ServerAvailable bool              `json:"server_available"`
	Connected       bool              `json:"connected"`
	Serial          string            `json:"serial,omitempty"`
	State           string            `json:"state,omitempty"`
	Error           string            `json:"error,omitempty"`
	Command         string            `json:"command,omitempty"`
	CommandSource   string            `json:"command_source,omitempty"`
	Devices         []ADBDeviceStatus `json:"devices,omitempty"`
}

type ADBDeviceStatus struct {
	Serial string `json:"serial"`
	State  string `json:"state"`
	Online bool   `json:"online"`
}

type BackupStatus struct {
	Available  bool   `json:"available"`
	Command    string `json:"command,omitempty"`
	Script     string `json:"script,omitempty"`
	DefaultDir string `json:"default_dir,omitempty"`
}

// Status 从短 TTL 缓存提供固件状态; 缓存过期时才运行完整探测序列。
func (s *Service) Status(ctx context.Context) (Status, error) {
	s.statusCacheMu.Lock()
	if s.cachedStatus != nil && time.Since(s.cachedAt) < firmwareStatusCacheTTL {
		cached := *s.cachedStatus
		s.statusCacheMu.Unlock()
		return cached, nil
	}
	s.statusCacheMu.Unlock()
	status, err := s.statusFresh(ctx)
	if err == nil {
		s.statusCacheMu.Lock()
		copy := status
		s.cachedStatus = &copy
		s.cachedAt = time.Now()
		s.statusCacheMu.Unlock()
	}
	return status, err
}

func (s *Service) statusFresh(ctx context.Context) (Status, error) {
	status := Status{Mode: "unknown", Backup: s.backupStatus()}
	if s.config.DetectEDL != nil {
		detected, err := s.config.DetectEDL(ctx)
		if err != nil {
			log.Printf("firmware EDL detection failed: %v", err)
		} else if detected {
			status.Available = true
			status.Mode = "edl"
			status.ModeReason = "Qualcomm EDL device detected"
			status.USBVID = "0x05C6"
			status.USBPID = "0x9008"
			status.USBID = "05C6:9008"
			return status, nil
		}
	}
	var adbEnabled bool
	var adbEnabledKnown bool
	if s.at != nil {
		responses := map[string]string{}
		commands := []struct{ key, command string }{
			{"ati", "ATI"},
			{"firmware", "AT+CGMR"},
			{"adb_key", "AT+QADBKEY?"},
			{"usb_config", `AT+QCFG="usbcfg"?`},
		}
		var lastErr error
		for _, item := range commands {
			response, err := s.at.Execute(ctx, item.command)
			if err != nil {
				lastErr = err
				continue
			}
			responses[item.key] = response
		}
		status.Manufacturer, status.Model = parseATI(responses["ati"])
		status.Firmware = firstValue(responses["firmware"])
		status.ADBKeySerial = parseADBKeySerial(responses["adb_key"])
		status.USBConfig = firstValue(responses["usb_config"])
		if config, ok := parseUSBConfig(responses["usb_config"]); ok {
			status.USBConfig = config.raw
			status.USBConfigFields = config.fieldsForStatus()
			status.USBVID = config.vid
			status.USBPID = config.pid
			status.USBID = strings.TrimPrefix(config.vid, "0x") + ":" + strings.TrimPrefix(config.pid, "0x")
			adbEnabled, adbEnabledKnown = config.adbEnabled()
		}
		status.USBID = parseUSBID(status.USBConfig)
		status.Available = len(responses) > 0
		if !status.Available && lastErr != nil {
			return status, lastErr
		}
	}

	status.ADB = s.adbStatus()
	status.ADB.Enabled = adbEnabled
	status.ADB.EnabledKnown = adbEnabledKnown
	if status.ADB.EnabledKnown && status.ADB.Enabled {
		status.Mode = "adb"
		if status.ADB.Connected {
			status.ModeReason = "ADB is enabled and an online device is connected"
		} else {
			status.ModeReason = "ADB is enabled in the current USB composition"
		}
	} else if status.Available {
		status.Mode = "normal"
		if status.ADB.EnabledKnown {
			status.ModeReason = "ADB is disabled in the current USB composition"
		} else {
			status.ModeReason = "AT control channel is available"
		}
	} else if status.ADB.Connected {
		status.Mode = "adb"
		status.ModeReason = "ADB device is connected"
	} else if status.ADB.ServerAvailable {
		status.Mode = "unknown"
		status.ModeReason = "ADB server is available but no online device is connected"
	}
	return status, nil
}

func (s *Service) StartUnlock(ctx context.Context) (string, error) {
	if s.ops == nil {
		return "", errors.New("operation manager is unavailable")
	}
	return s.ops.Start(ctx, "firmware.adb_unlock", func(ctx context.Context, _ string, report func(int, string)) error {
		return s.unlock(ctx, report)
	})
}

func (s *Service) StartADBMode(ctx context.Context, enabled bool) (string, error) {
	if s.ops == nil {
		return "", errors.New("operation manager is unavailable")
	}
	return s.ops.Start(ctx, "firmware.adb_mode", func(ctx context.Context, _ string, report func(int, string)) error {
		if s.at == nil {
			return derrors.CapabilityMissing("raw_at", "firmware_adb_mode", "AT control channel is unavailable")
		}
		report(10, "reading current USB composition")
		config, err := s.readUSBConfig(ctx)
		if err != nil {
			return err
		}
		command, err := config.withADB(enabled)
		if err != nil {
			return err
		}
		label := "ADB disabled"
		if enabled {
			label = "ADB enabled"
		}
		report(35, "writing USB composition")
		if _, err := s.at.Execute(ctx, command); err != nil {
			return err
		}
		report(100, label)
		return nil
	})
}

func (s *Service) StartEnterEDL(ctx context.Context, serial string) (string, error) {
	serial = strings.TrimSpace(serial)
	if serial == "" {
		return "", derrors.New(derrors.InvalidRequest, "an ADB device must be selected", false, nil)
	}
	if s.ops == nil {
		return "", errors.New("operation manager is unavailable")
	}
	return s.ops.Start(ctx, "firmware.enter_edl", func(ctx context.Context, _ string, report func(int, string)) error {
		device, err := s.selectOnlineADBDevice(serial)
		if err != nil {
			return err
		}
		report(20, "sending reboot edl")
		if err := device.Reboot("edl"); err != nil {
			return fmt.Errorf("ADB reboot to EDL failed: %w", err)
		}
		report(100, "EDL reboot requested")
		return nil
	})
}

func (s *Service) OpenADBShell(serial string) (io.ReadWriteCloser, error) {
	serial = strings.TrimSpace(serial)
	if serial == "" {
		return nil, derrors.New(derrors.InvalidRequest, "an ADB device must be selected", false, nil)
	}
	device, err := s.selectOnlineADBDevice(serial)
	if err != nil {
		return nil, err
	}
	return device.OpenShell()
}

func (s *Service) selectOnlineADBDevice(serial string) (ADBDevice, error) {
	devices, err := s.adbList()
	if err != nil {
		return nil, derrors.New(derrors.TransportUnavailable, "ADB server is unavailable", true, map[string]any{"cause": err.Error()})
	}
	for _, device := range devices {
		if device.Serial() != serial {
			continue
		}
		state, stateErr := device.State()
		if stateErr != nil {
			return nil, derrors.New(derrors.DeviceOffline, "the selected ADB device cannot be reached", true, map[string]any{"serial": serial, "cause": stateErr.Error()})
		}
		if state != gadb.StateOnline {
			return nil, derrors.New(derrors.DeviceOffline, "the selected ADB device is not online", true, map[string]any{"serial": serial, "state": string(state)})
		}
		return device, nil
	}
	return nil, derrors.New(derrors.DeviceOffline, "the selected ADB device is no longer connected", true, map[string]any{"serial": serial})
}

type BackupRequest struct {
	OutputPath string `json:"output_path"`
	LoaderPath string `json:"loader_path"`
	EDLPath    string `json:"edl_path"`
	EDLRunner  string `json:"edl_runner"`
}

func (s *Service) StartBackup(ctx context.Context, request BackupRequest) (string, error) {
	output, err := resolveOutputPath(request.OutputPath)
	if err != nil {
		return "", err
	}
	loader := strings.TrimSpace(request.LoaderPath)
	if loader != "" {
		if _, err := os.Stat(loader); err != nil {
			return "", derrors.New(derrors.InvalidRequest, "loader_path does not exist", false, map[string]any{"path": loader})
		}
	}
	if _, err := os.Stat(output); err == nil {
		return "", derrors.New(derrors.InvalidRequest, "output_path already exists", false, map[string]any{"path": output})
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if s.ops == nil {
		return "", errors.New("operation manager is unavailable")
	}
	invocation, err := s.edlInvocation(request.EDLPath, request.EDLRunner)
	if err != nil {
		return "", err
	}
	var operationID string
	ready := make(chan struct{})
	var startErr error
	operationID, startErr = s.ops.Start(ctx, "firmware.backup", func(ctx context.Context, _ string, report func(int, string)) error {
		<-ready
		return s.backup(ctx, output, loader, invocation, report, func(message string) {
			s.ops.Log(operationID, message)
		})
	})
	if startErr != nil {
		return "", startErr
	}
	close(ready)
	return operationID, nil
}

// SelectBackupDirectory opens the platform directory picker when available.
// The headless fallback returns the service's conventional backup directory.
func (s *Service) SelectBackupDirectory(ctx context.Context) (string, error) {
	defaultDir := s.backupStatus().DefaultDir
	return selectDirectory(ctx, "Choose firmware backup directory", defaultDir)
}

// SelectEDLDirectory opens a picker for the bkerler/edl project directory.
func (s *Service) SelectEDLDirectory(ctx context.Context) (string, error) {
	return selectDirectory(ctx, "Choose EDL tool directory", "")
}

// SelectADBFile opens a file picker for the adb executable.
func (s *Service) SelectADBFile(ctx context.Context) (string, error) {
	return selectFile(ctx, "Choose the adb executable", "")
}

// selectDirectory opens the platform directory chooser. Platforms without a
// native picker fall back to the conventional value.
func selectDirectory(ctx context.Context, prompt, fallback string) (string, error) {
	switch goruntime.GOOS {
	case "darwin":
		return macPicker(ctx, prompt, "folder")
	case "linux":
		return linuxPicker(ctx, prompt, true)
	default:
		return fallback, nil
	}
}

// selectFile opens the platform file chooser. Platforms without a native
// picker fall back to the conventional value.
func selectFile(ctx context.Context, prompt, fallback string) (string, error) {
	switch goruntime.GOOS {
	case "darwin":
		return macPicker(ctx, prompt, "file")
	case "linux":
		return linuxPicker(ctx, prompt, false)
	default:
		return fallback, nil
	}
}

// macPicker wraps an osascript choose folder/file dialog.
func macPicker(ctx context.Context, prompt, kind string) (string, error) {
	verb := "folder"
	if kind == "file" {
		verb = "file"
	}
	script := `POSIX path of (choose ` + verb + ` with prompt "` + strings.ReplaceAll(prompt, `"`, "") + `")`
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	output, err := cmd.Output()
	if err != nil {
		return "", derrors.New(derrors.InvalidRequest, "file selection was cancelled", false, nil)
	}
	path := strings.TrimSpace(string(output))
	if path == "" {
		return "", derrors.New(derrors.InvalidRequest, "file selection returned an empty path", false, nil)
	}
	return filepath.Clean(path), nil
}

// linuxPicker opens the graphical chooser through zenity (GNOME) or kdialog
// (KDE). If neither desktop helper is installed the caller gets a capability
// error instead of a silent fallback.
func linuxPicker(ctx context.Context, prompt string, directory bool) (string, error) {
	choices := [][]string{
		{"zenity", "--file-selection", "--title=" + prompt},
		{"kdialog", "--getopenfilename", "."},
	}
	if directory {
		choices = [][]string{
			{"zenity", "--file-selection", "--directory", "--title=" + prompt},
			{"kdialog", "--getexistingdirectory", "."},
		}
	}
	for _, argv := range choices {
		if _, err := exec.LookPath(argv[0]); err != nil {
			continue
		}
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		output, err := cmd.Output()
		if err != nil {
			return "", derrors.New(derrors.InvalidRequest, "file selection was cancelled", false, nil)
		}
		path := strings.TrimSpace(string(output))
		if path == "" {
			return "", derrors.New(derrors.InvalidRequest, "file selection returned an empty path", false, nil)
		}
		return filepath.Clean(path), nil
	}
	return "", derrors.New(derrors.CapabilityNotSupported, "no graphical file picker is available (install zenity or kdialog)", false, nil)
}

func (s *Service) unlock(ctx context.Context, report func(int, string)) error {
	if s.at == nil {
		return derrors.CapabilityMissing("raw_at", "firmware_adb_unlock", "AT control channel is unavailable")
	}
	report(10, "reading ADB key serial")
	response, err := s.at.Execute(ctx, "AT+QADBKEY?")
	if err != nil {
		return err
	}
	serial := parseADBKeySerial(response)
	if serial == "" {
		return derrors.New(derrors.InvalidRequest, "the modem did not return an ADB key serial", false, nil)
	}
	code := adbUnlockCode(serial)
	report(45, "writing ADB unlock key")
	if _, err := s.at.Execute(ctx, `AT+QADBKEY="`+code+`"`); err != nil {
		return err
	}
	report(70, "reading current USB composition")
	config, err := s.readUSBConfig(ctx)
	if err != nil {
		return err
	}
	command, err := config.withADB(true)
	if err != nil {
		return err
	}
	report(82, "enabling ADB USB composition")
	if _, err := s.at.Execute(ctx, command); err != nil {
		return err
	}
	report(100, "ADB unlocked")
	return nil
}

type USBIDRequest struct {
	VID string `json:"vid"`
	PID string `json:"pid"`
}

func (s *Service) StartUSBID(ctx context.Context, request USBIDRequest) (string, error) {
	vid, err := normalizeUSBID(request.VID)
	if err != nil {
		return "", derrors.New(derrors.InvalidRequest, "vid must be a four-digit hexadecimal value", false, nil)
	}
	pid, err := normalizeUSBID(request.PID)
	if err != nil {
		return "", derrors.New(derrors.InvalidRequest, "pid must be a four-digit hexadecimal value", false, nil)
	}
	if s.ops == nil {
		return "", errors.New("operation manager is unavailable")
	}
	return s.ops.Start(ctx, "firmware.usb_id", func(ctx context.Context, _ string, report func(int, string)) error {
		if s.at == nil {
			return derrors.CapabilityMissing("raw_at", "firmware_usb_id", "AT control channel is unavailable")
		}
		report(15, "reading current USB composition")
		config, err := s.readUSBConfig(ctx)
		if err != nil {
			return err
		}
		command := config.withID(vid, pid)
		report(45, "writing USB ID")
		if _, err := s.at.Execute(ctx, command); err != nil {
			return err
		}
		report(70, "restarting modem")
		if _, err := s.at.Execute(ctx, "AT+CFUN=1,1"); err != nil {
			return err
		}
		report(100, "USB ID updated; reconnect required")
		return nil
	})
}

type usbConfig struct {
	vid    string
	pid    string
	fields []string
	raw    string
}

var usbConfigFieldKeys = []string{"diag", "nmea", "at", "modem", "usbnet", "adb", "uac"}

func (c usbConfig) fieldsForStatus() []USBConfigField {
	fields := make([]USBConfigField, 0, len(usbConfigFieldKeys))
	for index, key := range usbConfigFieldKeys {
		value := ""
		if index < len(c.fields) {
			value = c.fields[index]
		}
		fields = append(fields, USBConfigField{Index: index + 1, Key: key, Value: value})
	}
	return fields
}

func (c usbConfig) adbEnabled() (bool, bool) {
	if len(c.fields) < 6 {
		return false, false
	}
	switch strings.TrimSpace(c.fields[5]) {
	case "1":
		return true, true
	case "0":
		return false, true
	default:
		return false, false
	}
}

func (s *Service) readUSBConfig(ctx context.Context) (usbConfig, error) {
	response, err := s.at.Execute(ctx, `AT+QCFG="usbcfg"?`)
	if err != nil {
		return usbConfig{}, err
	}
	config, ok := parseUSBConfig(response)
	if !ok {
		return usbConfig{}, derrors.New(derrors.InvalidRequest, "the modem returned an invalid USB composition", false, nil)
	}
	return config, nil
}

func (c usbConfig) withADB(enabled bool) (string, error) {
	if len(c.fields) < 6 {
		return "", derrors.New(derrors.InvalidRequest, "the current USB composition has no ADB control field", false, nil)
	}
	fields := append([]string(nil), c.fields...)
	if enabled {
		fields[5] = "1"
	} else {
		fields[5] = "0"
	}
	return `AT+QCFG="usbcfg",` + c.vid + "," + c.pid + "," + strings.Join(fields, ","), nil
}

func (c usbConfig) withID(vid, pid string) string {
	return `AT+QCFG="usbcfg",` + vid + "," + pid + "," + strings.Join(c.fields, ",")
}

type edlInvocation struct {
	command string
	prefix  []string
	dir     string
}

func (s *Service) backup(ctx context.Context, output, loader string, invocation edlInvocation, report func(int, string), logOutput func(string)) error {
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	args := append(append([]string(nil), invocation.prefix...), "rf", output)
	args = append(args, edlArgs(loader)...)
	report(5, "waiting for EDL device")
	writer := &progressWriter{report: report, logOutput: logOutput}
	cmd := exec.CommandContext(ctx, invocation.command, args...)
	cmd.Dir = invocation.dir
	cmd.Stdout = writer
	cmd.Stderr = writer
	commandErr := cmd.Run()
	if commandErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	info, statErr := os.Stat(output)
	if statErr != nil || info.Size() == 0 {
		if statErr == nil {
			statErr = errors.New("backup file is empty")
		}
		if commandErr != nil {
			return fmt.Errorf("EDL backup failed: %w: %s", commandErr, strings.TrimSpace(writer.String()))
		}
		err := statErr
		return fmt.Errorf("EDL backup did not produce a valid file: %w", err)
	}
	if commandErr != nil {
		// Some EDL wrappers return a non-zero status after successfully flushing
		// the image. The completed output is authoritative in that case.
		if logOutput != nil {
			logOutput(fmt.Sprintf("EDL command exited with %v; valid backup file is present, accepting result\n", commandErr))
		}
	}
	report(100, "NAND backup completed")
	return nil
}

func (s *Service) edlInvocation(path, runner string) (edlInvocation, error) {
	path = strings.TrimSpace(path)
	if path != "" {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return edlInvocation{}, derrors.New(derrors.InvalidRequest, "invalid EDL path", false, map[string]any{"path": path})
		}
		info, err := os.Stat(absolute)
		if err != nil {
			return edlInvocation{}, derrors.New(derrors.InvalidRequest, "EDL path does not exist", false, map[string]any{"path": absolute})
		}
		directory, script := filepath.Dir(absolute), absolute
		hasProject := false
		if info.IsDir() {
			directory, script = absolute, ""
			for _, name := range []string{"edl.py", "edl"} {
				candidate := filepath.Join(absolute, name)
				if candidateInfo, candidateErr := os.Stat(candidate); candidateErr == nil && !candidateInfo.IsDir() {
					script = candidate
					break
				}
			}
			if projectInfo, projectErr := os.Stat(filepath.Join(absolute, "pyproject.toml")); projectErr == nil && !projectInfo.IsDir() {
				hasProject = true
			}
		}
		switch strings.ToLower(strings.TrimSpace(runner)) {
		case "python", "":
			if script == "" {
				return edlInvocation{}, derrors.New(derrors.InvalidRequest, "EDL Python entry file was not found", false, map[string]any{"directory": directory, "expected": []string{"edl.py", "edl"}})
			}
			python, ok := commandPath(s.config.EDLPython, "python3")
			if !ok {
				return edlInvocation{}, derrors.New(derrors.CapabilityNotSupported, "Python 3 is not available", false, nil)
			}
			return edlInvocation{command: python, prefix: []string{script}, dir: directory}, nil
		case "uv":
			uv, ok := commandPath("uv", "uv")
			if !ok {
				return edlInvocation{}, derrors.New(derrors.CapabilityNotSupported, "uv is not available", false, nil)
			}
			if hasProject {
				return edlInvocation{command: uv, prefix: []string{"run", "edl"}, dir: directory}, nil
			}
			if script == "" {
				return edlInvocation{}, derrors.New(derrors.InvalidRequest, "EDL entry was not found", false, map[string]any{"directory": directory, "expected": []string{"pyproject.toml", "edl.py", "edl"}})
			}
			return edlInvocation{command: uv, prefix: []string{"run", "python", script}, dir: directory}, nil
		default:
			return edlInvocation{}, derrors.New(derrors.InvalidRequest, "edl_runner must be python or uv", false, nil)
		}
	}

	if s.config.EDLScript != "" {
		python := s.config.EDLPython
		if python == "" {
			python = "python3"
		}
		resolved, ok := commandPath(python, "python3")
		if !ok {
			return edlInvocation{}, derrors.New(derrors.CapabilityNotSupported, "configured Python interpreter is unavailable", false, nil)
		}
		return edlInvocation{command: resolved, prefix: []string{s.config.EDLScript}, dir: filepath.Dir(s.config.EDLScript)}, nil
	}
	command := s.config.EDLCommand
	if command == "" {
		command = "edl"
	}
	parts := strings.Fields(command)
	resolved, ok := commandPath(parts[0], "edl")
	if !ok {
		return edlInvocation{}, derrors.New(derrors.CapabilityNotSupported, "EDL command is unavailable; choose an EDL tool directory", false, nil)
	}
	return edlInvocation{command: resolved, prefix: parts[1:]}, nil
}

func edlArgs(loader string) []string {
	args := []string{"--memory=NAND", "--pagesperblock=64", "--sectorsize=2048", "--vid=0x05c6", "--pid=0x9008"}
	if strings.TrimSpace(loader) != "" {
		args = append([]string{"--loader=" + loader}, args...)
	}
	return args
}

func (s *Service) backupStatus() BackupStatus {
	command := s.config.EDLCommand
	if command == "" {
		command = "edl"
	}
	available := false
	if s.config.EDLScript != "" {
		_, available = commandPath(s.config.EDLPython, "python3")
		_, scriptErr := os.Stat(s.config.EDLScript)
		available = available && scriptErr == nil
	} else {
		_, available = commandPath(strings.Fields(command)[0], "edl")
	}
	defaultDir, _ := os.UserHomeDir()
	if defaultDir != "" {
		defaultDir = filepath.Join(defaultDir, "DJOneHub", "firmware-backups")
	}
	return BackupStatus{Available: available, Command: command, Script: s.config.EDLScript, DefaultDir: defaultDir}
}

func commandPath(value, fallback string) (string, bool) {
	if strings.TrimSpace(value) == "" {
		value = fallback
	}
	path, err := exec.LookPath(strings.Fields(value)[0])
	return path, err == nil
}

type progressWriter struct {
	mu        sync.Mutex
	buffer    strings.Builder
	report    func(int, string)
	logOutput func(string)
}

var ansiEscapePattern = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))`)

func cleanTerminalOutput(value string) string {
	return ansiEscapePattern.ReplaceAllString(value, "")
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	raw := string(p)
	cleaned := cleanTerminalOutput(raw)
	w.buffer.WriteString(cleaned)
	if w.logOutput != nil {
		w.logOutput(raw)
	}
	for _, line := range strings.FieldsFunc(cleaned, func(r rune) bool { return r == '\n' || r == '\r' }) {
		match := regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)%`).FindStringSubmatch(line)
		if len(match) == 2 {
			value, _ := strconv.ParseFloat(match[1], 64)
			w.report(int(value), strings.TrimSpace(line))
		}
	}
	return len(p), nil
}

func (w *progressWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.String()
}

// adbServerRetryInterval bounds how often we re-attempt to start the ADB
// server, so a polling status doesn't spawn an adb process on every tick.
const adbServerRetryInterval = 10 * time.Second

var (
	adbServerMu      sync.Mutex
	adbServerLastTry time.Time
)

// listADBDevices talks to the ADB server on port 5037. When the adb
// executable is missing the failure is reported as such without dialing. If
// no server is running, it starts one via `adb start-server` (idempotent
// when a server is already up) and retries, so the app works without
// requiring a pre-running adb server on the host.
func listADBDevices(adbCommand string) ([]ADBDevice, error) {
	if err := resolveADBCommand(adbCommand); err != nil {
		return nil, err
	}
	devices, err := queryADBDevices()
	if err == nil {
		return devices, nil
	}
	startErr := ensureADBServer(adbCommand)
	devices, err = queryADBDevices()
	if err == nil {
		return devices, nil
	}
	if startErr != nil && !errors.Is(startErr, errADBServerStartThrottled) {
		return nil, fmt.Errorf("%v (adb server could not be started: %v)", err, startErr)
	}
	return nil, err
}

// resolveADBCommand verifies the configured adb executable exists so a
// missing binary is reported clearly instead of surfacing dial errors.
func resolveADBCommand(adbCommand string) error {
	if adbCommand = strings.TrimSpace(adbCommand); adbCommand == "" {
		adbCommand = "adb"
	}
	if _, err := exec.LookPath(adbCommand); err != nil {
		return fmt.Errorf("adb executable not found: %s", adbCommand)
	}
	return nil
}

func queryADBDevices() ([]ADBDevice, error) {
	client, err := gadb.NewClient()
	if err != nil {
		return nil, err
	}
	items, err := client.DeviceList()
	if err != nil {
		return nil, err
	}
	result := make([]ADBDevice, 0, len(items))
	for _, item := range items {
		device := item
		result = append(result, device)
	}
	return result, nil
}

var errADBServerStartThrottled = errors.New("adb start attempt made recently, skipping")

// ensureADBServer runs `adb start-server` unless a start attempt was made
// within adbServerRetryInterval. It never blocks a caller for long: the
// command is best-effort and idempotent.
func ensureADBServer(command string) error {
	adbServerMu.Lock()
	defer adbServerMu.Unlock()
	if time.Since(adbServerLastTry) < adbServerRetryInterval {
		return errADBServerStartThrottled
	}
	adbServerLastTry = time.Now()
	if command == "" {
		command = "adb"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, command, "start-server").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s start-server: %w: %s", command, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (s *Service) adbStatus() ADBStatus {
	status := ADBStatus{}
	status.Command, status.CommandSource = s.adbCommandConfig()
	devices, err := s.adbList()
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.ServerAvailable = true
	for _, device := range devices {
		item := ADBDeviceStatus{Serial: device.Serial()}
		state, stateErr := device.State()
		if stateErr != nil {
			item.State = string(gadb.StateUnknown)
			status.Devices = append(status.Devices, item)
			continue
		}
		item.State = string(state)
		item.Online = state == gadb.StateOnline
		status.Devices = append(status.Devices, item)
		if state == gadb.StateOnline {
			status.Connected = true
			if status.Serial == "" {
				status.Serial = device.Serial()
				status.State = string(state)
			}
		}
		if status.State == "" {
			status.Serial = device.Serial()
			status.State = string(state)
		}
	}
	return status
}

var adbSerialPattern = regexp.MustCompile(`(?im)\+QADBKEY:\s*"?([^"\r\n]+)"?`)

func parseADBKeySerial(response string) string {
	match := adbSerialPattern.FindStringSubmatch(response)
	if len(match) == 2 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

func firstValue(response string) string {
	for _, line := range strings.Split(response, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.EqualFold(line, "OK") || strings.EqualFold(line, "ERROR") || strings.HasPrefix(line, "AT") || strings.HasPrefix(line, "+") {
			continue
		}
		return strings.Trim(line, "\r\" ")
	}
	return ""
}

func parseATI(response string) (manufacturer, model string) {
	values := []string{}
	for _, line := range strings.Split(response, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.EqualFold(line, "OK") || strings.HasPrefix(line, "AT") {
			continue
		}
		values = append(values, line)
	}
	if len(values) > 0 {
		manufacturer = values[0]
	}
	if len(values) > 1 {
		model = values[1]
	}
	return
}

func parseUSBID(response string) string {
	config, ok := parseUSBConfig(response)
	if !ok {
		return ""
	}
	return strings.ToUpper(strings.TrimPrefix(config.vid, "0x") + ":" + strings.TrimPrefix(config.pid, "0x"))
}

func parseUSBConfig(response string) (usbConfig, bool) {
	for _, line := range strings.Split(response, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(strings.ToLower(line), `"usbcfg"`) {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			continue
		}
		vid := strings.TrimSpace(parts[1])
		pid := strings.TrimSpace(parts[2])
		if _, err := normalizeUSBID(vid); err != nil {
			continue
		}
		if _, err := normalizeUSBID(pid); err != nil {
			continue
		}
		fields := make([]string, 0, len(parts)-3)
		for _, value := range parts[3:] {
			value = strings.TrimSpace(value)
			if value == "" || strings.EqualFold(value, "OK") {
				continue
			}
			fields = append(fields, value)
		}
		return usbConfig{vid: normalizeUSBIDOrOriginal(vid), pid: normalizeUSBIDOrOriginal(pid), fields: fields, raw: line}, true
	}
	return usbConfig{}, false
}

func normalizeUSBID(value string) (string, error) {
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "0x"), "0X"))
	if len(value) == 0 || len(value) > 4 {
		return "", errors.New("USB ID must be up to four hexadecimal digits")
	}
	if _, err := strconv.ParseUint(value, 16, 16); err != nil {
		return "", err
	}
	return "0x" + strings.ToUpper(fmt.Sprintf("%04s", value)), nil
}

func normalizeUSBIDOrOriginal(value string) string {
	result, err := normalizeUSBID(value)
	if err != nil {
		return value
	}
	return result
}

func resolveOutputPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		value = filepath.Join(home, "DJOneHub", "firmware-backups", "full-nand-"+time.Now().Format("20060102-150405")+".bin")
	}
	value, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	return filepath.Clean(value), nil
}

func adbUnlockCode(serial string) string {
	return md5Crypt("SH_adb_quectel", serial)[12:28]
}

// md5Crypt is the $1$ crypt variant used by the Quectel unlock utility.
func md5Crypt(password, salt string) string {
	key := []byte(password)
	salt = strings.SplitN(salt, "$", 2)[0]
	if len(salt) > 8 {
		salt = salt[:8]
	}
	magic := []byte("$1$")
	altInput := append(append([]byte{}, key...), []byte(salt)...)
	altInput = append(altInput, key...)
	alt := md5.Sum(altInput)
	input := append(append(append([]byte{}, key...), magic...), []byte(salt)...)
	for remaining := len(key); remaining > 0; remaining -= 16 {
		count := remaining
		if count > 16 {
			count = 16
		}
		input = append(input, alt[:count]...)
	}
	for remaining := len(key); remaining > 0; remaining >>= 1 {
		if remaining&1 != 0 {
			input = append(input, 0)
		} else {
			input = append(input, key[0])
		}
	}
	digest := md5.Sum(input)
	result := digest[:]
	for round := 0; round < 1000; round++ {
		var block []byte
		if round&1 != 0 {
			block = append(block, key...)
		} else {
			block = append(block, result...)
		}
		if round%3 != 0 {
			block = append(block, []byte(salt)...)
		}
		if round%7 != 0 {
			block = append(block, key...)
		}
		if round&1 != 0 {
			block = append(block, result...)
		} else {
			block = append(block, key...)
		}
		next := md5.Sum(block)
		result = next[:]
	}
	const alphabet = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	encoded := make([]byte, 0, 22)
	add := func(b2, b1, b0, count byte) {
		value := uint32(b2)<<16 | uint32(b1)<<8 | uint32(b0)
		for i := byte(0); i < count; i++ {
			encoded = append(encoded, alphabet[value&0x3f])
			value >>= 6
		}
	}
	add(result[0], result[6], result[12], 4)
	add(result[1], result[7], result[13], 4)
	add(result[2], result[8], result[14], 4)
	add(result[3], result[9], result[15], 4)
	add(result[4], result[10], result[5], 4)
	add(0, 0, result[11], 2)
	return "$1$" + salt + "$" + string(encoded)
}
