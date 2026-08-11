package backend

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vohive/internal/apduarbiter"
	"github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/modem"
	"github.com/iniwex5/vohive/pkg/smscodec"
)

// ATCommandTransport is the small transport contract used by platform USB AT
// implementations that are not exposed as an operating-system serial port.
type ATCommandTransport interface {
	Command(string, time.Duration) (string, error)
	Close() error
}

// CommandBackend provides the modem business contract over a raw AT command
// transport. It is used for DJI/Quectel devices that expose AT over USB bulk
// endpoints instead of a macOS serial node.
type CommandBackend struct {
	transport   ATCommandTransport
	identity    device.Identity
	smsMu       sync.Mutex
	smsStore    map[int]string
	esimPort    ESIMPort
	apduArbiter *apduarbiter.Arbiter
}

func NewCommandBackend(transport ATCommandTransport, identity device.Identity) *CommandBackend {
	return &CommandBackend{transport: transport, identity: identity, smsStore: make(map[int]string)}
}

func (b *CommandBackend) Mode() string { return "at" }

func (b *CommandBackend) command(ctx context.Context, command string, timeout time.Duration) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < timeout {
		timeout = time.Until(deadline)
	}
	if timeout <= 0 {
		return "", ctx.Err()
	}
	return b.transport.Command(command, timeout)
}

func (b *CommandBackend) Identity(ctx context.Context) (Identity, error) {
	out := Identity{}
	if b.identity.IMEI != "" {
		out.IMEI = b.identity.IMEI
	}
	if value, err := b.command(ctx, "AT+CGSN", 3*time.Second); err == nil {
		out.IMEI = firstDigits(value, 14, 16)
	}
	if value, err := b.command(ctx, "AT+CIMI", 3*time.Second); err == nil {
		out.IMSI = firstDigits(value, 14, 16)
	}
	if value, err := b.command(ctx, "AT+QCCID", 3*time.Second); err == nil {
		out.ICCID = firstDigits(value, 18, 22)
	}
	if value, err := b.command(ctx, "AT+CNUM", 3*time.Second); err == nil {
		out.MSISDN = firstPhoneNumber(value)
	}
	if revision, err := modem.ProbeFirmwareRevision(func(command string, timeout time.Duration) (string, error) {
		return b.command(ctx, command, timeout)
	}); err == nil {
		out.Firmware = revision.Value
		out.FirmwareSource = revision.Source
		out.FirmwareLive = revision.Live
	}
	if out.IMEI == "" {
		return out, fmt.Errorf("AT identity query returned no IMEI")
	}
	return out, nil
}

func (b *CommandBackend) GetFirmwareRevision(ctx context.Context) (string, string, bool, error) {
	revision, err := modem.ProbeFirmwareRevision(func(command string, timeout time.Duration) (string, error) {
		return b.command(ctx, command, timeout)
	})
	if err != nil {
		return "", "", false, err
	}
	return revision.Value, revision.Source, revision.Live, nil
}

func (b *CommandBackend) Radio(ctx context.Context) (RadioState, error) {
	out := RadioState{}
	// LTE/5G registration is reported by CEREG, while older devices may only
	// expose CGREG or CREG. Treat registration in any supported domain as online.
	for _, command := range []string{"AT+CEREG?", "AT+CGREG?", "AT+CREG?"} {
		if value, err := b.command(ctx, command, 3*time.Second); err == nil && registrationStatus(value) {
			out.Registered = true
			break
		}
	}
	if value, err := b.command(ctx, "AT+COPS?", 3*time.Second); err == nil {
		out.Operator = quotedField(value)
	}
	if value, err := b.command(ctx, "AT+QNWINFO", 3*time.Second); err == nil {
		fields := quotedFields(value)
		if len(fields) > 0 {
			out.NetworkMode = fields[0]
		}
		if len(fields) > 2 {
			out.RadioBand = fields[2]
		}
	}
	if value, err := b.command(ctx, "AT+CSQ", 3*time.Second); err == nil {
		if match := regexp.MustCompile(`\+CSQ:\s*(\d+)`).FindStringSubmatch(value); len(match) == 2 {
			if rssi, parseErr := strconv.Atoi(match[1]); parseErr == nil && rssi <= 31 {
				out.SignalDBM = -113 + 2*rssi
			}
		}
	}
	if value, err := b.command(ctx, `AT+QENG="servingcell"`, 3*time.Second); err == nil {
		if cell, ok := modem.ParseServingCellLTEInfo(value); ok {
			if out.RadioBand == "" {
				out.RadioBand = cell.Band
			}
			out.SignalRSRP = cell.RSRP
			out.SignalRSRQ = cell.RSRQ
			out.SignalSINR = cell.SINR
		}
	}
	return out, nil
}

