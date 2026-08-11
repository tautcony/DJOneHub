package device

import "time"

// EDLState is the observed Qualcomm EDL protocol state. Values describe
// verified protocol facts. They do not infer a normal-mode firmware revision.
type EDLState string

const (
	EDLStateUnknown          EDLState = "unknown"
	EDLStateDetected         EDLState = "detected"
	EDLStateSaharaConnected  EDLState = "sahara_connected"
	EDLStateSaharaIdentified EDLState = "sahara_identified"
	EDLStateFirehoseReady    EDLState = "firehose_ready"
	EDLStateNANDReading      EDLState = "nand_reading"
	EDLStateBackupSucceeded  EDLState = "backup_succeeded"
	EDLStateResetRequested   EDLState = "reset_requested"
	EDLStateReconnecting     EDLState = "reconnecting"
	EDLStateRecoveryRequired EDLState = "recovery_required"
)

// EDLObservation contains bounded facts observed from the active EDL device.
// Public API projections must mask device identifiers before returning them.
type EDLObservation struct {
	State          EDLState  `json:"state"`
	Protocol       string    `json:"protocol,omitempty"`
	Source         string    `json:"source,omitempty"`
	SerialNumber   string    `json:"serial_number,omitempty"`
	HardwareID     string    `json:"hardware_id,omitempty"`
	PKHash         string    `json:"pk_hash,omitempty"`
	SBLVersion     string    `json:"sbl_version,omitempty"`
	ObservedAt     time.Time `json:"observed_at,omitempty"`
	Reason         string    `json:"reason,omitempty"`
	RecoveryNeeded bool      `json:"recovery_required,omitempty"`
}

type EDLSessionSnapshot struct {
	SessionID        string         `json:"session_id,omitempty"`
	PhysicalLocation string         `json:"physical_location,omitempty"`
	Observation      EDLObservation `json:"observation"`
	LeaseHeld        bool           `json:"lease_held"`
	LeaseOwned       bool           `json:"lease_owned"`
	LeaseExpiresAt   time.Time      `json:"lease_expires_at,omitempty"`
	ActiveOperation  string         `json:"active_operation,omitempty"`
}

// PublicEDLObservation removes raw hardware identifiers while retaining a
// stable suffix that lets a local user compare repeated observations.
func PublicEDLObservation(value EDLObservation) EDLObservation {
	value.SerialNumber = maskEDLIdentifier(value.SerialNumber)
	value.HardwareID = maskEDLIdentifier(value.HardwareID)
	value.PKHash = maskEDLIdentifier(value.PKHash)
	return value
}

func maskEDLIdentifier(value string) string {
	if len(value) <= 4 {
		if value == "" {
			return ""
		}
		return "****"
	}
	return "****" + value[len(value)-4:]
}
