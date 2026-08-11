package backend

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
)

// BusinessAdapter turns the existing protocol-facing backend into the smaller
// business contract used by application services and the single-device runtime.
type BusinessAdapter struct {
	legacy DeviceBackend
	caps   device.CapabilitySet
}

func Adapt(legacy DeviceBackend) *BusinessAdapter {
	a := &BusinessAdapter{legacy: legacy, caps: device.CapabilitySet{
		device.CapabilityDeviceStatus: "",
	}}
	if _, ok := legacy.(DeviceInfoProvider); ok {
		a.caps[device.CapabilitySIM] = "identity and SIM status provider"
		a.caps[device.CapabilityNetworkStatus] = "radio and registration status provider"
	}
	if _, ok := legacy.(SMSProvider); ok {
		a.caps[device.CapabilitySMSRead] = ""
		a.caps[device.CapabilitySMSSend] = ""
	}
	if _, ok := legacy.(SIMAuthProvider); ok {
		a.caps[device.CapabilityAPDU] = ""
	}
	if _, ok := legacy.(USSDProvider); ok {
		a.caps[device.CapabilityUSSD] = ""
	}
	if _, ok := legacy.(RawATBackend); ok {
		a.caps[device.CapabilityRawAT] = ""
		a.caps[device.CapabilityCallMonitor] = "AT voice call commands"
	}
	if _, ok := legacy.(Rebooter); ok {
		a.caps[device.CapabilityDeviceControl] = "backend device control"
	}
	// DeviceBackend already includes SMSProvider. BusinessAdapter exposes the
	// SMSPort view below, converting the legacy SMS type at the boundary.
	if _, ok := legacy.(ESIMPort); ok {
		a.caps[device.CapabilityESIM] = "backend eSIM service port"
	}
	if _, ok := legacy.(NetworkPort); ok {
		a.caps[device.CapabilityNetworkStatus] = "backend network service port"
		a.caps[device.CapabilityNetworkControl] = "backend network service port"
	}
	if _, ok := legacy.(VoWiFiServicePort); ok {
		a.caps[device.CapabilityVoWiFiInspect] = "backend VoWiFi service port"
		a.caps[device.CapabilityVoWiFiControl] = "backend VoWiFi service port"
	}
	return a
}

func (a *BusinessAdapter) Mode() string { return a.legacy.Mode() }

// Legacy returns the wrapped protocol-facing backend. Protocol-level consumers
// (e.g. the VoWiFi host) use it to reach the SIMAuthProvider and
// OperatingModeController surfaces the business contract does not carry.
func (a *BusinessAdapter) Legacy() DeviceBackend {
	if a == nil {
		return nil
	}
	return a.legacy
}

func (a *BusinessAdapter) Identity(ctx context.Context) (Identity, error) {
	provider, ok := a.legacy.(DeviceInfoProvider)
	if !ok {
		return Identity{}, fmt.Errorf("backend does not expose identity")
	}
	var out Identity
	var err error
	if out.IMEI, err = provider.GetIMEI(ctx); err != nil {
		return out, err
	}
	out.IMSI, _ = provider.GetIMSI(ctx)
	out.ICCID, _ = provider.GetICCID(ctx)
	out.MSISDN, _ = provider.GetMSISDN(ctx)
	if revision, ok := a.legacy.(FirmwareRevisionProvider); ok {
		out.Firmware, out.FirmwareSource, out.FirmwareLive, _ = revision.GetFirmwareRevision(ctx)
	} else {
		out.Firmware, _ = provider.GetRevision(ctx)
		if out.Firmware != "" {
			out.FirmwareSource = "backend identity"
			out.FirmwareLive = true
		}
	}
	return out, nil
}

func (a *BusinessAdapter) Radio(ctx context.Context) (RadioState, error) {
	provider, ok := a.legacy.(DeviceInfoProvider)
	if !ok {
		return RadioState{}, fmt.Errorf("backend does not expose radio state")
	}
	serving, err := provider.GetServingSystem(ctx)
	if err != nil {
		return RadioState{}, err
	}
	signal, _ := provider.GetSignalInfo(ctx)
	out := RadioState{Registered: serving.RegStatus == 1 || serving.RegStatus == 5, Operator: serving.Operator, NetworkMode: serving.NetworkMode, RadioBand: serving.RadioBand}
	if signal != nil {
		out.SignalDBM, out.SignalRSRP, out.SignalRSRQ, out.SignalSINR = signal.RSSI, signal.RSRP, signal.RSRQ, signal.SINR
	}
	return out, nil
}