func (b *CommandBackend) SIM(ctx context.Context) (SIMState, error) {
	out := SIMState{}
	if value, err := b.command(ctx, "AT+CPIN?", 3*time.Second); err == nil {
		out.Inserted = strings.Contains(strings.ToUpper(value), "READY")
	}
	if value, err := b.command(ctx, "AT+CIMI", 3*time.Second); err == nil {
		out.IMSI = firstDigits(value, 14, 16)
	}
	if value, err := b.command(ctx, "AT+QCCID", 3*time.Second); err == nil {
		out.ICCID = firstDigits(value, 18, 22)
	}
	return out, nil
}

// The following narrow legacy methods let protocol consumers use the direct
// USB backend without forcing it through the incompatible legacy SMS surface.
func (b *CommandBackend) GetIMEI(ctx context.Context) (string, error) {
	value, err := b.command(ctx, "AT+CGSN", 3*time.Second)
	if err != nil {
		return "", err
	}
	imei := firstDigits(value, 14, 16)
	if imei == "" {
		return "", fmt.Errorf("AT+CGSN returned no IMEI")
	}
	return imei, nil
}

func (b *CommandBackend) GetIMSI(ctx context.Context) (string, error) {
	value, err := b.command(ctx, "AT+CIMI", 3*time.Second)
	if err != nil {
		return "", err
	}
	imsi := firstDigits(value, 14, 16)
	if imsi == "" {
		return "", fmt.Errorf("AT+CIMI returned no IMSI")
	}
	return imsi, nil
}

func (b *CommandBackend) GetICCID(ctx context.Context) (string, error) {
	value, err := b.command(ctx, "AT+QCCID", 3*time.Second)
	if err != nil {
		return "", err
	}
	iccid := firstDigits(value, 18, 22)
	if iccid == "" {
		return "", fmt.Errorf("AT+QCCID returned no ICCID")
	}
	return iccid, nil
}

func (b *CommandBackend) IsSimInserted(ctx context.Context) (bool, error) {
	value, err := b.command(ctx, "AT+CPIN?", 3*time.Second)
	if err != nil {
		return false, err
	}
	return strings.Contains(strings.ToUpper(value), "READY"), nil
}

func (b *CommandBackend) GetServingSystem(ctx context.Context) (*ServingSystem, error) {
	radio, err := b.Radio(ctx)
	if err != nil {
		return nil, err
	}
	status := 0
	statusText := "not_registered"
	if radio.Registered {
		status = 1
		statusText = "registered"
	}
	return &ServingSystem{
		RegStatus: status, RegStatusText: statusText, Operator: radio.Operator,
		NetworkMode: radio.NetworkMode, RadioBand: radio.RadioBand,
	}, nil
}

func (b *CommandBackend) GetNativeMCCMNC(ctx context.Context) (string, string, error) {
	imsi, err := b.GetIMSI(ctx)
	if err != nil {
		return "", "", err
	}
	var efAD []byte
	if response, readErr := b.command(ctx, "AT+CRSM=176,28589,0,0,4", 3*time.Second); readErr == nil {
		sw1, sw2, value, ok := modem.ParseCRSM(response)
		if ok && sw1 == 144 && sw2 == 0 {
			efAD, _ = hex.DecodeString(value)
		}
	}
	mcc, mnc, _, _, err := modem.HomeMCCMNCFromIMSIAndEFAD(imsi, efAD)
	return mcc, mnc, err
}

