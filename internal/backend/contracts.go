package backend

import (
	"context"
	"time"

	"github.com/iniwex5/vohive/internal/domain/device"
)

type Identity struct {
	IMEI     string `json:"imei,omitempty"`
	IMSI     string `json:"imsi,omitempty"`
	ICCID    string `json:"iccid,omitempty"`
	MSISDN   string `json:"msisdn,omitempty"`
	Firmware string `json:"firmware,omitempty"`
}

type RadioState struct {
	Registered  bool   `json:"registered"`
	Operator    string `json:"operator,omitempty"`
	NetworkMode string `json:"network_mode,omitempty"`
	SignalDBM   int    `json:"signal_dbm,omitempty"`
	SignalRSRP  int    `json:"signal_rsrp,omitempty"`
	SignalRSRQ  int    `json:"signal_rsrq,omitempty"`
	SignalSINR  int    `json:"signal_sinr,omitempty"`
}

type SIMState struct {
	Inserted bool   `json:"inserted"`
	IMSI     string `json:"imsi,omitempty"`
	ICCID    string `json:"iccid,omitempty"`
	EID      string `json:"eid,omitempty"`
}

type SMSMessage struct {
	Index      int       `json:"index"`
	Sender     string    `json:"sender,omitempty"`
	Recipient  string    `json:"recipient,omitempty"`
	Body       string    `json:"body"`
	Code       string    `json:"code,omitempty"`
	ReceivedAt time.Time `json:"received_at,omitempty"`
	ConcatRef  int       `json:"concat_ref,omitempty"`
	PartNumber int       `json:"part_number,omitempty"`
	TotalParts int       `json:"total_parts,omitempty"`
}

type SMSPort interface {
	ReadSMS(context.Context, int) (SMSMessage, error)
	DeleteSMS(context.Context, int) error
	DeleteAllSMS(context.Context) error
}

type APDURequest struct {
	Command []byte `json:"command"`
}
type APDUResponse struct {
	Response []byte `json:"response"`
}

type BackendEvent struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

type RawATBackend interface {
	RawAT(context.Context, string) (string, error)
}

// ATInteractiveTransport is implemented by transports that can handle an AT
// prompt followed by a payload, such as SMS PDU submission.
type ATInteractiveTransport interface {
	ATCommandTransport
	CommandWithPrompt(string, []byte, time.Duration) (string, error)
}

// ModemBackend is the business contract consumed by runtime and application code.
// Existing protocol adapters can implement it without exposing protocol response types.
type ModemBackend interface {
	Mode() string
	Identity(context.Context) (Identity, error)
	Radio(context.Context) (RadioState, error)
	SIM(context.Context) (SIMState, error)
	ListSMS(context.Context) ([]SMSMessage, error)
	SendSMS(context.Context, string, string) error
	USSD(context.Context, string) (string, error)
	APDU(context.Context, APDURequest) (APDUResponse, error)
	Capabilities(context.Context) device.CapabilitySet
	Events(context.Context) (<-chan BackendEvent, error)
	Close() error
}

type BackendFactory interface {
	Open(context.Context, device.Candidate) (ModemBackend, string, error)
}
