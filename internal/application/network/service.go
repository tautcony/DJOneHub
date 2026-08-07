package network

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vohive/internal/application/device"
	"github.com/iniwex5/vohive/internal/application/notification"
	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/backend"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/runtime"
	"github.com/iniwex5/vohive/internal/storage"
	"github.com/iniwex5/vohive/internal/transport"
)

type Service struct {
	devices    *device.Service
	ops        *operation.Manager
	runtime    *runtime.Runtime
	controller transport.NetworkController
	store      *storage.SQLiteStore

	mu            sync.Mutex
	lastPublished *notification.NetworkUpdateEvent
	// lastTrafficPublished 记录上一个已发布的流量样本, 用于去重发布。
	lastTrafficPublished *TrafficUpdateEvent
	iccid         string
	iccidChecked  time.Time

	// stopMu guards the cancel/done pair created by Start, following the
	// notification service's Stop pattern so shutdown can join both pollers
	// before storage closes underneath them.
	stopMu sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

const EventTrafficUpdated = "network.traffic.updated"

type TrafficUpdateEvent struct {
	RXBytes        uint64    `json:"rx_bytes"`
	TXBytes        uint64    `json:"tx_bytes"`
	DailyRXBytes   uint64    `json:"daily_rx_bytes"`
	DailyTXBytes   uint64    `json:"daily_tx_bytes"`
	DailyAvailable bool      `json:"daily_available"`
	SampledAt      time.Time `json:"sampled_at"`
}

type TrafficDailyStatus struct {
	Date      string    `json:"date"`
	RXBytes   uint64    `json:"rx_bytes"`
	TXBytes   uint64    `json:"tx_bytes"`
	SampledAt time.Time `json:"sampled_at,omitempty"`
	Available bool      `json:"available"`
}

type TrafficDailyPoint struct {
	Date    string `json:"date"`
	RXBytes uint64 `json:"rx_bytes"`
	TXBytes uint64 `json:"tx_bytes"`
}

type TrafficRangeStatus struct {
	Range     string              `json:"range"`
	StartDate string              `json:"start_date"`
	EndDate   string              `json:"end_date"`
	Items     []TrafficDailyPoint `json:"items"`
	Available bool                `json:"available"`
}

func NewService(devices *device.Service, ops *operation.Manager, runtime *runtime.Runtime, controller transport.NetworkController, store ...*storage.SQLiteStore) *Service {
	service := &Service{devices: devices, ops: ops, runtime: runtime, controller: controller}
	if len(store) > 0 {
		service.store = store[0]
	}
	return service
}

// Start runs the periodic radio refresh that drives the 4G menu bar model,
// replacing the legacy notifier's 15-second cellular polling. It stores the
// cancel/done pair that Stop uses to join both pollers.
func (s *Service) Start(ctx context.Context) {
	s.stopMu.Lock()
	if s.cancel != nil {
		s.stopMu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.cancel = cancel
	s.done = done
	s.stopMu.Unlock()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		s.poller(runCtx)
	}()
	go func() {
		defer wg.Done()
		s.trafficPoller(runCtx)
	}()
	go func() {
		wg.Wait()
		close(done)
	}()
}

// Stop cancels both pollers and waits for them to join within the deadline,
// so a mid-refresh poller never writes to a closed store. Repeated calls are
// safe.
func (s *Service) Stop(ctx context.Context) error {
	s.stopMu.Lock()
	cancel := s.cancel
	done := s.done
	s.cancel = nil
	s.done = nil
	s.stopMu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	if done != nil {
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (s *Service) trafficPoller(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.publishTraffic(ctx)
		}
	}
}

func (s *Service) publishTraffic(ctx context.Context) {
	value, err := s.Traffic(ctx)
	if err != nil {
		return
	}
	sampledAt := time.Now().UTC()
	rxBytes := uint64Value(value["rx_bytes"])
	txBytes := uint64Value(value["tx_bytes"])
	event := TrafficUpdateEvent{RXBytes: rxBytes, TXBytes: txBytes, SampledAt: sampledAt}
	if s.store != nil {
		if iccid, iccidErr := s.currentICCID(ctx); iccidErr == nil && iccid != "" {
			if daily, dailyErr := s.store.RecordTrafficSample(iccid, sampledAt, rxBytes, txBytes); dailyErr == nil {
				event.DailyRXBytes = daily.RXBytes
				event.DailyTXBytes = daily.TXBytes
				event.DailyAvailable = true
			}
		}
	}
	// 采样值未变化时不重复发布事件 (design D17): 1s 轮询下不变化的样本
	// 不应制造无意义的 WS 流量与前端重绘。
	s.mu.Lock()
	unchanged := s.lastTrafficPublished != nil &&
		s.lastTrafficPublished.RXBytes == event.RXBytes &&
		s.lastTrafficPublished.TXBytes == event.TXBytes &&
		s.lastTrafficPublished.DailyRXBytes == event.DailyRXBytes &&
		s.lastTrafficPublished.DailyTXBytes == event.DailyTXBytes &&
		s.lastTrafficPublished.DailyAvailable == event.DailyAvailable
	if !unchanged {
		copy := event
		s.lastTrafficPublished = &copy
	}
	s.mu.Unlock()
	if unchanged {
		return
	}
	s.ops.Publish(EventTrafficUpdated, event)
}

func (s *Service) currentICCID(ctx context.Context) (string, error) {
	s.mu.Lock()
	if s.iccid != "" && time.Since(s.iccidChecked) < 15*time.Second {
		iccid := s.iccid
		s.mu.Unlock()
		return iccid, nil
	}
	s.mu.Unlock()

	b, err := s.devices.RequireCapability(domain.CapabilityNetworkStatus, "network_traffic_iccid")
	if err != nil {
		return "", err
	}
	sim, err := b.SIM(ctx)
	if err != nil {
		return "", err
	}
	iccid := strings.TrimSpace(sim.ICCID)
	if iccid == "" {
		identity, identityErr := b.Identity(ctx)
		if identityErr != nil {
			return "", identityErr
		}
		iccid = strings.TrimSpace(identity.ICCID)
	}
	s.mu.Lock()
	s.iccid, s.iccidChecked = iccid, time.Now()
	s.mu.Unlock()
	return iccid, nil
}

func (s *Service) TrafficDaily(ctx context.Context, date time.Time) (TrafficDailyStatus, error) {
	if date.IsZero() {
		date = time.Now()
	}
	status := TrafficDailyStatus{Date: date.Local().Format("2006-01-02")}
	if s.store == nil {
		return status, nil
	}
	iccid, err := s.currentICCID(ctx)
	if err != nil {
		return status, err
	}
	if iccid == "" {
		return status, nil
	}
	record, found, err := s.store.TrafficDaily(iccid, status.Date)
	if err != nil {
		return status, err
	}
	if !found {
		return status, nil
	}
	status.RXBytes, status.TXBytes = record.RXBytes, record.TXBytes
	status.SampledAt, status.Available = record.LastSampledAt, true
	return status, nil
}

func (s *Service) TrafficDailyRange(ctx context.Context, period string, now time.Time) (TrafficRangeStatus, error) {
	if now.IsZero() {
		now = time.Now()
	}
	now = now.In(time.Local)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	start, end := today, today
	switch period {
	case "day":
	case "week":
		start = today.AddDate(0, 0, -6)
	case "month":
		start = today.AddDate(0, 0, -29)
	default:
		return TrafficRangeStatus{}, fmt.Errorf("unsupported traffic range %q", period)
	}
	status := TrafficRangeStatus{
		Range: period, StartDate: start.Format("2006-01-02"), EndDate: end.Format("2006-01-02"),
		Items: []TrafficDailyPoint{},
	}
	for date := start; !date.After(end); date = date.AddDate(0, 0, 1) {
		status.Items = append(status.Items, TrafficDailyPoint{Date: date.Format("2006-01-02")})
	}
	if s.store == nil {
		return status, nil
	}
	iccid, err := s.currentICCID(ctx)
	if err != nil {
		return status, err
	}
	if iccid == "" {
		return status, nil
	}
	records, err := s.store.ListTrafficDaily(iccid, status.StartDate, status.EndDate)
	if err != nil {
		return status, err
	}
	byDate := make(map[string]storage.TrafficDailyRecord, len(records))
	for _, record := range records {
		byDate[record.UsageDate] = record
	}
	for index := range status.Items {
		if record, ok := byDate[status.Items[index].Date]; ok {
			status.Items[index].RXBytes = record.RXBytes
			status.Items[index].TXBytes = record.TXBytes
			status.Available = true
		}
	}
	return status, nil
}

func (s *Service) poller(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	select {
	case <-ctx.Done():
		return
	case <-time.After(5 * time.Second):
	}
	for {
		s.publishRadioState(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) publishRadioState(ctx context.Context) {
	b, err := s.devices.RequireCapability(domain.CapabilityNetworkStatus, "network_radio")
	if err != nil {
		return
	}
	radio, err := b.Radio(ctx)
	sim, simErr := b.SIM(ctx)
	if err != nil && simErr != nil {
		return
	}
	state := notification.NetworkUpdateEvent{
		NetworkMode: radio.NetworkMode,
		Registered:  radio.Registered,
		SIMInserted: sim.Inserted,
		SIMKnown:    simErr == nil,
		Operator:    radio.Operator,
		SignalDBM:   radio.SignalDBM,
	}
	s.mu.Lock()
	changed := s.lastPublished == nil || *s.lastPublished != state
	if changed {
		s.lastPublished = &state
	}
	s.mu.Unlock()
	if changed {
		s.ops.Publish(notification.EventNetworkUpdated, state)
	}
}

func (s *Service) Status(ctx context.Context) (transport.NetworkStatus, error) {
	b, err := s.devices.RequireCapability(domain.CapabilityNetworkStatus, "network_status")
	if err != nil {
		return transport.NetworkStatus{}, err
	}
	var status transport.NetworkStatus
	hasBackendStatus := false
	if port, ok := b.(backend.NetworkPort); ok {
		value, err := port.Status(ctx)
		if err != nil {
			return transport.NetworkStatus{}, err
		}
		status = mapStatus(value)
		hasBackendStatus = true
	}
	if status.NetworkMode == "" || status.RadioBand == "" {
		if radio, radioErr := b.Radio(ctx); radioErr == nil {
			if status.NetworkMode == "" {
				status.NetworkMode = radio.NetworkMode
			}
			if status.RadioBand == "" {
				status.RadioBand = radio.RadioBand
			}
		}
	}
	candidate, err := s.runtime.Candidate()
	if err != nil {
		if hasBackendStatus {
			return status, nil
		}
		return transport.NetworkStatus{}, err
	}
	if s.controller != nil {
		platformStatus, platformErr := s.controller.Status(ctx, candidate)
		if platformErr == nil {
			mergeHostStatus(&status, platformStatus)
			return status, nil
		}
		if hasBackendStatus {
			return status, nil
		}
	}
	if hasBackendStatus {
		return status, nil
	}
	return transport.NetworkStatus{}, fmt.Errorf("network controller is unavailable")
}

func (s *Service) SetMode(ctx context.Context, mode string) (string, error) {
	b, err := s.devices.RequireCapability(domain.CapabilityNetworkControl, "network_set_mode")
	if err != nil {
		return "", err
	}
	return s.ops.Start(ctx, "network.set_mode", func(taskCtx context.Context, _ string, progress func(int, string)) error {
		release, err := s.runtime.Acquire(taskCtx, runtime.ResourceNetwork)
		if err != nil {
			return err
		}
		defer release()
		progress(10, "applying network mode")
		if port, ok := b.(backend.NetworkPort); ok {
			if err := port.SetMode(taskCtx, mode); err != nil {
				return err
			}
		} else {
			candidate, err := s.runtime.Candidate()
			if err != nil {
				return err
			}
			if s.controller == nil {
				return fmt.Errorf("network controller is unavailable")
			}
			if err := s.controller.SetMode(taskCtx, candidate, mode); err != nil {
				return err
			}
		}
		if err := taskCtx.Err(); err != nil {
			return err
		}
		progress(100, "network mode applied")
		s.ops.Publish("network.updated", map[string]any{"mode": mode})
		return nil
	})
}

func (s *Service) Check(ctx context.Context) (transport.Connectivity, error) {
	b, err := s.devices.RequireCapability(domain.CapabilityNetworkStatus, "network_check")
	if err != nil {
		return transport.Connectivity{}, err
	}
	if candidate, candidateErr := s.runtime.Candidate(); candidateErr == nil && s.controller != nil {
		if value, platformErr := s.controller.CheckConnectivity(ctx, candidate); platformErr == nil {
			return value, nil
		}
	}
	if port, ok := b.(backend.NetworkPort); ok {
		value, err := port.Check(ctx)
		if err != nil {
			return transport.Connectivity{}, err
		}
		return transport.Connectivity{
			OK:      boolValue(value["ok"]),
			Summary: stringValue(value["summary"]),
			Detail:  stringValue(value["detail"]),
		}, nil
	}
	candidate, err := s.runtime.Candidate()
	if err != nil {
		return transport.Connectivity{}, err
	}
	if s.controller == nil {
		return transport.Connectivity{}, fmt.Errorf("network controller is unavailable")
	}
	return s.controller.CheckConnectivity(ctx, candidate)
}

func (s *Service) Traffic(ctx context.Context) (map[string]any, error) {
	b, err := s.devices.RequireCapability(domain.CapabilityNetworkStatus, "network_traffic")
	if err != nil {
		return nil, err
	}
	candidate, err := s.runtime.Candidate()
	if err == nil && s.controller != nil {
		if reader, ok := s.controller.(transport.NetworkTrafficReader); ok {
			rxBytes, txBytes, trafficErr := reader.NetworkTraffic(ctx, candidate)
			if trafficErr == nil {
				return map[string]any{"rx_bytes": rxBytes, "tx_bytes": txBytes}, nil
			}
		}
		status, platformErr := s.controller.Status(ctx, candidate)
		if platformErr == nil {
			return map[string]any{"rx_bytes": status.RXBytes, "tx_bytes": status.TXBytes}, nil
		}
	}
	if port, ok := b.(backend.NetworkPort); ok {
		return port.Traffic(ctx)
	}
	return nil, fmt.Errorf("network controller is unavailable")
}

func (s *Service) Diagnostics(ctx context.Context) (map[string]any, error) {
	adapter, ok := s.controller.(transport.NetworkDiagnostics)
	if !ok {
		return nil, derrors.CapabilityMissing("network_diagnostics", "network_diagnostics", "the platform does not expose network diagnostics")
	}
	candidate, err := s.runtime.Candidate()
	if err != nil {
		return nil, err
	}
	value, err := adapter.Diagnostics(ctx, candidate)
	if err != nil {
		return nil, err
	}
	if raw, ok := s.backendRaw(ctx); ok {
		commands := map[string]string{"usbnet": `AT+QCFG="usbnet"`, "usbcfg": `AT+QCFG="usbcfg"`, "cgdcont": "AT+CGDCONT?", "cgact": "AT+CGACT?", "cgpaddr": "AT+CGPADDR=1"}
		rawValues := map[string]string{}
		errors := map[string]string{}
		for name, command := range commands {
			response, commandErr := raw.RawAT(ctx, command)
			if commandErr != nil {
				errors[name] = commandErr.Error()
			} else {
				rawValues[name] = response
			}
		}
		value["raw"] = rawValues
		value["errors"] = errors
		value["usbnet_mode"] = rawValues["usbnet"]
		value["usbcfg"] = rawValues["usbcfg"]
		value["pdp_contexts"] = rawValues["cgdcont"]
		value["active_contexts"] = rawValues["cgact"]
		value["pdp_addresses"] = rawValues["cgpaddr"]
	}
	return value, nil
}

func (s *Service) backendRaw(ctx context.Context) (backend.RawATBackend, bool) {
	value, err := s.devices.RequireCapability(domain.CapabilityRawAT, "network_diagnostics")
	if err != nil {
		return nil, false
	}
	raw, ok := value.(backend.RawATBackend)
	return raw, ok
}

func mapStatus(value map[string]any) transport.NetworkStatus {
	return transport.NetworkStatus{
		Mode:               stringValue(value["mode"]),
		NetworkMode:        stringValue(value["network_mode"]),
		RadioBand:          stringValue(value["radio_band"]),
		Interface:          stringValue(value["interface"]),
		DefaultRoute:       stringValue(value["default_route"]),
		SystemDefaultRoute: stringValue(value["system_default_route"]),
		Addresses:          stringSlice(value["addresses"]),
		RXBytes:            uint64Value(value["rx_bytes"]),
		TXBytes:            uint64Value(value["tx_bytes"]),
	}
}

func mergeHostStatus(status *transport.NetworkStatus, host transport.NetworkStatus) {
	if status.Mode == "" {
		status.Mode = host.Mode
	}
	if status.NetworkMode == "" {
		status.NetworkMode = host.NetworkMode
	}
	if status.RadioBand == "" {
		status.RadioBand = host.RadioBand
	}
	if host.Interface != "" {
		status.Interface = host.Interface
	}
	if host.Addresses != nil {
		status.Addresses = append([]string(nil), host.Addresses...)
	}
	if host.DefaultRoute != "" {
		status.DefaultRoute = host.DefaultRoute
	}
	if host.SystemDefaultRoute != "" {
		status.SystemDefaultRoute = host.SystemDefaultRoute
	}
	status.RXBytes = host.RXBytes
	status.TXBytes = host.TXBytes
}

func stringValue(value any) string {
	if result, ok := value.(string); ok {
		return result
	}
	return ""
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}

func uint64Value(value any) uint64 {
	switch result := value.(type) {
	case uint64:
		return result
	case int:
		if result > 0 {
			return uint64(result)
		}
	case float64:
		if result > 0 {
			return uint64(result)
		}
	}
	return 0
}

func stringSlice(value any) []string {
	values, ok := value.([]string)
	if ok {
		return values
	}
	return nil
}
