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
	RadioBand   string `json:"radio_band,omitempty"`
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
	Index     int    `json:"index"`
	Sender    string `json:"sender,omitempty"`
	Recipient string `json:"recipient,omitempty"`
	Body      string `json:"body"`
	// ReceivedAt is the network-side time: the SMSC timestamp embedded in the
	// deliver PDU for incoming messages, the local send time for outgoing.
	// It is a display attribute and must not be used for ordering, because
	// the SMSC clock and the device clock are not synchronized.
	ReceivedAt time.Time `json:"received_at,omitempty"`
	// RecordedAt is the single-clock ordering key: the device-local time the
	// message was first recorded. Both directions share this clock, so sorting
	// by it never interleaves sent and received messages out of order.
	RecordedAt time.Time `json:"recorded_at,omitempty"`
	// ICCID is the SIM identity the message was recorded under; an empty
	// string when the SIM state was unavailable at record time.
	ICCID      string `json:"iccid,omitempty"`
	ConcatRef  int    `json:"concat_ref,omitempty"`
	PartNumber int    `json:"part_number,omitempty"`
	TotalParts int    `json:"total_parts,omitempty"`
	// Storage is the modem storage identity of the entry (AT CPMS storage
	// name, or the QMI/MBIM storage type); empty when the modem reports none.
	Storage string `json:"storage,omitempty"`
	// Tag is the QMI/MBIM storage tag (0=read, 1=unread, 2=sent, 3=unsent).
	Tag int `json:"tag,omitempty"`
}

type SMSPort interface {
	ReadSMS(context.Context, NewSMSRef) (SMSMessage, error)
	DeleteSMS(context.Context, NewSMSRef) error
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

// EventDropCounter reports the cumulative count of backend events dropped for
// a slow subscriber. It is optional: backends that never drop (or do not
// track) simply do not implement it, and diagnostics omit the entry.
type EventDropCounter interface {
	EventDrops() uint64
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
	// SetInboundSMSHandler registers the consumer of inbound SMS delivery;
	// pass nil to unregister. Backends without a push notification source
	// (QMI/MBIM) record the registration and deliver through their polling
	// path, keeping the same consumer-owned delivery contract.
	SetInboundSMSHandler(InboundSMSHandler)
	Close() error
}

type BackendFactory interface {
	Open(context.Context, device.Candidate) (ModemBackend, string, error)
}
