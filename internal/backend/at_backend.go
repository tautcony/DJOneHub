package backend

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/iniwex5/vohive/internal/modem"
	"github.com/iniwex5/vohive/pkg/logger"
	"github.com/iniwex5/vohive/pkg/smscodec"
)

// ATBackend AT 后端适配器 — 纯包装层，委托给现有 modem.Manager
// 不修改 modem.Manager 的任何一行代码
type ATBackend struct {
	modem       *modem.Manager
	esimPort    ESIMPort
	eventsOnce  sync.Once
	eventsCh    chan BackendEvent
	eventsDone  chan struct{}
	eventsClose sync.Once
	// eventsDropped counts modem-reset events dropped because the event
	// subscriber was full; exposed through EventDrops for diagnostics.
	eventsDropped atomic.Uint64
}

// NewATBackend 创建 AT 后端适配器
func NewATBackend(m *modem.Manager) *ATBackend {
	return &ATBackend{modem: m}
}

// Mode 返回后端模式标识
func (a *ATBackend) Mode() string { return "at" }

// Close releases the manager owned by this backend connection.
func (a *ATBackend) Close() error {
	if a == nil || a.modem == nil {
		return nil
	}
	a.eventsClose.Do(func() {
		if a.eventsDone != nil {
			close(a.eventsDone)
		}
	})
	return a.modem.Close()
}

// Modem 返回底层 modem.Manager（供需要直接访问 AT 通道的调用方使用，如 AT+QCFG）
func (a *ATBackend) Modem() *modem.Manager { return a.modem }

// SetESIMPort 注入 eSIM 服务端口。未注入时 eSIM 能力保持不可用。
func (a *ATBackend) SetESIMPort(port ESIMPort) { a.esimPort = port }

// ============================================================================
// ESIMPort 实现 — 委托给注入的 eSIM 服务端口（由 internal/esim.NewATPort 构建）
// ============================================================================

func (a *ATBackend) EID(ctx context.Context) (string, error) {
	if a.esimPort == nil {
		return "", unsupported("esim", "esim_eid")
	}
	return a.esimPort.EID(ctx)
}

func (a *ATBackend) Profiles(ctx context.Context) ([]Profile, error) {
	if a.esimPort == nil {
		return nil, unsupported("esim", "esim_profiles")
	}
	return a.esimPort.Profiles(ctx)
}

func (a *ATBackend) ESIMStorage(ctx context.Context) (ESIMStorageInfo, error) {
	port, ok := a.esimPort.(ESIMStoragePort)
	if !ok {
		return ESIMStorageInfo{}, unsupported("esim", "esim_storage")
	}
	return port.ESIMStorage(ctx)
}

func (a *ATBackend) ESIMSnapshot(ctx context.Context) (ESIMSnapshot, error) {
	port, ok := a.esimPort.(ESIMSnapshotPort)
	if !ok {
		return ESIMSnapshot{}, unsupported("esim", "esim_snapshot")
	}
	return port.ESIMSnapshot(ctx)
}

func (a *ATBackend) Download(ctx context.Context, activationCode, confirmationCode, matchingID string, opts *ESIMDownloadOptions) error {
	if a.esimPort == nil {
		return unsupported("esim", "esim_download")
	}
	return a.esimPort.Download(ctx, activationCode, confirmationCode, matchingID, opts)
}

func (a *ATBackend) Enable(ctx context.Context, iccid string) error {
	if a.esimPort == nil {
		return unsupported("esim", "esim_enable")
	}
	return a.esimPort.Enable(ctx, iccid)
}

func (a *ATBackend) Disable(ctx context.Context, iccid string) error {
	if a.esimPort == nil {
		return unsupported("esim", "esim_disable")
	}
	return a.esimPort.Disable(ctx, iccid)
}

func (a *ATBackend) Rename(ctx context.Context, iccid, label string) error {
	if a.esimPort == nil {
		return unsupported("esim", "esim_rename")
	}
	return a.esimPort.Rename(ctx, iccid, label)
}

func (a *ATBackend) Delete(ctx context.Context, iccid string) error {
	if a.esimPort == nil {
		return unsupported("esim", "esim_delete")
	}
	return a.esimPort.Delete(ctx, iccid)
}

