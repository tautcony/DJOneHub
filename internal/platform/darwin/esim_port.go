package darwin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/damonto/euicc-go/driver"
	"github.com/iniwex5/vohive/internal/backend"
	djiesim "github.com/iniwex5/vohive/internal/esim"
)

// esimPort adapts the existing LPA manager to the application backend port.
// The manager creates a fresh APDU channel for each LPA session, while the
// underlying USB AT transport remains owned by CommandBackend.
type esimPort struct {
	manager *djiesim.Manager
}

func newESIMPort(candidateID string, command func(string, time.Duration) (string, error), imei func(context.Context) (string, error), iccid func(context.Context) (string, error)) (*esimPort, error) {
	manager, err := djiesim.NewManager(djiesim.ManagerOptions{
		DeviceID:             candidateID,
		Transport:            "custom",
		IMEIProvider:         imei,
		ICCIDProvider:        iccid,
		SwitchUseRefreshTrue: true,
		SmartCardChannelFactory: func() (driver.SmartCardChannel, error) {
			return newUSBATESIMChannel(command), nil
		},
	})
	if err != nil {
		return nil, err
	}
	return &esimPort{manager: manager}, nil
}

func (p *esimPort) Overview(context.Context) (*djiesim.EsimOverview, error) {
	if p == nil || p.manager == nil {
		return nil, fmt.Errorf("eSIM manager is unavailable")
	}
	return p.manager.GetEsimOverview()
}

func (p *esimPort) EID(ctx context.Context) (string, error) {
	overview, err := p.Overview(ctx)
	if err != nil {
		return "", err
	}
	if overview == nil || overview.ChipInfo == nil || len(overview.ChipInfo.EIDs) == 0 {
		return "", fmt.Errorf("eUICC EID is unavailable")
	}
	return overview.ChipInfo.EIDs[0].EID, nil
}

func (p *esimPort) Profiles(ctx context.Context) ([]backend.Profile, error) {
	overview, err := p.Overview(ctx)
	if err != nil {
		return nil, err
	}
	profiles := make([]backend.Profile, 0)
	for _, group := range overview.Profiles {
		for _, item := range group.Profiles {
			state := "unknown"
			if item.StateKnown {
				switch item.State {
				case 0:
					state = "disabled"
				case 1:
					state = "enabled"
				default:
					state = "unknown"
				}
			}
			var stateCode *int
			if item.StateKnown {
				code := item.State
				stateCode = &code
			}
			profiles = append(profiles, backend.Profile{
				ICCID:               item.ICCID,
				State:               state,
				StateCode:           stateCode,
				StateKnown:          item.StateKnown,
				Label:               firstNonEmpty(item.Name, item.ServiceProviderName),
				EID:                 group.EID,
				AID:                 group.AIDHex,
				ServiceProviderName: item.ServiceProviderName,
				ProfileClass:        item.ClassText,
			})
		}
	}
	if activeICCID, err := p.manager.CurrentICCID(ctx); err == nil {
		for index := range profiles {
			if strings.EqualFold(strings.TrimSpace(profiles[index].ICCID), strings.TrimSpace(activeICCID)) {
				profiles[index].State = "enabled"
				profiles[index].StateCode = intPointer(1)
				profiles[index].StateKnown = true
			}
		}
	}
	return profiles, nil
}

func (p *esimPort) Download(ctx context.Context, activationCode, confirmationCode, matchingID string) error {
	if p == nil || p.manager == nil {
		return fmt.Errorf("eSIM manager is unavailable")
	}
	_, err := p.manager.DownloadProfile(ctx, "", activationCode, matchingID, confirmationCode, "", nil)
	return err
}

func (p *esimPort) Enable(ctx context.Context, iccid string) error {
	if p == nil || p.manager == nil {
		return fmt.Errorf("eSIM manager is unavailable")
	}
	return p.manager.SwitchProfile(ctx, strings.TrimSpace(iccid), "")
}

func (p *esimPort) Rename(ctx context.Context, iccid, label string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || p.manager == nil {
		return fmt.Errorf("eSIM manager is unavailable")
	}
	return p.manager.RenameProfile(strings.TrimSpace(iccid), strings.TrimSpace(label), "")
}

func (p *esimPort) Delete(ctx context.Context, iccid string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || p.manager == nil {
		return fmt.Errorf("eSIM manager is unavailable")
	}
	_, err := p.manager.DeleteProfile(strings.TrimSpace(iccid), "")
	return err
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func intPointer(value int) *int { return &value }