func (b *CommandBackend) SetOperatingMode(ctx context.Context, mode OperatingMode) error {
	_, err := b.command(ctx, fmt.Sprintf("AT+CFUN=%d", int(mode)), 5*time.Second)
	return err
}

func (b *CommandBackend) GetOperatingMode(ctx context.Context) (OperatingMode, error) {
	response, err := b.command(ctx, "AT+CFUN?", 3*time.Second)
	if err != nil {
		return ModeOnline, err
	}
	match := regexp.MustCompile(`(?m)\+CFUN:\s*(\d+)`).FindStringSubmatch(response)
	if len(match) != 2 {
		return ModeOnline, fmt.Errorf("AT+CFUN? returned no operating mode")
	}
	value, parseErr := strconv.Atoi(match[1])
	if parseErr != nil {
		return ModeOnline, fmt.Errorf("invalid operating mode %q", match[1])
	}
	return OperatingMode(value), nil
}

func (b *CommandBackend) ListSMS(ctx context.Context) ([]SMSMessage, error) {
	if err := b.setPDUMode(ctx); err != nil {
		return nil, err
	}

	all := make([]SMSMessage, 0)
	var failures []string
	for _, storage := range []string{"SM", "ME"} {
		items, err := b.listSMSStorage(ctx, storage)
		if err != nil {
			failures = append(failures, storage+": "+err.Error())
			continue
		}
		all = append(all, items...)
	}
	if len(all) == 0 && len(failures) == 2 {
		return nil, fmt.Errorf("list SMS failed: %s", strings.Join(failures, "; "))
	}
	sort.SliceStable(all, func(i, j int) bool {
		return all[i].ReceivedAt.After(all[j].ReceivedAt)
	})
	return all, nil
}

func (b *CommandBackend) listSMSStorage(ctx context.Context, storage string) ([]SMSMessage, error) {
	if _, err := b.command(ctx, fmt.Sprintf(`AT+CPMS="%s","%s","%s"`, storage, storage, storage), 5*time.Second); err != nil {
		return nil, fmt.Errorf("select storage: %w", err)
	}
	response, err := b.command(ctx, "AT+CMGL=4", 15*time.Second)
	if err != nil {
		return nil, fmt.Errorf("list storage: %w", err)
	}
	items, err := parseSMSListResponse(response, storage)
	if err != nil {
		return nil, err
	}
	b.smsMu.Lock()
	for _, item := range items {
		b.smsStore[item.Index] = storage
	}
	b.smsMu.Unlock()
	return items, nil
}