func (a *ATBackend) ListNotifications(ctx context.Context) ([]NotificationItem, error) {
	if a.esimPort == nil {
		return nil, unsupported("esim", "esim_notifications")
	}
	return a.esimPort.ListNotifications(ctx)
}

func (a *ATBackend) ProcessNotification(ctx context.Context, sequenceNumber int64) error {
	if a.esimPort == nil {
		return unsupported("esim", "esim_notifications")
	}
	return a.esimPort.ProcessNotification(ctx, sequenceNumber)
}

func (a *ATBackend) RemoveNotification(ctx context.Context, sequenceNumber int64) error {
	if a.esimPort == nil {
		return unsupported("esim", "esim_notifications")
	}
	return a.esimPort.RemoveNotification(ctx, sequenceNumber)
}

func (a *ATBackend) RawAT(ctx context.Context, command string) (string, error) {
	if a == nil || a.modem == nil {
		return "", fmt.Errorf("AT backend is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	timeout := 30 * time.Second
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < timeout {
		timeout = time.Until(deadline)
	}
	if timeout <= 0 {
		return "", ctx.Err()
	}
	return a.modem.ExecuteATRaw(command, timeout)
}

// Events adapts the manager's modem-reset signal to the runtime backend event
// contract. The AT manager remains responsible for command serialization and
// transport ownership; this adapter only publishes lifecycle signals.
func (a *ATBackend) Events(ctx context.Context) (<-chan BackendEvent, error) {
	if a == nil || a.modem == nil {
		return nil, fmt.Errorf("AT backend is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	a.eventsOnce.Do(func() {
		a.eventsCh = make(chan BackendEvent, 8)
		a.eventsDone = make(chan struct{})
		ready := a.modem.SubscribeRDY()
		go func() {
			defer close(a.eventsCh)
			for {
				select {
				case <-ctx.Done():
					return
				case <-a.eventsDone:
					return
				case <-ready:
					select {
					case a.eventsCh <- BackendEvent{Type: "modem.reset"}:
					case <-ctx.Done():
						return
					case <-a.eventsDone:
						return
					default:
						// A slow event consumer must not stall the AT command
						// loop; count the drop instead of blocking.
						a.eventsDropped.Add(1)
					}
					// RDY 订阅是一次性的: 触发后 channel 被关闭, 已关闭的
					// channel 会让 select 空转 (忙循环) 并漏报后续重启。
					// 每次收到后重新订阅, 保证周期性模组重启都能上报。
					ready = a.modem.SubscribeRDY()
				}
			}
		}()
	})
	return a.eventsCh, nil
}

// EventDrops reports the cumulative count of modem-reset events dropped for a
// slow subscriber since this backend started.
func (a *ATBackend) EventDrops() uint64 { return a.eventsDropped.Load() }

// ============================================================================
// DeviceInfoProvider 实现
// ============================================================================

func (a *ATBackend) GetIMEI(ctx context.Context) (string, error) {
	return a.modem.QueryIMEI()
}

func (a *ATBackend) GetIMSI(ctx context.Context) (string, error) {
	return a.modem.QueryIMSI()
}

// GetIMSILive AT 模式下 IMSI 本身即实时读取。
func (a *ATBackend) GetIMSILive(ctx context.Context) (string, error) {
	return a.modem.QueryIMSI()
}

func (a *ATBackend) GetICCID(ctx context.Context) (string, error) {
	return a.modem.QueryICCID()
}

func (a *ATBackend) GetMSISDN(ctx context.Context) (string, error) {
	return a.modem.QueryMSISDN()
}

// GetICCIDLive AT 模式下 ICCID 本身即实时读取。
func (a *ATBackend) GetICCIDLive(ctx context.Context) (string, error) {
	return a.modem.QueryICCID()
}

func (a *ATBackend) GetRevision(ctx context.Context) (string, error) {
	return a.modem.QueryFirmware()
}

func (a *ATBackend) GetFirmwareRevision(ctx context.Context) (string, string, bool, error) {
	revision, err := a.modem.QueryFirmwareRevision()
	if err != nil {
		return "", "", false, err
	}
	return revision.Value, revision.Source, revision.Live, nil
}

// GetSignalInfo sends AT+CSQ for RSSI and AT+QENG="servingcell" for LTE
// RSRP, RSRQ, and SINR on an AT transport.
func (a *ATBackend) GetSignalInfo(ctx context.Context) (*SignalInfo, error) {
	info := &SignalInfo{}

	// AT+CSQ → RSSI/dBm
	if _, dbm, err := a.modem.QueryCSQ(); err == nil {
		info.RSSI = dbm
	}

	// AT+QENG="servingcell" → RSRP/RSRQ/SINR
	if cell, err := a.modem.QueryServingCellLTEInfo(); err == nil {
		info.RSRP = cell.RSRP
		info.RSRQ = cell.RSRQ
		info.SINR = cell.SINR
	}

	return info, nil
}

// GetServingSystem sends AT+CEREG?/AT+CGREG?/AT+CREG? for registration,
// AT+COPS? for the operator, and AT+QNWINFO for mode, duplex, band, and
// channel on an AT transport.
func (a *ATBackend) GetServingSystem(ctx context.Context) (*ServingSystem, error) {
	ss := &ServingSystem{}

	// CEREG/CGREG/CREG → 分组域或电路域注册状态、LAC、CellID
	if regStatus, regText, lac, cellID, err := a.modem.QueryRegistration(); err == nil {
		ss.RegStatus = regStatus
		ss.RegStatusText = regText
		ss.LAC = lac
		ss.CellID = cellID
	}

	// AT+COPS? → 运营商
	if operator, err := a.modem.QueryOperator(); err == nil {
		ss.Operator = operator
	}

	// AT+QNWINFO → 网络模式 / 双工方式 / 频段 / 信道
	if mode, duplex, band, channel, err := a.modem.QueryNetworkRadio(); err == nil {
		ss.NetworkMode = mode
		ss.NetworkDuplex = duplex
		ss.RadioBand = band
		ss.RadioChannel = channel
	}

	return ss, nil
}

// Status implements NetworkPort for serial and injected AT transports. It sends
// AT+QCFG="usbnet"? and then calls GetServingSystem and GetSignalInfo, which
// add registration/operator/radio commands plus AT+CSQ and
// AT+QENG="servingcell".
func (a *ATBackend) Status(ctx context.Context) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	mode, err := a.modem.QueryUSBNetMode()
	if err != nil {
		return nil, err
	}
	result := map[string]any{"mode": strconv.Itoa(mode)}
	if radio, radioErr := a.GetServingSystem(ctx); radioErr == nil {
		result["network_mode"] = radio.NetworkMode
		result["radio_band"] = radio.RadioBand
		result["registered"] = radio.RegStatus == 1 || radio.RegStatus == 5
	}
	if signal, signalErr := a.GetSignalInfo(ctx); signalErr == nil {
		result["signal_dbm"] = signal.RSSI
		result["signal_rsrp"] = signal.RSRP
		result["signal_rsrq"] = signal.RSRQ
		result["signal_sinr"] = signal.SINR
	}
	return result, nil
}

func (a *ATBackend) SetMode(ctx context.Context, mode string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	value, err := strconv.Atoi(strings.TrimSpace(mode))
	if err != nil || value < 0 || value > 3 {
		return fmt.Errorf("usbnet mode must be an integer from 0 to 3")
	}
	return a.modem.SetUSBNetMode(value)
}

func (a *ATBackend) Traffic(context.Context) (map[string]any, error) {
	// The serial AT channel has no host byte counters. Keep the contract
	// stable; host counters are supplied by platform-specific controllers.
	return map[string]any{"rx_bytes": uint64(0), "tx_bytes": uint64(0)}, nil
}

func (a *ATBackend) Check(ctx context.Context) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	radio, err := a.GetServingSystem(ctx)
	if err != nil {
		return nil, err
	}
	if radio.RegStatus == 1 || radio.RegStatus == 5 {
		return map[string]any{"ok": true, "summary": "cellular registration is ready", "detail": radio.NetworkMode}, nil
	}
	return map[string]any{"ok": false, "summary": "cellular network is not registered"}, nil
}

