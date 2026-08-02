package network

import (
	"context"
	"fmt"

	"github.com/iniwex5/vohive/internal/application/device"
	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/backend"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/runtime"
	"github.com/iniwex5/vohive/internal/transport"
)

type Service struct {
	devices    *device.Service
	ops        *operation.Manager
	runtime    *runtime.Runtime
	controller transport.NetworkController
}

func NewService(devices *device.Service, ops *operation.Manager, runtime *runtime.Runtime, controller transport.NetworkController) *Service {
	return &Service{devices: devices, ops: ops, runtime: runtime, controller: controller}
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