func (a *BusinessAdapter) SIM(ctx context.Context) (SIMState, error) {
	provider, ok := a.legacy.(DeviceInfoProvider)
	if !ok {
		return SIMState{}, fmt.Errorf("backend does not expose SIM state")
	}
	inserted, err := provider.IsSimInserted(ctx)
	if err != nil {
		return SIMState{}, err
	}
	out := SIMState{Inserted: inserted}
	out.IMSI, _ = provider.GetIMSI(ctx)
	out.ICCID, _ = provider.GetICCID(ctx)
	if esim, ok := a.legacy.(ESIMPort); ok {
		out.EID, _ = esim.EID(ctx)
	}
	return out, nil
}

func (a *BusinessAdapter) ListSMS(ctx context.Context) ([]SMSMessage, error) {
	provider, ok := a.legacy.(SMSProvider)
	if !ok {
		return nil, unsupported("sms_read", "list_sms")
	}
	items, err := provider.ListSMS(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]SMSMessage, 0, len(items))
	for _, item := range items {
		out = append(out, SMSMessage{
			Index: item.Index, Tag: item.Tag, Storage: item.Storage,
			ReceivedAt: item.ReceivedAt, Sender: item.Sender, Body: item.Body,
			ConcatRef: item.ConcatRef, PartNumber: item.PartNumber, TotalParts: item.TotalParts,
		})
	}
	return out, nil
}

// ReadSMS and the other SMSPort methods preserve message contents for
// application services while keeping protocol-specific SMS types below the
// adapter boundary.
func (a *BusinessAdapter) ReadSMS(ctx context.Context, ref NewSMSRef) (SMSMessage, error) {
	provider, ok := a.legacy.(SMSProvider)
	if !ok {
		return SMSMessage{}, unsupported("sms_read", "read_sms")
	}
	message, err := provider.ReadSMS(ctx, ref)
	if err != nil {
		return SMSMessage{}, err
	}
	if message == nil {
		return SMSMessage{}, fmt.Errorf("SMS %d is empty", ref.Index)
	}
	return SMSMessage{
		Index: message.Index, Sender: message.Sender, Body: message.Content,
		ReceivedAt: message.Timestamp, Storage: ref.Storage,
		ConcatRef: message.ConcatRef, PartNumber: message.PartNumber, TotalParts: message.TotalParts,
	}, nil
}

func (a *BusinessAdapter) DeleteSMS(ctx context.Context, ref NewSMSRef) error {
	provider, ok := a.legacy.(SMSProvider)
	if !ok {
		return unsupported("sms_read", "delete_sms")
	}
	return provider.DeleteSMS(ctx, ref)
}

// SetInboundSMSHandler forwards consumer registration to the underlying
// protocol backend when it supports push notification (AT +CMTI); polling
// backends record the registration through the same contract.
func (a *BusinessAdapter) SetInboundSMSHandler(handler InboundSMSHandler) {
	if port, ok := a.legacy.(SMSInboundPort); ok {
		port.SetInboundSMSHandler(handler)
	}
}

func (a *BusinessAdapter) DeleteAllSMS(ctx context.Context) error {
	provider, ok := a.legacy.(SMSProvider)
	if !ok {
		return unsupported("sms_read", "delete_all_sms")
	}
	return provider.DeleteAllSMS(ctx)
}

func (a *BusinessAdapter) SendSMS(ctx context.Context, to, body string) error {
	provider, ok := a.legacy.(SMSProvider)
	if !ok {
		return unsupported("sms_send", "send_sms")
	}
	return provider.SendSMS(ctx, to, body)
}

func (a *BusinessAdapter) USSD(ctx context.Context, command string) (string, error) {
	provider, ok := a.legacy.(USSDProvider)
	if !ok {
		return "", unsupported("ussd", "ussd")
	}
	result, err := provider.ExecuteUSSD(ctx, command, defaultOperationTimeout)
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", nil
	}
	return result.Text, nil
}