func (a *ATBackend) IsSimInserted(ctx context.Context) (bool, error) {
	return a.modem.QuerySIMInserted()
}

func (a *ATBackend) GetNativeMCCMNC(ctx context.Context) (mcc, mnc string, err error) {
	return a.modem.QueryNativeMCCMNC()
}

func (a *ATBackend) GetNativeSPN(ctx context.Context) (string, error) {
	return a.modem.QueryNativeSPN()
}

func (a *ATBackend) GetNativeSPNLive(ctx context.Context) (string, error) {
	return a.modem.QueryNativeSPN()
}

func (a *ATBackend) GetSIMMetadata(ctx context.Context) (*SIMMetadata, error) {
	meta, err := a.modem.QuerySIMMetadata()
	return mapModemSIMMetadata(meta), err
}

func (a *ATBackend) GetSIMMetadataLive(ctx context.Context) (*SIMMetadata, error) {
	return a.GetSIMMetadata(ctx)
}

func mapModemSIMMetadata(meta *modem.SIMMetadata) *SIMMetadata {
	if meta == nil {
		return nil
	}
	out := &SIMMetadata{
		NativeMCC: meta.NativeMCC,
		NativeMNC: meta.NativeMNC,
		GID1:      meta.GID1,
		GID2:      meta.GID2,
	}
	if len(meta.PNN) > 0 {
		out.PNN = make([]PNNRecord, 0, len(meta.PNN))
		for _, rec := range meta.PNN {
			out.PNN = append(out.PNN, PNNRecord(rec))
		}
	}
	if len(meta.OPL) > 0 {
		out.OPL = make([]OPLRecord, 0, len(meta.OPL))
		for _, rec := range meta.OPL {
			out.OPL = append(out.OPL, OPLRecord(rec))
		}
	}
	if meta.SIMServiceTable != nil {
		out.ServiceTable = (*SIMServiceTable)(meta.SIMServiceTable)
	}
	return out
}

