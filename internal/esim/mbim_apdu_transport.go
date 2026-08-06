package esim

import (
	"context"
	"time"

	"github.com/iniwex5/vohive/internal/apduarbiter"
)

type uiccTransport interface {
	ControlDevice() string
	OpenChannel(ctx context.Context, aid []byte) (uint32, error)
	CloseChannel(ctx context.Context, channel uint32) error
	TransmitAPDU(ctx context.Context, channel uint32, command []byte) ([]byte, error)
}

// MBIMAPDUTransport adapts MS UICC Low Level Access to QMIAPDUTransport.
type MBIMAPDUTransport struct {
	src   uiccTransport
	coord *apduCoordinator
}

func NewMBIMAPDUTransport(src uiccTransport) *MBIMAPDUTransport {
	return &MBIMAPDUTransport{src: src, coord: newAPDUCoordinator("MBIM")}
}

func (t *MBIMAPDUTransport) ControlDevice() string { return t.src.ControlDevice() }

func (t *MBIMAPDUTransport) SetAPDUArbiter(arbiter *apduarbiter.Arbiter) {
	t.coord.setArbiter(arbiter)
	if aware, ok := t.src.(interface {
		SetAPDUArbiter(*apduarbiter.Arbiter)
	}); ok {
		aware.SetAPDUArbiter(arbiter)
	}
}

func (t *MBIMAPDUTransport) OpenEUICCLogicalChannel(ctx context.Context, slot byte, aid []byte) (byte, error) {
	lease, err := t.coord.acquireLease(ctx, 10*time.Second, "esim_session_open", apduarbiter.APDUClassEUICCWrite, 0, apduarbiter.TransportScopeExclusive)
	if err != nil {
		return 0, err
	}
	if lease != nil {
		defer lease.Release()
		lease.Touch()
	}
	openMu := t.coord.getOrCreateChanMu(0)
	openMu.Lock()
	defer openMu.Unlock()

	ch, err := t.src.OpenChannel(ctx, aid)
	if err != nil {
		return 0, err
	}
	if lease != nil {
		lease.Touch()
	}
	t.coord.bindSession(byte(ch), "esim")
	return byte(ch), nil
}

func (t *MBIMAPDUTransport) CloseEUICCLogicalChannel(ctx context.Context, slot, channel byte) error {
	t.coord.takeSession(channel)
	lease, err := t.coord.acquireLease(ctx, 10*time.Second, "esim_session_close", apduarbiter.APDUClassEUICCWrite, channel, apduarbiter.TransportScopeExclusive)
	if err != nil {
		return err
	}
	if lease != nil {
		defer lease.Release()
		lease.Touch()
	}
	closeMu := t.coord.getOrCreateChanMu(0)
	closeMu.Lock()
	defer closeMu.Unlock()
	if err := t.src.CloseChannel(ctx, uint32(channel)); err != nil {
		return err
	}
	if lease != nil {
		lease.Touch()
	}
	return nil
}

// TransmitEUICCAPDU 保持 TransportScopeExclusive (每 APDU 独占):
// MBIM 传输有意与 QMI 的 per-channel 并发模型 (TransportScopeQMIChannel,
// 见 apduarbiter.arbiter.go normalizeScope) 保持不同 — 当前 MBIM 数据源
// (MBIMEx UICC service) 未证明并发 logical channel 彼此独立安全, 因此所有
// MBIM APDU 串行化, 且仍共享设备级 SIM 切换 barrier。若未来 MBIM 数据源
// 证明 per-channel 安全, 再改为 QMI 模型。
func (t *MBIMAPDUTransport) TransmitEUICCAPDU(ctx context.Context, slot, channel byte, command []byte) ([]byte, error) {
	lease, err := t.coord.acquireLease(ctx, 10*time.Second, "esim_apdu", apduarbiter.APDUClassEUICCWrite, channel, apduarbiter.TransportScopeExclusive)
	if err != nil {
		return nil, err
	}
	if lease != nil {
		defer lease.Release()
		lease.Touch()
	}
	chanMu := t.coord.getOrCreateChanMu(channel)
	chanMu.Lock()
	defer chanMu.Unlock()

	resp, err := t.src.TransmitAPDU(ctx, uint32(channel), command)
	if lease != nil {
		lease.Touch()
	}
	return resp, err
}
