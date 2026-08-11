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
	"github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/modem"
	"github.com/iniwex5/vohive/internal/runtime"
	"github.com/iniwex5/vohive/internal/storage"
	"github.com/iniwex5/vohive/internal/transport"
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
	Store    storage.ValueStore
	EDLPort  transport.EDLPort
	Firehose transport.FirehosePort
}

// Settings is the atomic JSON document in the device-control namespace.
type Settings struct {
	ADBCommand      string `json:"adb_command,omitempty"`
	EDLPath         string `json:"edl_path,omitempty"`
	EDLRunner       string `json:"edl_runner,omitempty"`
	LoaderPath      string `json:"loader_path,omitempty"`
	BackupDirectory string `json:"backup_directory,omitempty"`
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
	statusCacheMu       sync.Mutex
	cachedStatus        *Status
	cachedAt            time.Time
	lastFirmware        modem.FirmwareRevision
	lastFirmwareReason  string
	lastNormalCandidate device.Candidate
	lastEDLCandidate    device.Candidate
}

func (s *Service) sessionLocation() (string, error) {
	if s.runtime != nil {
		if candidate, err := s.runtime.Candidate(); err == nil && strings.TrimSpace(candidate.Identity.PhysicalLocation) != "" {
			return candidate.Identity.PhysicalLocation, nil
		}
	}
	s.statusCacheMu.Lock()
	location := strings.TrimSpace(s.lastNormalCandidate.Identity.PhysicalLocation)
	if location == "" {
		location = strings.TrimSpace(s.lastEDLCandidate.Identity.PhysicalLocation)
	}
	s.statusCacheMu.Unlock()
	if location == "" {
		return "", derrors.New(derrors.DeviceOffline, "the managed device has no stable physical location", true, nil)
	}
	return location, nil
}

func (s *Service) AcquireControlLease() (string, device.EDLSessionSnapshot, error) {
	if s.runtime == nil {
		return "", device.EDLSessionSnapshot{}, derrors.New(derrors.CapabilityNotSupported, "device session control is unavailable", false, nil)
	}
	location, err := s.sessionLocation()
	if err != nil {
		return "", device.EDLSessionSnapshot{}, err
	}
	return s.runtime.EDLSessions().Acquire(location)
}

func (s *Service) RenewControlLease(token string) (device.EDLSessionSnapshot, error) {
	if s.runtime == nil {
		return device.EDLSessionSnapshot{}, derrors.New(derrors.CapabilityNotSupported, "device session control is unavailable", false, nil)
	}
	location, err := s.sessionLocation()
	if err != nil {
		return device.EDLSessionSnapshot{}, err
	}
	return s.runtime.EDLSessions().Renew(location, strings.TrimSpace(token))
}

func (s *Service) ReleaseControlLease(token string) error {
	if s.runtime == nil {
		return derrors.New(derrors.CapabilityNotSupported, "device session control is unavailable", false, nil)
	}
	location, err := s.sessionLocation()
	if err != nil {
		return err
	}
	return s.runtime.EDLSessions().Release(location, strings.TrimSpace(token))
}

func (s *Service) BeginControlOperation(token, operation string) (func(), error) {
	if s.runtime == nil {
		return nil, derrors.New(derrors.CapabilityNotSupported, "device session control is unavailable", false, nil)
	}
	location, err := s.sessionLocation()
	if err != nil {
		return nil, err
	}
	if err := s.runtime.EDLSessions().BeginOperation(location, strings.TrimSpace(token), operation); err != nil {
		return nil, err
	}
	return func() { s.runtime.EDLSessions().EndOperation(location, strings.TrimSpace(token)) }, nil
}

const deviceControlStatusCacheTTL = 1500 * time.Millisecond
const edlObservationReuseTTL = 5 * time.Second

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

// DeviceControlSettings returns one effective document for ADB and EDL tools.
func (s *Service) DeviceControlSettings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	value := s.settings
	if strings.TrimSpace(s.config.ADBCommand) != "" {
		value.ADBCommand = strings.TrimSpace(s.config.ADBCommand)
	}
	if strings.TrimSpace(s.config.EDLScript) != "" {
		value.EDLPath = strings.TrimSpace(s.config.EDLScript)
	}
	return value
}