func (b *CommandBackend) ReadSMS(ctx context.Context, ref NewSMSRef) (SMSMessage, error) {
	if ref.Index < 0 {
		return SMSMessage{}, fmt.Errorf("invalid SMS index %d", ref.Index)
	}
	if err := b.setPDUMode(ctx); err != nil {
		return SMSMessage{}, err
	}
	storage := ref.Storage
	if storage == "" {
		b.smsMu.Lock()
		storage = b.smsStore[ref.Index]
		b.smsMu.Unlock()
	}
	storages := []string{storage}
	if storage == "" {
		storages = []string{"SM", "ME"}
	}
	var lastErr error
	for _, candidate := range storages {
		if candidate == "" {
			continue
		}
		if _, err := b.command(ctx, fmt.Sprintf(`AT+CPMS="%s","%s","%s"`, candidate, candidate, candidate), 5*time.Second); err != nil {
			lastErr = err
			continue
		}
		response, err := b.command(ctx, fmt.Sprintf("AT+CMGR=%d", ref.Index), 10*time.Second)
		if err != nil {
			lastErr = err
			continue
		}
		item, err := parseSMSReadResponse(response, ref.Index, candidate)
		if err == nil {
			item.Storage = candidate
			return item, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("SMS not found")
	}
	return SMSMessage{}, lastErr
}

func (b *CommandBackend) DeleteSMS(ctx context.Context, ref NewSMSRef) error {
	if ref.Index < 0 {
		return fmt.Errorf("invalid SMS index %d", ref.Index)
	}
	storage := ref.Storage
	if storage == "" {
		b.smsMu.Lock()
		storage = b.smsStore[ref.Index]
		b.smsMu.Unlock()
	}
	if storage == "" {
		storage = "ME"
	}
	if _, err := b.command(ctx, fmt.Sprintf(`AT+CPMS="%s","%s","%s"`, storage, storage, storage), 5*time.Second); err != nil {
		return err
	}
	_, err := b.command(ctx, fmt.Sprintf("AT+CMGD=%d", ref.Index), 10*time.Second)
	return err
}

// SetInboundSMSHandler records the inbound SMS consumer. The command backend
// has no +CMTI push source; inbound delivery runs through the polling path.
func (b *CommandBackend) SetInboundSMSHandler(handler InboundSMSHandler) {}

func (b *CommandBackend) DeleteAllSMS(ctx context.Context) error {
	if _, err := b.command(ctx, `AT+CPMS="ME","ME","ME"`, 5*time.Second); err != nil {
		return fmt.Errorf("select ME storage: %w", err)
	}
	if _, err := b.command(ctx, "AT+CMGD=1,4", 20*time.Second); err != nil {
		return fmt.Errorf("clear ME storage: %w", err)
	}
	return nil
}

func (b *CommandBackend) SendSMS(ctx context.Context, to, body string) error {
	interactive, ok := b.transport.(ATInteractiveTransport)
	if !ok {
		return commandUnsupported("sms_send", "send_sms")
	}
	if err := b.setPDUMode(ctx); err != nil {
		return err
	}
	tpdus, lengths, err := smscodec.BuildSubmitTPDUsWithOptions(to, body, smsSubmitOptions(body))
	if err != nil {
		return fmt.Errorf("build SMS PDU: %w", err)
	}
	for i, tpdu := range tpdus {
		payload := append([]byte{0x00}, tpdu...)
		response, err := interactive.CommandWithPrompt(
			fmt.Sprintf("AT+CMGS=%d", lengths[i]),
			append([]byte(strings.ToUpper(hex.EncodeToString(payload))), 0x1a),
			45*time.Second,
		)
		if err != nil {
			return fmt.Errorf("send SMS segment %d/%d: %w", i+1, len(tpdus), err)
		}
		if atResponseIsError(response) || !strings.Contains(response, "+CMGS:") || !atProbeSucceeded(response) {
			return fmt.Errorf("send SMS segment %d/%d failed: %s", i+1, len(tpdus), response)
		}
		if i+1 < len(tpdus) {
			time.Sleep(500 * time.Millisecond)
		}
	}
	return nil
}

func (b *CommandBackend) USSD(context.Context, string) (string, error) {
	return "", commandUnsupported("ussd", "ussd")
}
func (b *CommandBackend) APDU(context.Context, APDURequest) (APDUResponse, error) {
	return APDUResponse{}, commandUnsupported("apdu", "apdu")
}

// SetAPDUArbiter injects the device-level APDU arbiter shared with eSIM.
func (b *CommandBackend) SetAPDUArbiter(arbiter *apduarbiter.Arbiter) { b.apduArbiter = arbiter }

func (b *CommandBackend) simAuthLease(ctx context.Context, channel int) (*apduarbiter.Lease, error) {
	if b.apduArbiter == nil {
		return nil, nil
	}
	lease, err := b.apduArbiter.AcquireTransport(ctx, apduarbiter.Request{
		Owner: "sim_aka", Mode: b.Mode(), Class: apduarbiter.APDUClassUSIMAKA,
		Channel: channel, Scope: apduarbiter.TransportScopeExclusive,
	})
	if err != nil {
		return nil, fmt.Errorf("acquire SIM authentication APDU lease: %w", err)
	}
	lease.Touch()
	return lease, nil
}

// OpenLogicalChannel opens a USIM/ISIM logical channel through AT+CCHO.
func (b *CommandBackend) OpenLogicalChannel(ctx context.Context, aid string) (int, error) {
	lease, err := b.simAuthLease(ctx, -1)
	if err != nil {
		return 0, err
	}
	if lease != nil {
		defer lease.Release()
	}
	response, err := b.command(ctx, fmt.Sprintf(`AT+CCHO="%s"`, strings.ToUpper(strings.TrimSpace(aid))), 8*time.Second)
	if err != nil {
		return 0, fmt.Errorf("AT+CCHO open logical channel: %w", err)
	}
	match := regexp.MustCompile(`(?m)\+CCHO:\s*(\d+)`).FindStringSubmatch(response)
	if len(match) != 2 {
		return 0, fmt.Errorf("AT+CCHO did not return a logical channel")
	}
	channel, parseErr := strconv.Atoi(match[1])
	if parseErr != nil || channel <= 0 || channel > 255 {
		return 0, fmt.Errorf("invalid logical channel %q", match[1])
	}
	if lease != nil {
		lease.Touch()
	}
	return channel, nil
}

// CloseLogicalChannel closes a USIM/ISIM logical channel through AT+CCHC.
func (b *CommandBackend) CloseLogicalChannel(ctx context.Context, channelID int) error {
	if channelID <= 0 || channelID > 255 {
		return fmt.Errorf("invalid logical channel %d", channelID)
	}
	lease, err := b.simAuthLease(ctx, channelID)
	if err != nil {
		return err
	}
	if lease != nil {
		defer lease.Release()
	}
	if _, err := b.command(ctx, fmt.Sprintf("AT+CCHC=%d", channelID), 8*time.Second); err != nil {
		return fmt.Errorf("AT+CCHC close logical channel %d: %w", channelID, err)
	}
	if lease != nil {
		lease.Touch()
	}
	return nil
}

// TransmitAPDU sends a hexadecimal APDU through AT+CGLA.
func (b *CommandBackend) TransmitAPDU(ctx context.Context, channelID int, commandHex string) (string, error) {
	if channelID <= 0 || channelID > 255 {
		return "", fmt.Errorf("invalid logical channel %d", channelID)
	}
	commandHex = strings.ToUpper(strings.TrimSpace(commandHex))
	if commandHex == "" || len(commandHex)%2 != 0 || !isHex(commandHex) {
		return "", fmt.Errorf("invalid APDU hex command")
	}
	lease, err := b.simAuthLease(ctx, channelID)
	if err != nil {
		return "", err
	}
	if lease != nil {
		defer lease.Release()
	}
	response, err := b.command(ctx, fmt.Sprintf(`AT+CGLA=%d,%d,"%s"`, channelID, len(commandHex), commandHex), 15*time.Second)
	if err != nil {
		return "", fmt.Errorf("AT+CGLA transmit APDU: %w", err)
	}
	match := regexp.MustCompile(`(?m)\+CGLA:\s*(?:\d+\s*,\s*)?\d+\s*,\s*"?([0-9A-Fa-f]+)"?`).FindStringSubmatch(response)
	if len(match) != 2 {
		return "", fmt.Errorf("AT+CGLA did not return an APDU response")
	}
	if lease != nil {
		lease.Touch()
	}
	return strings.ToUpper(match[1]), nil
}

func isHex(value string) bool {
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'F') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

func (b *CommandBackend) Capabilities(context.Context) device.CapabilitySet {
	caps := device.CapabilitySet{
		device.CapabilityDeviceStatus:   "DJI/Quectel USB AT status queries",
		device.CapabilityDeviceControl:  "AT modem reset control",
		device.CapabilitySIM:            "AT SIM status and identity queries",
		device.CapabilityRawAT:          "raw USB AT command transport",
		device.CapabilitySMSRead:        "AT PDU SMS storage read",
		device.CapabilityNetworkStatus:  "AT usbnet and radio status queries",
		device.CapabilityNetworkControl: "AT usbnet mode control",
		device.CapabilityCallMonitor:    "AT+CLCC voice call monitor",
	}
	if b.transport != nil {
		caps[device.CapabilityAPDU] = "AT+CCHO/CGLA SIM authentication APDU channel"
	}
	if _, ok := b.transport.(ATInteractiveTransport); ok {
		caps[device.CapabilitySMSSend] = "AT+CMGS interactive PDU submission"
	}
	if b.esimPort != nil {
		caps[device.CapabilityESIM] = "AT+CCHO/CGLA eUICC APDU channel"
	}
	return caps
}

func (b *CommandBackend) SetESIMPort(port ESIMPort) { b.esimPort = port }

func (b *CommandBackend) ESIMPort() ESIMPort { return b.esimPort }

func (b *CommandBackend) EID(ctx context.Context) (string, error) {
	if b.esimPort == nil {
		return "", commandUnsupported("esim", "esim_eid")
	}
	return b.esimPort.EID(ctx)
}

func (b *CommandBackend) Profiles(ctx context.Context) ([]Profile, error) {
	if b.esimPort == nil {
		return nil, commandUnsupported("esim", "esim_profiles")
	}
	return b.esimPort.Profiles(ctx)
}

func (b *CommandBackend) ESIMStorage(ctx context.Context) (ESIMStorageInfo, error) {
	port, ok := b.esimPort.(ESIMStoragePort)
	if !ok {
		return ESIMStorageInfo{}, commandUnsupported("esim", "esim_storage")
	}
	return port.ESIMStorage(ctx)
}

func (b *CommandBackend) ESIMSnapshot(ctx context.Context) (ESIMSnapshot, error) {
	port, ok := b.esimPort.(ESIMSnapshotPort)
	if !ok {
		return ESIMSnapshot{}, commandUnsupported("esim", "esim_snapshot")
	}
	return port.ESIMSnapshot(ctx)
}

func (b *CommandBackend) Download(ctx context.Context, activationCode, confirmationCode, matchingID string, opts *ESIMDownloadOptions) error {
	if b.esimPort == nil {
		return commandUnsupported("esim", "esim_download")
	}
	return b.esimPort.Download(ctx, activationCode, confirmationCode, matchingID, opts)
}

func (b *CommandBackend) Enable(ctx context.Context, iccid string) error {
	if b.esimPort == nil {
		return commandUnsupported("esim", "esim_enable")
	}
	return b.esimPort.Enable(ctx, iccid)
}

func (b *CommandBackend) Rename(ctx context.Context, iccid, label string) error {
	if b.esimPort == nil {
		return commandUnsupported("esim", "esim_rename")
	}
	return b.esimPort.Rename(ctx, iccid, label)
}

func (b *CommandBackend) Delete(ctx context.Context, iccid string) error {
	if b.esimPort == nil {
		return commandUnsupported("esim", "esim_delete")
	}
	return b.esimPort.Delete(ctx, iccid)
}

func (b *CommandBackend) Disable(ctx context.Context, iccid string) error {
	if b.esimPort == nil {
		return commandUnsupported("esim", "esim_disable")
	}
	return b.esimPort.Disable(ctx, iccid)
}

func (b *CommandBackend) ListNotifications(ctx context.Context) ([]NotificationItem, error) {
	if b.esimPort == nil {
		return nil, commandUnsupported("esim", "esim_notifications")
	}
	return b.esimPort.ListNotifications(ctx)
}

func (b *CommandBackend) ProcessNotification(ctx context.Context, sequenceNumber int64) error {
	if b.esimPort == nil {
		return commandUnsupported("esim", "esim_notifications")
	}
	return b.esimPort.ProcessNotification(ctx, sequenceNumber)
}

func (b *CommandBackend) RemoveNotification(ctx context.Context, sequenceNumber int64) error {
	if b.esimPort == nil {
		return commandUnsupported("esim", "esim_notifications")
	}
	return b.esimPort.RemoveNotification(ctx, sequenceNumber)
}
func (b *CommandBackend) Events(context.Context) (<-chan BackendEvent, error) {
	return make(chan BackendEvent), nil
}
func (b *CommandBackend) Close() error { return b.transport.Close() }
func (b *CommandBackend) RawAT(ctx context.Context, command string) (string, error) {
	return b.command(ctx, command, 30*time.Second)
}

// Status, SetMode, and Check implement the network service over Quectel's
// usbnet AT extension. Host interface details are added by a platform adapter
// when available.
func (b *CommandBackend) Status(ctx context.Context) (map[string]any, error) {
	response, err := b.command(ctx, `AT+QCFG="usbnet"?`, 5*time.Second)
	if err != nil {
		return nil, err
	}
	mode := parseUSBNetMode(response)
	result := map[string]any{"mode": mode}
	if radio, radioErr := b.Radio(ctx); radioErr == nil {
		result["network_mode"] = radio.NetworkMode
		result["radio_band"] = radio.RadioBand
		result["registered"] = radio.Registered
		result["signal_dbm"] = radio.SignalDBM
	}
	return result, nil
}

func (b *CommandBackend) SetMode(ctx context.Context, mode string) error {
	value, err := strconv.Atoi(strings.TrimSpace(mode))
	if err != nil || value < 0 || value > 3 {
		return fmt.Errorf("usbnet mode must be an integer from 0 to 3")
	}
	if _, err := b.command(ctx, fmt.Sprintf(`AT+QCFG="usbnet",%d`, value), 10*time.Second); err != nil {
		return err
	}
	// usbnet is applied after a modem restart. The caller owns the long
	// operation and the runtime will rediscover the device after re-enumeration.
	_, err = b.command(ctx, "AT+CFUN=1,1", 10*time.Second)
	return err
}

func (b *CommandBackend) Reboot(ctx context.Context) error {
	_, err := b.command(ctx, "AT+CFUN=1,1", 5*time.Second)
	return err
}

func (b *CommandBackend) Traffic(context.Context) (map[string]any, error) {
	return map[string]any{"rx_bytes": uint64(0), "tx_bytes": uint64(0)}, nil
}

func (b *CommandBackend) Check(ctx context.Context) (map[string]any, error) {
	radio, err := b.Radio(ctx)
	if err != nil {
		return nil, err
	}
	if radio.Registered {
		return map[string]any{"ok": true, "summary": "cellular registration is ready", "detail": radio.NetworkMode}, nil
	}
	return map[string]any{"ok": false, "summary": "cellular network is not registered"}, nil
}

func (b *CommandBackend) setPDUMode(ctx context.Context) error {
	response, err := b.command(ctx, "AT+CMGF?", 5*time.Second)
	if err != nil {
		return err
	}
	if strings.Contains(response, "+CMGF: 0") {
		return nil
	}
	_, err = b.command(ctx, "AT+CMGF=0", 5*time.Second)
	return err
}

func parseSMSListResponse(response, storage string) ([]SMSMessage, error) {
	lines := splitATLines(response)
	items := make([]SMSMessage, 0)
	for i := 0; i < len(lines)-1; i++ {
		if !strings.HasPrefix(lines[i], "+CMGL:") || !smscodec.IsHexString(lines[i+1]) {
			continue
		}
		index, ok := parseSMSIndex(lines[i])
		if !ok {
			continue
		}
		pdu, _ := smscodec.TrimFullPDUHexByATHeader(lines[i+1], lines[i])
		item, err := decodeSMSPDU(pdu)
		if err != nil {
			return nil, fmt.Errorf("decode SMS %d: %w", index, err)
		}
		item.Index = index
		item.ReceivedAt = item.ReceivedAt.UTC()
		_ = storage
		items = append(items, item)
		i++
	}
	return items, nil
}

func parseSMSReadResponse(response string, index int, storage string) (SMSMessage, error) {
	lines := splitATLines(response)
	for i := 0; i < len(lines)-1; i++ {
		if !strings.HasPrefix(lines[i], "+CMGR:") || !smscodec.IsHexString(lines[i+1]) {
			continue
		}
		pdu, _ := smscodec.TrimFullPDUHexByATHeader(lines[i+1], lines[i])
		item, err := decodeSMSPDU(pdu)
		if err != nil {
			return SMSMessage{}, err
		}
		item.Index = index
		_ = storage
		return item, nil
	}
	return SMSMessage{}, errors.New("SMS response did not contain a PDU")
}

func decodeSMSPDU(value string) (SMSMessage, error) {
	full, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return SMSMessage{}, err
	}
	if len(full) < 2 {
		return SMSMessage{}, errors.New("PDU is too short")
	}
	smscLength := int(full[0])
	tPDUOffset := 1 + smscLength
	if tPDUOffset >= len(full) {
		return SMSMessage{}, errors.New("PDU has an invalid SMSC length")
	}
	sender, body, timestamp, concat, err := smscodec.DecodeDeliverTPDU(full[tPDUOffset:])
	if err != nil {
		return SMSMessage{}, err
	}
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}
	return SMSMessage{Sender: sender, Body: body, ReceivedAt: timestamp, ConcatRef: concat.Ref, PartNumber: concat.Seq, TotalParts: concat.Total}, nil
}

