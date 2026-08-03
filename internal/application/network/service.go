package network

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/iniwex5/vohive/internal/application/device"
	"github.com/iniwex5/vohive/internal/application/notification"
	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/backend"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/runtime"
	"github.com/iniwex5/vohive/internal/transport"
)

type Service struct {
	devices    *device.Service
	ops        *operation.Manager
	runtime    *runtime.Runtime
	controller transport.NetworkController

	mu            sync.Mutex
	lastPublished *notification.NetworkUpdateEvent
}

func NewService(devices *device.Service, ops *operation.Manager, runtime *runtime.Runtime, controller transport.NetworkController) *Service {
	return &Service{devices: devices, ops: ops, runtime: runtime, controller: controller}
}

// Start runs the periodic radio refresh that drives the 4G menu bar model,
// replacing the legacy notifier's 15-second cellular polling.
func (s *Service) Start(ctx context.Context) {
	go s.poller(ctx)
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
	if err != nil {
		return
	}
	state := notification.NetworkUpdateEvent{NetworkMode: radio.NetworkMode, Registered: radio.Registered, Operator: radio.Operator, SignalDBM: radio.SignalDBM}
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
	if port, ok := b.(backend.NetworkPort); ok {
		value, err := port.Status(ctx)
		if err != nil {
			return transport.NetworkStatus{}, err
		}
		return mapStatus(value), nil
	}
	candidate, err := s.runtime.Candidate()
	if err != nil {
		return transport.NetworkStatus{}, err
	}
	if s.controller == nil {
		return transport.NetworkStatus{}, fmt.Errorf("network controller is unavailable")
	}
	return s.controller.Status(ctx, candidate)
}

func (s *Service) SetMode(ctx context.Context, mode string) (string, error) {
	b, err := s.devices.RequireCapability(domain.CapabilityNetworkControl, "network_set_mode")
	if err != nil {
		return "", err
	}
	return s.ops.Start(ctx, "network.set_mode", func(taskCtx context.Context, progress func(int, string)) error {
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
	}), nil
}

func (s *Service) Check(ctx context.Context) (transport.Connectivity, error) {
	b, err := s.devices.RequireCapability(domain.CapabilityNetworkStatus, "network_check")
	if err != nil {
		return transport.Connectivity{}, err
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
	if port, ok := b.(backend.NetworkPort); ok {
		return port.Traffic(ctx)
	}
	candidate, err := s.runtime.Candidate()
	if err != nil {
		return nil, err
	}
	if s.controller == nil {
		return nil, fmt.Errorf("network controller is unavailable")
	}
	status, err := s.controller.Status(ctx, candidate)
	if err != nil {
		return nil, err
	}
	return map[string]any{"rx_bytes": status.RXBytes, "tx_bytes": status.TXBytes}, nil
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

func (s *Service) CheckRoute(ctx context.Context, kind string) (transport.Connectivity, error) {
	adapter, ok := s.controller.(transport.NetworkDiagnostics)
	if !ok {
		return transport.Connectivity{}, derrors.CapabilityMissing("network_diagnostics", "network_route_check", "the platform does not expose route checks")
	}
	candidate, err := s.runtime.Candidate()
	if err != nil {
		return transport.Connectivity{}, err
	}
	return adapter.CheckRoute(ctx, candidate, kind)
}

func (s *Service) CellularPolicy(ctx context.Context) (transport.CellularPolicy, error) {
	adapter, ok := s.controller.(transport.NetworkDiagnostics)
	if !ok {
		return transport.CellularPolicy{}, derrors.CapabilityMissing("network_policy", "network_policy", "the platform does not expose cellular policy control")
	}
	candidate, err := s.runtime.Candidate()
	if err != nil {
		return transport.CellularPolicy{}, err
	}
	return adapter.CellularPolicy(ctx, candidate)
}

func (s *Service) SetCellularPolicy(ctx context.Context, forceOff bool) (transport.CellularPolicy, error) {
	adapter, ok := s.controller.(transport.NetworkDiagnostics)
	if !ok {
		return transport.CellularPolicy{}, derrors.CapabilityMissing("network_policy", "network_policy", "the platform does not expose cellular policy control")
	}
	candidate, err := s.runtime.Candidate()
	if err != nil {
		return transport.CellularPolicy{}, err
	}
	value, err := adapter.SetCellularPolicy(ctx, candidate, forceOff)
	if err == nil {
		s.ops.Publish("network.updated", value)
	}
	return value, err
}

func mapStatus(value map[string]any) transport.NetworkStatus {
	return transport.NetworkStatus{
		Mode:         stringValue(value["mode"]),
		NetworkMode:  stringValue(value["network_mode"]),
		Interface:    stringValue(value["interface"]),
		DefaultRoute: stringValue(value["default_route"]),
		Addresses:    stringSlice(value["addresses"]),
		RXBytes:      uint64Value(value["rx_bytes"]),
		TXBytes:      uint64Value(value["tx_bytes"]),
	}
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
