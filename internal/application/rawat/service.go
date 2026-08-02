package rawat

import (
	"context"

	"github.com/iniwex5/vohive/internal/application/device"
	"github.com/iniwex5/vohive/internal/backend"
	domain "github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/runtime"
)

type Service struct {
	devices *device.Service
	runtime *runtime.Runtime
}

func NewService(devices *device.Service, runtime *runtime.Runtime) *Service {
	return &Service{devices: devices, runtime: runtime}
}

func (s *Service) Execute(ctx context.Context, command string) (string, error) {
	b, err := s.devices.RequireCapability(domain.CapabilityRawAT, "raw_at")
	if err != nil {
		return "", err
	}
	provider, ok := b.(backend.RawATBackend)
	if !ok {
		return "", backend.AdaptUnsupported("raw_at", "raw_at")
	}
	release, err := s.runtime.Acquire(ctx, runtime.ResourceAT)
	if err != nil {
		return "", err
	}
	defer release()
	response, err := provider.RawAT(ctx, command)
	if err == nil {
		// Raw AT is intentionally observable without exposing the command in
		// events, because commands can contain SIM or network credentials.
		s.runtime.Events().Publish("at.updated", map[string]any{"completed": true})
	}
	return response, err
}