func parseSMSIndex(header string) (int, bool) {
	match := regexp.MustCompile(`\+CMGL:\s*(\d+)`).FindStringSubmatch(header)
	if len(match) != 2 {
		return 0, false
	}
	value, err := strconv.Atoi(match[1])
	return value, err == nil
}

func splitATLines(response string) []string {
	response = strings.ReplaceAll(response, "\r", "\n")
	lines := strings.Split(response, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

func parseUSBNetMode(response string) string {
	match := regexp.MustCompile(`\+QCFG:\s*"usbnet"\s*,\s*(\d+)`).FindStringSubmatch(response)
	if len(match) == 2 {
		return match[1]
	}
	return "unknown"
}

func smsSubmitOptions(body string) smscodec.SubmitOptions {
	for _, r := range body {
		if r > 127 {
			return smscodec.SubmitOptions{Encoding: smscodec.SMSEncodingUCS2}
		}
	}
	return smscodec.SubmitOptions{}
}

func atResponseIsError(response string) bool {
	normalized := strings.ToUpper(strings.ReplaceAll(response, "\r\n", "\n"))
	return strings.Contains(normalized, "\nERROR\n") ||
		strings.HasSuffix(normalized, "\nERROR") ||
		strings.Contains(normalized, "+CME ERROR:") ||
		strings.Contains(normalized, "+CMS ERROR:")
}

func atProbeSucceeded(response string) bool {
	normalized := strings.ReplaceAll(strings.TrimSpace(response), "\r\n", "\n")
	return normalized == "OK" || strings.HasSuffix(normalized, "\nOK")
}

func firstDigits(value string, min, max int) string {
	for _, match := range regexp.MustCompile(`\d+`).FindAllString(value, -1) {
		if len(match) >= min && len(match) <= max {
			return match
		}
	}
	return ""
}

func firstPhoneNumber(value string) string {
	return regexp.MustCompile(`\+?[0-9][0-9 -]{5,}`).FindString(value)
}

func responseValue(value, prefix string) string {
	echo := strings.ToUpper("AT" + prefix)
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		upper := strings.ToUpper(line)
		if line != "" && upper != echo && !strings.HasPrefix(upper, strings.ToUpper(prefix)) && upper != "OK" {
			return line
		}
	}
	return ""
}

func quotedField(value string) string {
	fields := quotedFields(value)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func quotedFields(value string) []string {
	matches := regexp.MustCompile(`"([^"]*)"`).FindAllStringSubmatch(value, -1)
	fields := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) == 2 {
			fields = append(fields, match[1])
		}
	}
	return fields
}

func registrationStatus(value string) bool {
	match := regexp.MustCompile(`\+(?:CE|CG|C)REG:\s*\d+\s*,?\s*(\d+)`).FindStringSubmatch(strings.ToUpper(value))
	if len(match) != 2 {
		return false
	}
	status, _ := strconv.Atoi(match[1])
	return status == 1 || status == 5
}

func commandUnsupported(capability, operation string) error {
	return derrors.CapabilityMissing(capability, operation, "raw USB AT transport does not expose this port")
}
