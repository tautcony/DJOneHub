package esim

import (
	"context"
	"strings"

	"github.com/iniwex5/vohive/internal/application/device"
	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/backend"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/runtime"
)

type Service struct {
	devices *device.Service
	ops     *operation.Manager
	runtime *runtime.Runtime
}

func NewService(devices *device.Service, ops *operation.Manager, runtime *runtime.Runtime) *Service {
	return &Service{devices: devices, ops: ops, runtime: runtime}
}

func (s *Service) port(operationName string) (backend.ESIMPort, error) {
	b, err := s.devices.RequireCapability(domain.CapabilityESIM, operationName)
	if err != nil {
		return nil, err
	}
	port, ok := b.(backend.ESIMPort)
	if !ok {
		return nil, derrors.CapabilityMissing("esim", operationName, "the selected backend has no eSIM service port")
	}
	return port, nil
}

func (s *Service) Overview(ctx context.Context) (map[string]any, error) {
	port, err := s.port("esim_overview")
	if err != nil {
		return nil, err
	}
	eid, err := port.EID(ctx)
	if err != nil {
		if isEUICCUnavailableProbeError(err) {
			return map[string]any{
				"card_type":   "unknown",
				"profiles":    []backend.Profile{},
				"probe_error": err.Error(),
				"message":     "eUICC profile service was not readable",
			}, nil
		}
		return nil, err
	}
	profiles, err := port.Profiles(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"card_type": "euicc", "eid": eid, "profiles": profiles}, nil
}

func isEUICCUnavailableProbeError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no euicc") ||
		(strings.Contains(message, "未发现任何 euicc") && strings.Contains(message, "at+ccho"))
}

func (s *Service) Download(ctx context.Context, activationCode, confirmationCode, matchingID string) (string, error) {
	port, err := s.port("esim_download")
	if err != nil {
		return "", err
	}
	return s.ops.Start(ctx, "esim.download", func(taskCtx context.Context, progress func(int, string)) error {
		release, err := s.runtime.Acquire(taskCtx, runtime.ResourceSIM)
		if err != nil {
			return err
		}
		defer release()
		progress(5, "preparing")
		if err := port.Download(taskCtx, activationCode, confirmationCode, matchingID); err != nil {
			return err
		}
		progress(100, "downloaded")
		s.ops.Publish("esim.updated", map[string]any{"operation": "download"})
		return nil
	})
}

func (s *Service) Enable(ctx context.Context, iccid string) (string, error) {
	port, err := s.port("esim_enable")
	if err != nil {
		return "", err
	}
	return s.ops.Start(ctx, "esim.enable", func(taskCtx context.Context, progress func(int, string)) error {
		release, err := s.runtime.Acquire(taskCtx, runtime.ResourceSIM)
		if err != nil {
			return err
		}
		defer release()
		progress(10, "switching profile")
		if err := port.Enable(taskCtx, iccid); err != nil {
			return err
		}
		progress(100, "profile enabled")
		s.ops.Publish("esim.updated", map[string]any{"operation": "enable", "iccid": iccid})
		return nil
	})
}

func (s *Service) Rename(ctx context.Context, iccid, label string) error {
	port, err := s.port("esim_rename")
	if err != nil {
		return err
	}
	if err := port.Rename(ctx, iccid, label); err != nil {
		return err
	}
	s.ops.Publish("esim.updated", map[string]any{"operation": "rename", "iccid": iccid})
	return nil
}

func (s *Service) Delete(ctx context.Context, iccid string) (string, error) {
	port, err := s.port("esim_delete")
	if err != nil {
		return "", err
	}
	return s.ops.Start(ctx, "esim.delete", func(taskCtx context.Context, progress func(int, string)) error {
		release, err := s.runtime.Acquire(taskCtx, runtime.ResourceSIM)
		if err != nil {
			return err
		}
		defer release()
		progress(10, "deleting profile")
		if err := port.Delete(taskCtx, iccid); err != nil {
			return err
		}
		progress(100, "profile deleted")
		s.ops.Publish("esim.updated", map[string]any{"operation": "delete", "iccid": iccid})
		return nil
	})
}