// SetDeviceControlSettings validates and persists the complete control
// document in one write. Empty optional paths clear their saved values.
func (s *Service) SetDeviceControlSettings(ctx context.Context, value Settings) error {
	_ = ctx
	value.ADBCommand = strings.TrimSpace(value.ADBCommand)
	value.EDLPath = strings.TrimSpace(value.EDLPath)
	value.EDLRunner = strings.TrimSpace(value.EDLRunner)
	value.LoaderPath = strings.TrimSpace(value.LoaderPath)
	value.BackupDirectory = strings.TrimSpace(value.BackupDirectory)
	if value.ADBCommand != "" {
		if _, err := exec.LookPath(value.ADBCommand); err != nil {
			return derrors.New(derrors.InvalidRequest, "adb command not found", false, nil)
		}
	}
	for name, path := range map[string]string{"edl_path": value.EDLPath, "loader_path": value.LoaderPath} {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			return derrors.New(derrors.InvalidRequest, name+" does not exist", false, nil)
		}
		if name == "loader_path" {
			if info, statErr := os.Stat(path); statErr != nil || !info.Mode().IsRegular() {
				return derrors.New(derrors.InvalidRequest, "loader_path is not a regular file", false, nil)
			}
		}
	}
	if value.EDLRunner != "" && value.EDLRunner != "python" && value.EDLRunner != "uv" {
		return derrors.New(derrors.InvalidRequest, "edl_runner must be python or uv", false, nil)
	}
	if s.config.Store == nil {
		return derrors.New(derrors.CapabilityNotSupported, "device-control settings are unavailable", false, nil)
	}
	s.mu.Lock()
	err := s.config.Store.Write(&value)
	if err == nil {
		s.settings = value
	}
	s.mu.Unlock()
	if err != nil {
		return derrors.New(derrors.Internal, "unable to save device-control settings", true, nil)
	}
	s.invalidateStatusCache()
	return nil
}