// GetSMSC 读取短信中心号码（AT+CSCA?）。
func (a *ATBackend) GetSMSC(ctx context.Context) (string, error) {
	return a.modem.QuerySMSC()
}

// ============================================================================
// SMSProvider 实现
// ============================================================================

func (a *ATBackend) SendSMS(ctx context.Context, to, body string) error {
	return a.SendSMSWithOptions(ctx, to, body, smscodec.SubmitOptions{})
}

func (a *ATBackend) SendSMSWithOptions(ctx context.Context, to, body string, opts smscodec.SubmitOptions) error {
	return a.modem.SendSMSWithOptions(to, body, opts)
}

// SetInboundSMSHandler forwards the consumer registration to the AT manager's
// +CMTI hook, converting the (storage, index) strings to NewSMSRef. nil
// unregisters the consumer so +CMTI entries are retained without auto-delete.
func (a *ATBackend) SetInboundSMSHandler(handler InboundSMSHandler) {
	if a == nil || a.modem == nil {
		return
	}
	if handler == nil {
		a.modem.SetNewSMSHandler(nil)
		return
	}
	a.modem.SetNewSMSHandler(func(storage, index string) {
		idx, err := strconv.Atoi(index)
		if err != nil {
			logger.Warn("[at_backend] 新短信索引非法，跳过交付", "index", index)
			return
		}
		handler(NewSMSRef{Storage: storage, Index: idx})
	})
}

// ReadSMS 按存储引用读取并解码短信 PDU。PDU 解析失败返回错误而不删除条目，
// 由消费者保留以便刷新重试。
func (a *ATBackend) ReadSMS(ctx context.Context, ref NewSMSRef) (*SMS, error) {
	pdu, err := a.modem.ReadSMSFromStorage(ref.Storage, uint32(ref.Index))
	if err != nil {
		return nil, err
	}
	if pdu == "" {
		return nil, fmt.Errorf("短信 %d 不存在或为空", ref.Index)
	}
	sender, content, timestamp, concat, err := smscodec.DecodeDeliverPDUHex(pdu)
	if err != nil {
		return nil, fmt.Errorf("短信 %d PDU 解码失败: %w", ref.Index, err)
	}
	return &SMS{
		Index:      ref.Index,
		Sender:     sender,
		Content:    content,
		Timestamp:  timestamp,
		ConcatRef:  concat.Ref,
		PartNumber: concat.Seq,
		TotalParts: concat.Total,
	}, nil
}

