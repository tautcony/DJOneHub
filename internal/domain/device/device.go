package device

import (
	"fmt"
	"strings"
)

// State is the lifecycle state owned by the single-device runtime.
type State string

const (
	StateAbsent       State = "absent"
	StateDiscovered   State = "discovered"
	StateConnecting   State = "connecting"
	StateInitializing State = "initializing"
	StateReady        State = "ready"
	StateDegraded     State = "degraded"
	StateDisconnected State = "disconnected"
)

func (s State) Valid() bool {
	switch s {
	case StateAbsent, StateDiscovered, StateConnecting, StateInitializing, StateReady, StateDegraded, StateDisconnected:
		return true
	default:
		return false
	}
}

// CanTransition describes the intentionally small lifecycle state machine.
func (s State) CanTransition(next State) bool {
	if s == next {
		return true
	}
	switch s {
	case StateAbsent:
		return next == StateDiscovered
	case StateDiscovered:
		return next == StateConnecting || next == StateAbsent || next == StateDisconnected
	case StateConnecting:
		return next == StateInitializing || next == StateDegraded || next == StateDisconnected
	case StateInitializing:
		return next == StateReady || next == StateDegraded || next == StateDisconnected
	case StateReady:
		return next == StateDegraded || next == StateDisconnected
	case StateDegraded:
		return next == StateReady || next == StateDisconnected || next == StateConnecting
	case StateDisconnected:
		return next == StateDiscovered || next == StateAbsent
	default:
		return false
	}
}

// Transition validates a lifecycle change and returns the new state.
func Transition(current, next State) (State, error) {
	if !next.Valid() {
		return current, fmt.Errorf("invalid device state %q", next)
	}
	if !current.Valid() || !current.CanTransition(next) {
		return current, fmt.Errorf("invalid device state transition %q -> %q", current, next)
	}
	return next, nil
}

type BackendMode string

const (
	BackendAT   BackendMode = "at"
	BackendQMI  BackendMode = "qmi"
	BackendMBIM BackendMode = "mbim"
)

func (m BackendMode) Valid() bool {
	return m == BackendAT || m == BackendQMI || m == BackendMBIM
}

type Capability string

const (
	CapabilityDeviceStatus       Capability = "device_status"
	CapabilityDeviceControl      Capability = "device_control"
	CapabilityRawAT              Capability = "raw_at"
	CapabilitySMSRead            Capability = "sms_read"
	CapabilitySMSSend            Capability = "sms_send"
	CapabilitySIM                Capability = "sim"
	CapabilityAPDU               Capability = "apdu"
	CapabilityESIM               Capability = "esim"
	CapabilityUSSD               Capability = "ussd"
	CapabilityNetworkStatus      Capability = "network_status"
	CapabilityNetworkControl     Capability = "network_control"
	CapabilityCallMonitor        Capability = "call_monitor"
	CapabilityNetworkDiagnostics Capability = "network_diagnostics"
	CapabilityVoWiFiInspect      Capability = "vowifi_inspect"
	CapabilityVoWiFiControl      Capability = "vowifi_control"
	CapabilityPacketTunnel       Capability = "packet_tunnel"
	CapabilityFirmwareEDLSwitch  Capability = "firmware_edl_switch"
	CapabilityFirmwareNANDBackup Capability = "firmware_nand_backup"
	CapabilityEDLObservation     Capability = "edl_observation"
)

type CapabilitySet map[Capability]string

func (c CapabilitySet) Clone() CapabilitySet {
	out := make(CapabilitySet, len(c))
	for name, reason := range c {
		out[name] = reason
	}
	return out
}

func (c CapabilitySet) Has(name Capability) bool {
	_, ok := c[name]
	return ok
}

func (c CapabilitySet) Names() []Capability {
	out := make([]Capability, 0, len(c))
	for name := range c {
		out = append(out, name)
	}
	// Keep snapshots deterministic without making callers sort them.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && strings.Compare(string(out[j]), string(out[j-1])) < 0; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

type Identity struct {
	StableID         string `json:"stable_id"`
	PhysicalLocation string `json:"physical_location,omitempty"`
	VendorID         string `json:"vendor_id,omitempty"`
	ProductID        string `json:"product_id,omitempty"`
	IMEI             string `json:"imei,omitempty"`
	SerialNumber     string `json:"serial_number,omitempty"`
	Manufacturer     string `json:"manufacturer,omitempty"`
	Product          string `json:"product,omitempty"`
}

// Candidate is the transport-level observation used to correlate re-enumeration.
type Candidate struct {
	Identity         Identity          `json:"identity"`
	ATPort           string            `json:"at_port,omitempty"`
	ATPorts          []string          `json:"at_ports,omitempty"`
	ControlPath      string            `json:"control_path,omitempty"`
	NetworkInterface string            `json:"network_interface,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

func (c Candidate) StableID() string {
	if id := strings.TrimSpace(c.Identity.StableID); id != "" {
		return id
	}
	parts := []string{c.Identity.SerialNumber, c.Identity.IMEI, c.Identity.PhysicalLocation, c.Identity.VendorID, c.Identity.ProductID}
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			return strings.Join(nonEmpty(parts), "/")
		}
	}
	return "unknown-device"
}

func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

type Snapshot struct {
	State         State         `json:"state"`
	Identity      Identity      `json:"identity"`
	Backend       BackendMode   `json:"backend,omitempty"`
	BackendReason string        `json:"backend_reason,omitempty"`
	Capabilities  CapabilitySet `json:"capabilities"`
	LastError     string        `json:"last_error,omitempty"`
	Generation    uint64        `json:"generation"`
}

// OfflineEvent is the payload of the device.offline bus event, published by
// the runtime when it leaves a usable device state.
type OfflineEvent struct {
	State     State  `json:"state"`
	Reason    string `json:"reason,omitempty"`
	LastError string `json:"last_error,omitempty"`
}