// SetADBCommand persists the adb command used to start the local adb server.
// An empty command clears the saved value; DJONEHUB_ADB_COMMAND still wins
// while it is set. A non-empty command must resolve to an executable.
func (s *Service) SetADBCommand(ctx context.Context, command string) error {
	command = strings.TrimSpace(command)
	if s.config.Store == nil {
		return derrors.New(derrors.CapabilityNotSupported, "device-control settings are unavailable", false, nil)
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
	Available             bool                       `json:"available"`
	Manufacturer          string                     `json:"manufacturer,omitempty"`
	Model                 string                     `json:"model,omitempty"`
	Firmware              string                     `json:"firmware,omitempty"`
	FirmwareVersionSource string                     `json:"firmware_version_source,omitempty"`
	FirmwareVersionLive   bool                       `json:"firmware_version_live,omitempty"`
	FirmwareVersionReason string                     `json:"firmware_version_reason,omitempty"`
	ADBKeySerial          string                     `json:"adb_key_serial,omitempty"`
	USBConfig             string                     `json:"usb_config,omitempty"`
	USBConfigFields       []USBConfigField           `json:"usb_config_fields,omitempty"`
	USBID                 string                     `json:"usb_id,omitempty"`
	USBVID                string                     `json:"usb_vid,omitempty"`
	USBPID                string                     `json:"usb_pid,omitempty"`
	Mode                  string                     `json:"mode"`
	ModeReason            string                     `json:"mode_reason,omitempty"`
	ADB                   ADBStatus                  `json:"adb"`
	Backup                BackupStatus               `json:"backup"`
	EntryMethods          []string                   `json:"entry_methods,omitempty"`
	EntryMethodReasons    map[string]string          `json:"entry_method_reasons,omitempty"`
	Settings              Settings                   `json:"settings"`
	EDL                   *device.EDLObservation     `json:"edl,omitempty"`
	EDLSession            *device.EDLSessionSnapshot `json:"edl_session,omitempty"`
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
	Available      bool   `json:"available"`
	ResetAvailable bool   `json:"reset_available"`
	Reason         string `json:"reason,omitempty"`
	Command        string `json:"command,omitempty"`
	Script         string `json:"script,omitempty"`
	DefaultDir     string `json:"default_dir,omitempty"`
}

// Status 从短 TTL 缓存提供固件状态; 缓存过期时才运行完整探测序列。
func (s *Service) Status(ctx context.Context) (Status, error) {
	s.statusCacheMu.Lock()
	if s.cachedStatus != nil && time.Since(s.cachedAt) < deviceControlStatusCacheTTL {
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

func (s *Service) StatusForLease(ctx context.Context, token string) (Status, error) {
	status, err := s.Status(ctx)
	if err != nil || s.runtime == nil {
		return status, err
	}
	location, locationErr := s.sessionLocation()
	if locationErr != nil {
		return status, nil
	}
	if snapshot, ok := s.runtime.EDLSessions().Snapshot(location); ok {
		public := snapshot
		public.PhysicalLocation = ""
		public.Observation = device.PublicEDLObservation(public.Observation)
		public.LeaseOwned = s.runtime.EDLSessions().Owns(location, strings.TrimSpace(token))
		status.EDLSession = &public
	}
	return status, nil
}

func (s *Service) invalidateStatusCache() {
	s.statusCacheMu.Lock()
	s.cachedStatus = nil
	s.cachedAt = time.Time{}
	s.statusCacheMu.Unlock()
}

func (s *Service) statusFresh(ctx context.Context) (Status, error) {
	status := Status{Mode: "unknown", Backup: s.backupStatus()}
	status.Settings = s.DeviceControlSettings()
	status.EntryMethodReasons = map[string]string{}
	status.EntryMethodReasons["adb"] = "ADB fallback requires one selected online device"
	if s.runtime != nil {
		caps := s.runtime.Snapshot().Capabilities
		if caps.Has(device.CapabilityFirmwareEDLSwitch) && s.config.EDLPort != nil {
			status.EntryMethods = append(status.EntryMethods, "direct")
		} else {
			status.EntryMethodReasons["direct"] = "direct DIAG EDL switching is unavailable on the active platform"
		}
	}
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
			status.FirmwareVersionReason = "AT firmware revision is not available in EDL"
			s.observeEDLStatus(ctx, &status)
			return status, nil
		}
	}
	var adbEnabled bool
	var adbEnabledKnown bool
	if s.at != nil {
		responses := map[string]string{}
		commands := []struct{ key, command string }{
			{"ati", "ATI"},
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
		if revision, revisionErr := modem.ProbeFirmwareRevision(func(command string, timeout time.Duration) (string, error) {
			probeCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			return s.at.Execute(probeCtx, command)
		}); revisionErr == nil {
			status.Firmware = revision.Value
			status.FirmwareVersionSource = revision.Source
			status.FirmwareVersionLive = revision.Live
			s.statusCacheMu.Lock()
			s.lastFirmware = revision
			s.lastFirmwareReason = ""
			s.statusCacheMu.Unlock()
		} else {
			status.FirmwareVersionReason = "the modem returned no unambiguous QGMR or CGMR revision"
		}
		if s.runtime != nil {
			if candidate, candidateErr := s.runtime.Candidate(); candidateErr == nil {
				s.statusCacheMu.Lock()
				s.lastNormalCandidate = candidate
				s.statusCacheMu.Unlock()
			}
		}
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
			if s.runtime == nil || s.runtime.Snapshot().State == device.StateReady {
				return status, lastErr
			}
			status.ModeReason = "device is reconnecting after a mode change"
		}
	}

	status.ADB = s.adbStatus()
	if status.ADB.Connected {
		status.EntryMethods = append(status.EntryMethods, "adb")
		delete(status.EntryMethodReasons, "adb")
	} else {
		status.EntryMethodReasons["adb"] = "ADB fallback requires one selected online device"
	}
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

func (s *Service) observeEDLStatus(ctx context.Context, status *Status) {
	observation := device.EDLObservation{State: device.EDLStateDetected, Protocol: "sahara", Source: "usb", ObservedAt: time.Now().UTC()}
	s.statusCacheMu.Lock()
	original := s.lastNormalCandidate
	s.statusCacheMu.Unlock()
	if s.runtime != nil {
		if candidate, err := s.runtime.Candidate(); err == nil && original.Identity.PhysicalLocation == "" {
			original = candidate
		}
		if original.Identity.PhysicalLocation != "" {
			if snapshot, ok := s.runtime.EDLSessions().Snapshot(original.Identity.PhysicalLocation); ok && reusableEDLObservation(snapshot.Observation, time.Now()) {
				public := device.PublicEDLObservation(snapshot.Observation)
				status.EDL = &public
				publicSnapshot := snapshot
				publicSnapshot.PhysicalLocation = ""
				publicSnapshot.Observation = public
				status.EDLSession = &publicSnapshot
				return
			}
		}
	}
	if s.config.EDLPort != nil {
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if edlCandidate, err := s.config.EDLPort.FindEDL(probeCtx, original); err == nil {
			if original.Identity.PhysicalLocation == "" {
				original = edlCandidate
			}
			s.statusCacheMu.Lock()
			s.lastEDLCandidate = edlCandidate
			s.statusCacheMu.Unlock()
			if s.runtime != nil {
				_, _ = s.runtime.EDLSessions().Correlate(original, edlCandidate)
			}
			observed, observeErr := s.config.EDLPort.ObserveEDL(probeCtx, edlCandidate)
			if observed.State != "" {
				observation = observed
			}
			if observeErr != nil {
				observation.State = device.EDLStateRecoveryRequired
				observation.RecoveryNeeded = true
				if observation.Reason == "" {
					observation.Reason = "Sahara observation failed"
				}
			}
		} else {
			observation.Reason = "matching EDL device could not be correlated"
			observation.State = device.EDLStateRecoveryRequired
			observation.RecoveryNeeded = true
		}
	}
	if observation.ObservedAt.IsZero() {
		observation.ObservedAt = time.Now().UTC()
	}
	if s.runtime != nil && original.Identity.PhysicalLocation != "" {
		if snapshot, err := s.runtime.EDLSessions().Observe(original.Identity.PhysicalLocation, observation); err == nil {
			publicSnapshot := snapshot
			publicSnapshot.PhysicalLocation = ""
			publicSnapshot.Observation = device.PublicEDLObservation(snapshot.Observation)
			status.EDLSession = &publicSnapshot
		}
	}
	public := device.PublicEDLObservation(observation)
	status.EDL = &public
}

func reusableEDLObservation(observation device.EDLObservation, now time.Time) bool {
	if observation.ObservedAt.IsZero() || now.Sub(observation.ObservedAt) < 0 || now.Sub(observation.ObservedAt) > edlObservationReuseTTL {
		return false
	}
	switch observation.State {
	case device.EDLStateSaharaIdentified, device.EDLStateFirehoseReady:
		return true
	default:
		return false
	}
}

func (s *Service) StartUnlock(ctx context.Context) (string, error) {
	if s.ops == nil {
		return "", errors.New("operation manager is unavailable")
	}
	return s.ops.Start(ctx, "device_control.adb_unlock", func(ctx context.Context, _ string, report func(int, string)) error {
		release, err := s.acquireDevice(ctx)
		if err != nil {
			return err
		}
		defer release()
		return s.unlock(ctx, report)
	})
}

func (s *Service) StartADBMode(ctx context.Context, enabled bool) (string, error) {
	if s.ops == nil {
		return "", errors.New("operation manager is unavailable")
	}
	return s.ops.Start(ctx, "device_control.adb_mode", func(ctx context.Context, _ string, report func(int, string)) error {
		release, err := s.acquireDevice(ctx)
		if err != nil {
			return err
		}
		defer release()
		defer s.invalidateStatusCache()
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
		report(70, "restarting modem")
		if err := s.restartModem(ctx); err != nil {
			return err
		}
		report(100, label+"; reconnect required")
		return nil
	})
}

// StartADBReboot restarts the selected online ADB device in normal mode.
func (s *Service) StartADBReboot(ctx context.Context, serial string) (string, error) {
	serial = strings.TrimSpace(serial)
	if serial == "" {
		return "", derrors.New(derrors.InvalidRequest, "an ADB device must be selected", false, nil)
	}
	if s.ops == nil {
		return "", errors.New("operation manager is unavailable")
	}
	return s.ops.Start(ctx, "device_control.adb_reboot", func(ctx context.Context, _ string, report func(int, string)) error {
		defer s.invalidateStatusCache()
		release, err := s.acquireDevice(ctx)
		if err != nil {
			return err
		}
		defer release()
		adbDevice, err := s.selectOnlineADBDevice(serial)
		if err != nil {
			return err
		}
		report(30, "sending ADB reboot request")
		if err := adbDevice.Reboot(""); err != nil {
			return derrors.New(derrors.TransportUnavailable, "ADB reboot failed", true, map[string]any{"phase": "adb_reboot"})
		}
		report(100, "ADB reboot requested; reconnect required")
		return nil
	})
}

// StartEnterEDLWithMethod selects direct DIAG or the explicit ADB fallback.
// Direct failures never fall back to ADB because the serial could identify a
// different online device.
func (s *Service) StartEnterEDLWithMethod(ctx context.Context, method, serial string) (string, error) {
	method = strings.ToLower(strings.TrimSpace(method))
	if method == "" {
		method = "direct"
		if s.runtime == nil || !s.runtime.Snapshot().Capabilities.Has(device.CapabilityFirmwareEDLSwitch) || s.config.EDLPort == nil {
			method = "adb"
		}
	}
	if method != "direct" && method != "adb" {
		return "", derrors.New(derrors.InvalidRequest, "entry method must be direct or adb", false, map[string]any{"method": method})
	}
	if method == "direct" && (s.config.EDLPort == nil || s.runtime == nil || !s.runtime.Snapshot().Capabilities.Has(device.CapabilityFirmwareEDLSwitch)) {
		return "", derrors.CapabilityMissing(string(device.CapabilityFirmwareEDLSwitch), "device_control.enter_edl", "direct DIAG switching is unavailable")
	}
	if method == "adb" && strings.TrimSpace(serial) == "" {
		return "", derrors.New(derrors.InvalidRequest, "an ADB device must be selected for the ADB fallback", false, nil)
	}
	if s.ops == nil {
		return "", errors.New("operation manager is unavailable")
	}
	return s.ops.Start(ctx, "device_control.enter_edl", func(ctx context.Context, _ string, report func(int, string)) error {
		defer s.invalidateStatusCache()
		release, err := s.acquireDevice(ctx)
		if err != nil {
			return err
		}
		defer release()
		var original device.Candidate
		if s.runtime != nil {
			original, err = s.runtime.Candidate()
			if err != nil {
				return err
			}
		}
		if s.runtime != nil && original.Identity.PhysicalLocation != "" {
			s.runtime.EDLSessions().ClearObservation(original.Identity.PhysicalLocation)
		}
		if method == "adb" {
			adbDevice, selectErr := s.selectOnlineADBDevice(strings.TrimSpace(serial))
			if selectErr != nil {
				return selectErr
			}
			report(20, "enter_edl: sending ADB reboot request")
			if err := adbDevice.Reboot("edl"); err != nil {
				return derrors.New(derrors.TransportUnavailable, "ADB reboot to EDL failed", true, map[string]any{"phase": "enter_edl"})
			}
		} else {
			report(20, "enter_edl: sending direct DIAG reboot request")
			if err := s.config.EDLPort.EnterEDL(ctx, original); err != nil {
				return derrors.New(derrors.TransportUnavailable, "direct DIAG EDL entry failed", true, map[string]any{"phase": "enter_edl", "cause": err.Error()})
			}
		}
		if s.config.EDLPort != nil && original.Identity.PhysicalLocation != "" {
			report(55, "await_edl: waiting for the matching Qualcomm device")
			deadline := time.Now().Add(15 * time.Second)
			for time.Now().Before(deadline) {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if edlCandidate, findErr := s.config.EDLPort.FindEDL(ctx, original); findErr == nil {
					if s.runtime != nil {
						_, _ = s.runtime.EDLSessions().Correlate(original, edlCandidate)
						_, _ = s.runtime.EDLSessions().Observe(original.Identity.PhysicalLocation, device.EDLObservation{State: device.EDLStateDetected, Protocol: "sahara", Source: "usb"})
					}
					report(100, "complete: EDL device matched at the original location")
					return nil
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(250 * time.Millisecond):
				}
			}
			if s.runtime != nil {
				s.runtime.EDLSessions().MarkRecoveryRequired(original.Identity.PhysicalLocation, "matching EDL device did not re-enumerate")
			}
			return derrors.New(derrors.DeviceOffline, "matching EDL device did not re-enumerate", true, map[string]any{"phase": "await_edl"})
		}
		report(100, "complete: EDL reboot requested")
		return nil
	})
}

func (s *Service) acquireDevice(ctx context.Context) (func(), error) {
	if s.runtime == nil || s.runtime.Locks() == nil {
		return func() {}, nil
	}
	return s.runtime.Locks().Acquire(ctx, runtime.ResourceDevice)
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
	if err := validateEDLInvocation(ctx, invocation); err != nil {
		return "", err
	}
	var operationID string
	ready := make(chan struct{})
	var startErr error
	operationID, startErr = s.ops.Start(ctx, "device_control.nand_backup", func(ctx context.Context, _ string, report func(int, string)) error {
		<-ready
		defer s.invalidateStatusCache()
		firehose := s.config.Firehose
		if firehose == nil {
			firehose = &CommandFirehose{
				ClientPath: invocation.command,
				LoaderPath: loader,
				Prefix:     invocation.prefix,
				Dir:        invocation.dir,
			}
		}
		return s.backupWithFirehose(ctx, firehose, output, loader, invocation, report, func(message string) {
			s.ops.Log(operationID, message)
		})
	})
	if startErr != nil {
		return "", startErr
	}
	close(ready)
	return operationID, nil
}

// StartReset leaves Qualcomm EDL mode through the configured Firehose client
// and waits for the same physical device to return in its normal USB mode.
func (s *Service) StartReset(ctx context.Context) (string, error) {
	if s.ops == nil {
		return "", errors.New("operation manager is unavailable")
	}
	settings := s.DeviceControlSettings()
	invocation, err := s.edlInvocation(settings.EDLPath, settings.EDLRunner)
	if err != nil && s.config.Firehose == nil {
		return "", err
	}
	if s.config.Firehose == nil {
		if err := validateEDLInvocation(ctx, invocation); err != nil {
			return "", err
		}
	}
	loader := strings.TrimSpace(settings.LoaderPath)
	return s.ops.Start(ctx, "device_control.reset", func(ctx context.Context, _ string, report func(int, string)) error {
		defer s.invalidateStatusCache()
		release, acquireErr := s.acquireDevice(ctx)
		if acquireErr != nil {
			return acquireErr
		}
		defer release()

		original := device.Candidate{}
		if s.runtime != nil {
			if candidate, candidateErr := s.runtime.Candidate(); candidateErr == nil {
				original = candidate
			}
		}
		edlCandidate := original
		if s.config.EDLPort != nil {
			report(15, "await_edl: locating the matching Qualcomm device")
			edlCandidate, err = s.config.EDLPort.FindEDL(ctx, original)
			if err != nil {
				return derrors.New(derrors.DeviceOffline, "matching EDL device was not found", true, map[string]any{"phase": "await_edl"})
			}
			if original.Identity.PhysicalLocation == "" {
				original = edlCandidate
			}
		}

		firehose := s.config.Firehose
		if firehose == nil {
			firehose = &CommandFirehose{
				ClientPath: invocation.command,
				LoaderPath: loader,
				Prefix:     invocation.prefix,
				Dir:        invocation.dir,
			}
		}
		report(45, "reset: requesting normal USB mode through Firehose")
		s.recordEDLState(original, device.EDLStateResetRequested, "reset requested", false)
		if resetErr := firehose.Reset(ctx, edlCandidate); resetErr != nil {
			s.recordEDLState(original, device.EDLStateRecoveryRequired, "Firehose reset failed", true)
			return derrors.New(derrors.TransportUnavailable, "Firehose reset failed", true, map[string]any{"phase": "reset", "reconnect_required": true, "cause": resetErr.Error()})
		}
		if s.config.EDLPort == nil || original.Identity.PhysicalLocation == "" {
			report(100, "complete: reset request completed")
			return nil
		}
		report(70, "await_boot: waiting for the original device to reconnect")
		s.recordEDLState(original, device.EDLStateReconnecting, "waiting for normal USB mode", false)
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if _, findErr := s.config.EDLPort.FindOriginal(ctx, original); findErr == nil {
				if s.runtime != nil {
					s.runtime.EDLSessions().ClearObservation(original.Identity.PhysicalLocation)
				}
				report(100, "complete: normal USB mode restored")
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
		s.recordEDLState(original, device.EDLStateRecoveryRequired, "normal USB reconnect timed out", true)
		return derrors.New(derrors.DeviceOffline, "the original device did not reconnect after reset", true, map[string]any{"phase": "await_boot", "reconnect_required": true})
	})
}

func (s *Service) backupWithFirehose(ctx context.Context, firehose transport.FirehosePort, output, loader string, invocation edlInvocation, report func(int, string), logOutput func(string)) error {
	release, err := s.acquireDevice(ctx)
	if err != nil {
		return err
	}
	defer release()
	original := device.Candidate{}
	if s.runtime != nil {
		original, err = s.runtime.Candidate()
		if err != nil && s.config.EDLPort == nil {
			return err
		}
		err = nil
	}
	edlCandidate := original
	if s.config.EDLPort != nil {
		report(0, "await_edl: waiting for the matching Qualcomm device")
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			edlCandidate, err = s.config.EDLPort.FindEDL(ctx, original)
			if err == nil {
				break
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
		if err != nil {
			s.recordEDLState(original, device.EDLStateRecoveryRequired, "matching EDL device was not found", true)
			return derrors.New(derrors.DeviceOffline, "matching EDL device was not found", true, map[string]any{"phase": "await_edl"})
		}
		if original.Identity.PhysicalLocation == "" {
			original = edlCandidate
			s.statusCacheMu.Lock()
			s.lastEDLCandidate = edlCandidate
			s.statusCacheMu.Unlock()
		}
		s.recordEDLState(original, device.EDLStateNANDReading, "NAND read in progress", false)
	}
	if commandFirehose, ok := firehose.(*CommandFirehose); ok {
		configured := *commandFirehose
		configured.Output = firehoseOutputReporter(report, logOutput)
		firehose = &configured
	}
	readReq := transport.FirehoseReadRequest{ClientPath: invocation.command, LoaderPath: loader, OutputPath: output, PageSize: 2048, BlockSize: 131072}
	report(0, "read_nand: reading NAND image")
	result, readErr := firehose.ReadNAND(ctx, edlCandidate, readReq)
	if readErr != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cleanupErr := firehose.Reset(cleanupCtx, edlCandidate)
		recoveryRequired := cleanupErr != nil
		state := device.EDLStateResetRequested
		reason := "cleanup reset requested after NAND read failure"
		if recoveryRequired {
			state = device.EDLStateRecoveryRequired
			reason = "NAND read and cleanup reset failed"
		}
		s.recordEDLState(original, state, reason, recoveryRequired)
		code := derrors.TransportUnavailable
		message := "NAND read failed"
		if errors.Is(readErr, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			code = derrors.OperationCancelled
			message = "NAND read was cancelled"
		} else if errors.Is(readErr, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			code = derrors.OperationTimeout
			message = "NAND read timed out"
		}
		return derrors.New(code, message, true, map[string]any{"phase": "read_nand", "backup_valid": false, "reconnect_required": recoveryRequired})
	}
	if !result.Valid || result.Bytes == 0 {
		s.recordEDLState(original, device.EDLStateRecoveryRequired, "NAND image validation failed", true)
		return derrors.New(derrors.InvalidRequest, "NAND read did not produce a valid image", false, map[string]any{"phase": "read_nand", "backup_valid": false})
	}
	s.recordEDLState(original, device.EDLStateBackupSucceeded, "valid NAND backup completed; device remains in EDL", false)
	if logOutput != nil {
		logOutput("NAND backup completed; the device remains in EDL\n")
	}
	report(100, "complete: NAND backup completed; device remains in EDL")
	return nil
}

var firehosePercentPattern = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)%`)

func firehoseOutputReporter(report func(int, string), logOutput func(string)) func(string) {
	var mu sync.Mutex
	var progressBuffer string
	lastProgress := -1
	lastMessage := ""
	return func(raw string) {
		mu.Lock()
		defer mu.Unlock()
		if logOutput != nil {
			logOutput(raw)
		}
		// Process writes may split a percentage or an ANSI sequence. Keep only a
		// short diagnostic tail for parsing; the raw chunks above remain unchanged.
		progressBuffer += raw
		if len(progressBuffer) > 16*1024 {
			progressBuffer = progressBuffer[len(progressBuffer)-16*1024:]
		}
		cleaned := cleanTerminalOutput(progressBuffer)
		matches := firehosePercentPattern.FindAllStringSubmatchIndex(cleaned, -1)
		if len(matches) == 0 {
			return
		}
		match := matches[len(matches)-1]
		value, err := strconv.ParseFloat(cleaned[match[2]:match[3]], 64)
		if err != nil {
			return
		}
		progress := int(value)
		if progress > 100 {
			progress = 100
		}
		lineStart := strings.LastIndexAny(cleaned[:match[0]], "\r\n") + 1
		lineEnd := strings.IndexAny(cleaned[match[1]:], "\r\n")
		if lineEnd < 0 {
			lineEnd = len(cleaned)
		} else {
			lineEnd += match[1]
		}
		message := strings.TrimSpace(cleaned[lineStart:lineEnd])
		if progress != lastProgress || message != lastMessage {
			lastProgress = progress
			lastMessage = message
			report(progress, message)
		}
	}
}

func (s *Service) recordEDLState(candidate device.Candidate, state device.EDLState, reason string, recovery bool) {
	if s.runtime == nil || strings.TrimSpace(candidate.Identity.PhysicalLocation) == "" {
		return
	}
	_, _ = s.runtime.EDLSessions().Observe(candidate.Identity.PhysicalLocation, device.EDLObservation{
		State: state, Protocol: "sahara", Source: "device_control", Reason: reason, RecoveryNeeded: recovery,
	})
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

// SelectLoaderFile opens a picker for an optional Firehose loader.
func (s *Service) SelectLoaderFile(ctx context.Context) (string, error) {
	return selectFile(ctx, "Choose an optional Firehose loader", "")
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
	report(90, "restarting modem")
	if err := s.restartModem(ctx); err != nil {
		return err
	}
	report(100, "ADB unlocked; reconnect required")
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
	return s.ops.Start(ctx, "device_control.usb_id", func(ctx context.Context, _ string, report func(int, string)) error {
		release, err := s.acquireDevice(ctx)
		if err != nil {
			return err
		}
		defer release()
		defer s.invalidateStatusCache()
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
		if err := s.restartModem(ctx); err != nil {
			return err
		}
		report(100, "USB ID updated; reconnect required")
		return nil
	})
}

// restartModem resets the modem via AT+CFUN=1,1 so a newly written USB
// composition re-enumerates over USB. The AT channel drops briefly while the
// module comes back.
func (s *Service) restartModem(ctx context.Context) error {
	_, err := s.at.Execute(ctx, "AT+CFUN=1,1")
	return err
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

func validateEDLInvocation(ctx context.Context, invocation edlInvocation) error {
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	args := append(append([]string(nil), invocation.prefix...), "--help")
	cmd := exec.CommandContext(checkCtx, invocation.command, args...)
	cmd.Dir = invocation.dir
	var stdout, stderr boundedBuffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if checkCtx.Err() != nil {
			return derrors.New(derrors.TransportUnavailable, "EDL client startup check timed out", true, map[string]any{"phase": "configure_edl"})
		}
		output := boundedToolOutput(stdout.Bytes(), stderr.Bytes())
		if strings.Contains(output, "ModuleNotFoundError") || strings.Contains(output, "No module named") {
			return derrors.New(derrors.InvalidRequest, "EDL Python environment is missing required packages; select the uv runner or install the project dependencies", false, map[string]any{"phase": "configure_edl"})
		}
		return derrors.New(derrors.InvalidRequest, "EDL client failed its startup check", false, map[string]any{"phase": "configure_edl"})
	}
	return nil
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
	settings := s.DeviceControlSettings()
	clientAvailable := s.config.Firehose != nil
	if !clientAvailable {
		_, invocationErr := s.edlInvocation(settings.EDLPath, settings.EDLRunner)
		clientAvailable = invocationErr == nil
	}
	defaultDir, _ := os.UserHomeDir()
	if defaultDir != "" {
		defaultDir = filepath.Join(defaultDir, "DJOneHub", "firmware-backups")
	}
	resetAvailable := clientAvailable
	available := clientAvailable && resetAvailable
	reason := ""
	if !clientAvailable {
		reason = "EDL client is not configured or is unavailable"
	} else if !resetAvailable {
		reason = "Firehose reset path is unavailable"
	}
	return BackupStatus{Available: available, ResetAvailable: resetAvailable, Reason: reason, Command: command, Script: s.config.EDLScript, DefaultDir: defaultDir}
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