func (a *ATBackend) DeleteSMS(ctx context.Context, ref NewSMSRef) error {
	return a.modem.DeleteSMSFromStorage(ref.Storage, uint32(ref.Index))
}

// ListSMS 返回全部短信概要：真实存储索引 + 解码出的时间戳/内容（+CMGL 一次
// 返回全部 PDU，解码无额外往返），供上层合并与去重。
func (a *ATBackend) ListSMS(ctx context.Context) ([]SMSSummary, error) {
	entries, err := a.modem.SMSListAllPDU()
	if err != nil {
		return nil, err
	}
	result := make([]SMSSummary, 0, len(entries))
	for _, entry := range entries {
		sender, content, timestamp, concat, decodeErr := smscodec.DecodeDeliverPDUHex(entry.PDU)
		if decodeErr != nil {
			// 无法解码的条目保留在模组存储中，不进入上层摘要。
			logger.Debug("[at_backend] 列表条目 PDU 解码失败，保留条目", "index", entry.Index, "err", decodeErr)
			continue
		}
		result = append(result, SMSSummary{
			Index:      int(entry.Index),
			ReceivedAt: timestamp,
			Sender:     sender,
			Body:       content,
			ConcatRef:  concat.Ref,
			PartNumber: concat.Seq,
			TotalParts: concat.Total,
		})
	}
	return result, nil
}

func (a *ATBackend) DeleteAllSMS(ctx context.Context) error {
	return a.modem.SMSDeleteAll()
}

// ============================================================================
// USSDProvider 实现
// ============================================================================

func (a *ATBackend) ExecuteUSSD(ctx context.Context, command string, timeout time.Duration) (*USSDResult, error) {
	result, err := a.modem.ExecuteUSSD(command, timeout)
	if err != nil {
		return nil, err
	}
	return modemUSSDResult(result), nil
}

func (a *ATBackend) CancelUSSD(ctx context.Context) error {
	a.modem.CancelUSSD()
	return nil
}

// ============================================================================
// OperatingModeController 实现
// ============================================================================

func (a *ATBackend) SetOperatingMode(ctx context.Context, mode OperatingMode) error {
	cmd := fmt.Sprintf("AT+CFUN=%d", int(mode))
	_, err := a.modem.ExecuteAT(cmd, 5*time.Second)
	return err
}

func (a *ATBackend) GetOperatingMode(ctx context.Context) (OperatingMode, error) {
	resp, err := a.modem.ExecuteATSilent("AT+CFUN?", 2*time.Second)
	if err != nil {
		return ModeOnline, err
	}
	// 解析 +CFUN: N
	var mode int
	if _, err := fmt.Sscanf(resp, "+CFUN: %d", &mode); err != nil {
		return ModeOnline, fmt.Errorf("解析 CFUN 响应失败: %s", resp)
	}
	return OperatingMode(mode), nil
}

func (a *ATBackend) Reboot(ctx context.Context) error {
	_, err := a.modem.ExecuteAT("AT+CFUN=1,1", 5*time.Second)
	return err
}

// ============================================================================
// SIMAuthProvider 实现
// ============================================================================

func (a *ATBackend) OpenLogicalChannel(ctx context.Context, aid string) (int, error) {
	return a.modem.OpenSIMAuthLogicalChannel(aid)
}

func (a *ATBackend) ResolveSIMAuthAID(ctx context.Context, app string, fallbackAID string) (string, string, error) {
	return a.modem.ResolveSIMAuthAID(app, fallbackAID)
}

func (a *ATBackend) CloseLogicalChannel(ctx context.Context, channelID int) error {
	return a.modem.CloseSIMAuthLogicalChannel(channelID)
}

func (a *ATBackend) TransmitAPDU(ctx context.Context, channelID int, command string) (string, error) {
	return a.modem.TransmitAPDU(channelID, command)
}
