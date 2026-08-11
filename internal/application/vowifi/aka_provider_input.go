package vowifi

import (
	"context"
	"fmt"
	"strings"

	"github.com/iniwex5/vohive/internal/backend"
	innersim "github.com/iniwex5/vohive/internal/sim"
	"github.com/iniwex5/vohive/pkg/mbim"
	"github.com/iniwex5/vowifi-go/runtimehost"
)

// 本文件移植自 vohive-open internal/device（vowifi_start_orchestrator.go 的
// workerAKAProviderInput）：把 DJOneHub 的后端（DeviceBackend + 运行时 Modem）
// 适配为 internal/sim.BuildAKAProvider 的输入接口。

// akaProviderInput 为 internal/sim.BuildAKAProvider 提供后端信息。
type akaProviderInput struct {
	db       voWiFiBackend
	modem    runtimehost.Modem
	deviceID string
}

func (w akaProviderInput) BackendMode() string {
	if w.db == nil {
		return ""
	}
	return w.db.Mode()
}

func (w akaProviderInput) MBIMAKAProvider() (innersim.BackendAKAProvider, bool) {
	if w.db == nil {
		return nil, false
	}
	provider, ok := w.db.(interface {
		CalculateAKA(ctx context.Context, rand16, autn16 []byte) (res, ik, ck, auts []byte, err error)
	})
	if !ok || !strings.EqualFold(w.db.Mode(), backend.BackendMBIM) {
		return nil, false
	}
	return provider, true
}

func (w akaProviderInput) MBIMCapability() (*mbim.Capabilities, bool) {
	if w.db == nil {
		return nil, false
	}
	cp, ok := w.db.(interface{ Capability() *mbim.Capabilities })
	if !ok {
		return nil, false
	}
	c := cp.Capability()
	return c, c != nil
}

func (w akaProviderInput) RuntimeModem() (innersim.ATModem, error) {
	modemIface := w.modem
	if modemIface == nil {
		return nil, fmt.Errorf("device %s runtime modem is nil", strings.TrimSpace(w.deviceID))
	}
	modem, ok := modemIface.(innersim.ATModem)
	if !ok {
		return nil, fmt.Errorf("device %s runtime modem does not implement sim.ATModem", strings.TrimSpace(w.deviceID))
	}
	return modem, nil
}
