package errors

import "fmt"

type Code string

const (
	InvalidRequest           Code = "invalid_request"
	Unauthenticated          Code = "unauthenticated"
	NotFound                 Code = "not_found"
	DeviceOffline            Code = "device_offline"
	OperationConflict        Code = "operation_conflict"
	OperationCancelled       Code = "operation_cancelled"
	OperationTimeout         Code = "operation_timeout"
	BackendUnavailable       Code = "backend_unavailable"
	TransportUnavailable     Code = "transport_unavailable"
	Unavailable              Code = "unavailable"
	CapabilityNotSupported   Code = "capability_not_supported"
	PacketTunnelNotSupported Code = "packet_tunnel_not_supported"
	Internal                 Code = "internal_error"
)

type Error struct {
	Code      Code           `json:"code"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details,omitempty"`
	Cause     error          `json:"-"`
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Cause == nil {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
}

func (e *Error) Unwrap() error { return e.Cause }

func New(code Code, message string, retryable bool, details map[string]any) *Error {
	return &Error{Code: code, Message: message, Retryable: retryable, Details: details}
}

func CapabilityMissing(capability, operation, reason string) *Error {
	details := map[string]any{"capability": capability, "operation": operation}
	if reason != "" {
		details["reason"] = reason
	}
	return New(CapabilityNotSupported, "the requested capability is not available", false, details)
}

// PublicMessage returns the stable, locale-neutral message used by API clients.
// Human-readable translations belong to the client, while the code remains the
// machine-readable contract shared by all clients.
func PublicMessage(code Code) string {
	switch code {
	case InvalidRequest:
		return "the request is invalid"
	case Unauthenticated:
		return "local authentication is required"
	case NotFound:
		return "the requested resource was not found"
	case DeviceOffline:
		return "the device is offline"
	case OperationConflict:
		return "the device is busy with another operation"
	case OperationCancelled:
		return "the operation was cancelled"
	case OperationTimeout:
		return "the operation timed out"
	case BackendUnavailable:
		return "the device backend is unavailable"
	case TransportUnavailable:
		return "the device transport is unavailable"
	case Unavailable:
		return "the application is shutting down"
	case CapabilityNotSupported:
		return "the requested capability is not available"
	case PacketTunnelNotSupported:
		return "packet tunneling is not supported"
	case Internal:
		return "an internal error occurred"
	default:
		return "the request could not be completed"
	}
}
