package windows

import (
	"github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/platform/unsupported"
)

type Adapter struct{ *unsupported.Adapter }

func New() *Adapter {
	return &Adapter{Adapter: unsupported.New("windows", device.CapabilitySet{
		device.CapabilityDeviceStatus: "Windows adapter is present; SetupAPI/MBIM hardware verification is pending",
	})}
}