func (a *BusinessAdapter) APDU(ctx context.Context, request APDURequest) (APDUResponse, error) {
	provider, ok := a.legacy.(SIMAuthProvider)
	if !ok {
		return APDUResponse{}, unsupported("apdu", "apdu")
	}
	channel, err := provider.OpenLogicalChannel(ctx, "")
	if err != nil {
		return APDUResponse{}, err
	}
	defer provider.CloseLogicalChannel(ctx, channel)
	response, err := provider.TransmitAPDU(ctx, channel, strings.ToUpper(hex.EncodeToString(request.Command)))
	if err != nil {
		return APDUResponse{}, err
	}
	decoded, err := hex.DecodeString(strings.TrimSpace(response))
	if err != nil {
		return APDUResponse{}, fmt.Errorf("decode APDU response: %w", err)
	}
	return APDUResponse{Response: decoded}, nil
}

func (a *BusinessAdapter) OpenLogicalChannel(ctx context.Context, aid string) (int, error) {
	provider, ok := a.legacy.(SIMAuthProvider)
	if !ok {
		return 0, unsupported("apdu", "sim_auth_open")
	}
	return provider.OpenLogicalChannel(ctx, aid)
}

func (a *BusinessAdapter) CloseLogicalChannel(ctx context.Context, channelID int) error {
	provider, ok := a.legacy.(SIMAuthProvider)
	if !ok {
		return unsupported("apdu", "sim_auth_close")
	}
	return provider.CloseLogicalChannel(ctx, channelID)
}

func (a *BusinessAdapter) TransmitLogicalChannelAPDU(ctx context.Context, channelID int, command string) (string, error) {
	provider, ok := a.legacy.(SIMAuthProvider)
	if !ok {
		return "", unsupported("apdu", "sim_auth_apdu")
	}
	return provider.TransmitAPDU(ctx, channelID, command)
}

func (a *BusinessAdapter) ResolveSIMAuthAID(ctx context.Context, app, fallbackAID string) (string, string, error) {
	provider, ok := a.legacy.(SIMAuthAIDResolver)
	if !ok {
		return "", "", unsupported("apdu", "sim_auth_aid")
	}
	return provider.ResolveSIMAuthAID(ctx, app, fallbackAID)
}

func (a *BusinessAdapter) Capabilities(context.Context) device.CapabilitySet { return a.caps.Clone() }
func (a *BusinessAdapter) Events(ctx context.Context) (<-chan BackendEvent, error) {
	source, ok := a.legacy.(interface {
		Events(context.Context) (<-chan BackendEvent, error)
	})
	if !ok {
		return closedBackendEvents(), nil
	}
	return source.Events(ctx)
}

// EventDrops forwards the underlying backend's event drop counter so the
// diagnostics surface sees drops even through the adapter.
func (a *BusinessAdapter) EventDrops() uint64 {
	if counter, ok := a.legacy.(EventDropCounter); ok {
		return counter.EventDrops()
	}
	return 0
}

func (a *BusinessAdapter) Close() error { return a.legacy.Close() }

func (a *BusinessAdapter) Reboot(ctx context.Context) error {
	provider, ok := a.legacy.(Rebooter)
	if !ok {
		return unsupported("device_control", "device_reboot")
	}
	return provider.Reboot(ctx)
}

func (a *BusinessAdapter) RawAT(ctx context.Context, command string) (string, error) {
	provider, ok := a.legacy.(RawATBackend)
	if !ok {
		return "", unsupported("raw_at", "raw_at")
	}
	return provider.RawAT(ctx, command)
}

const defaultOperationTimeout = 30 * time.Second

func unsupported(capability, operation string) error {
	e := derrors.CapabilityMissing(capability, operation, "backend does not expose the required port")
	return e
}

func AdaptUnsupported(capability, operation string) error { return unsupported(capability, operation) }

func closedBackendEvents() <-chan BackendEvent {
	ch := make(chan BackendEvent)
	close(ch)
	return ch
}

func (a *BusinessAdapter) EID(ctx context.Context) (string, error) {
	port, ok := a.legacy.(ESIMPort)
	if !ok {
		return "", unsupported("esim", "esim_eid")
	}
	return port.EID(ctx)
}

func (a *BusinessAdapter) Profiles(ctx context.Context) ([]Profile, error) {
	port, ok := a.legacy.(ESIMPort)
	if !ok {
		return nil, unsupported("esim", "esim_profiles")
	}
	return port.Profiles(ctx)
}

func (a *BusinessAdapter) ESIMStorage(ctx context.Context) (ESIMStorageInfo, error) {
	port, ok := a.legacy.(ESIMStoragePort)
	if !ok {
		return ESIMStorageInfo{}, unsupported("esim", "esim_storage")
	}
	return port.ESIMStorage(ctx)
}

func (a *BusinessAdapter) Download(ctx context.Context, activationCode, confirmationCode, matchingID string, opts *ESIMDownloadOptions) error {
	port, ok := a.legacy.(ESIMPort)
	if !ok {
		return unsupported("esim", "esim_download")
	}
	return port.Download(ctx, activationCode, confirmationCode, matchingID, opts)
}

func (a *BusinessAdapter) Enable(ctx context.Context, iccid string) error {
	port, ok := a.legacy.(ESIMPort)
	if !ok {
		return unsupported("esim", "esim_enable")
	}
	return port.Enable(ctx, iccid)
}

func (a *BusinessAdapter) Disable(ctx context.Context, iccid string) error {
	port, ok := a.legacy.(ESIMPort)
	if !ok {
		return unsupported("esim", "esim_disable")
	}
	return port.Disable(ctx, iccid)
}

func (a *BusinessAdapter) Rename(ctx context.Context, iccid, label string) error {
	port, ok := a.legacy.(ESIMPort)
	if !ok {
		return unsupported("esim", "esim_rename")
	}
	return port.Rename(ctx, iccid, label)
}

func (a *BusinessAdapter) Delete(ctx context.Context, iccid string) error {
	port, ok := a.legacy.(ESIMPort)
	if !ok {
		return unsupported("esim", "esim_delete")
	}
	return port.Delete(ctx, iccid)
}

func (a *BusinessAdapter) ListNotifications(ctx context.Context) ([]NotificationItem, error) {
	port, ok := a.legacy.(ESIMPort)
	if !ok {
		return nil, unsupported("esim", "esim_notifications")
	}
	return port.ListNotifications(ctx)
}

func (a *BusinessAdapter) ProcessNotification(ctx context.Context, sequenceNumber int64) error {
	port, ok := a.legacy.(ESIMPort)
	if !ok {
		return unsupported("esim", "esim_notifications")
	}
	return port.ProcessNotification(ctx, sequenceNumber)
}

func (a *BusinessAdapter) RemoveNotification(ctx context.Context, sequenceNumber int64) error {
	port, ok := a.legacy.(ESIMPort)
	if !ok {
		return unsupported("esim", "esim_notifications")
	}
	return port.RemoveNotification(ctx, sequenceNumber)
}

func (a *BusinessAdapter) Status(ctx context.Context) (map[string]any, error) {
	port, ok := a.legacy.(interface {
		Status(context.Context) (map[string]any, error)
	})
	if !ok {
		return nil, unsupported("network_status", "network_status")
	}
	return port.Status(ctx)
}

func (a *BusinessAdapter) SetMode(ctx context.Context, mode string) error {
	port, ok := a.legacy.(NetworkPort)
	if !ok {
		return unsupported("network_control", "network_set_mode")
	}
	return port.SetMode(ctx, mode)
}

func (a *BusinessAdapter) Traffic(ctx context.Context) (map[string]any, error) {
	port, ok := a.legacy.(NetworkPort)
	if !ok {
		return nil, unsupported("network_status", "network_traffic")
	}
	return port.Traffic(ctx)
}

func (a *BusinessAdapter) Check(ctx context.Context) (map[string]any, error) {
	port, ok := a.legacy.(NetworkPort)
	if !ok {
		return nil, unsupported("network_status", "network_check")
	}
	return port.Check(ctx)
}

func (a *BusinessAdapter) EnableVoWiFi(ctx context.Context) error {
	port, ok := a.legacy.(VoWiFiPort)
	if !ok {
		return unsupported("vowifi_control", "vowifi_enable")
	}
	return port.Enable(ctx)
}

func (a *BusinessAdapter) DisableVoWiFi(ctx context.Context) error {
	port, ok := a.legacy.(VoWiFiPort)
	if !ok {
		return unsupported("vowifi_control", "vowifi_disable")
	}
	return port.Disable(ctx)
}

func (a *BusinessAdapter) ReconnectVoWiFi(ctx context.Context) error {
	port, ok := a.legacy.(VoWiFiPort)
	if !ok {
		return unsupported("vowifi_control", "vowifi_reconnect")
	}
	return port.Reconnect(ctx)
}

func (a *BusinessAdapter) VoWiFiStatus(ctx context.Context) (map[string]any, error) {
	port, ok := a.legacy.(VoWiFiPort)
	if !ok {
		return nil, unsupported("vowifi_inspect", "vowifi_status")
	}
	return port.Status(ctx)
}
