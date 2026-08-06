package backend

import (
	"context"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/testfixtures"
)

type contractLegacy struct {
	events chan BackendEvent
}

func (f *contractLegacy) Mode() string                                { return BackendAT }
func (f *contractLegacy) Close() error                                { return nil }
func (f *contractLegacy) GetIMEI(context.Context) (string, error)     { return "123456789012345", nil }
func (f *contractLegacy) GetIMSI(context.Context) (string, error)     { return testfixtures.IMSI, nil }
func (f *contractLegacy) GetICCID(context.Context) (string, error)    { return testfixtures.ICCID19, nil }
func (f *contractLegacy) GetMSISDN(context.Context) (string, error)   { return testfixtures.MSISDN, nil }
func (f *contractLegacy) GetRevision(context.Context) (string, error) { return "test", nil }
func (f *contractLegacy) GetSignalInfo(context.Context) (*SignalInfo, error) {
	return &SignalInfo{RSSI: -70}, nil
}
func (f *contractLegacy) GetServingSystem(context.Context) (*ServingSystem, error) {
	return &ServingSystem{RegStatus: 1, Operator: "TestNet", NetworkMode: "LTE", RadioBand: "LTE BAND 3"}, nil
}
func (f *contractLegacy) IsSimInserted(context.Context) (bool, error) { return true, nil }
func (f *contractLegacy) GetNativeMCCMNC(context.Context) (string, string, error) {
	return "460", "00", nil
}
func (f *contractLegacy) GetNativeSPN(context.Context) (string, error)         { return "Test", nil }
func (f *contractLegacy) GetSIMMetadata(context.Context) (*SIMMetadata, error) { return nil, nil }
func (f *contractLegacy) SendSMS(context.Context, string, string) error        { return nil }
func (f *contractLegacy) ReadSMS(context.Context, NewSMSRef) (*SMS, error) {
	return &SMS{Index: 7, Sender: testfixtures.MSISDN, Content: "code 123456", Timestamp: time.Unix(1, 0)}, nil
}
func (f *contractLegacy) DeleteSMS(context.Context, NewSMSRef) error { return nil }
func (f *contractLegacy) ListSMS(context.Context) ([]SMSSummary, error) {
	return []SMSSummary{{Index: 7, Storage: "SM", ReceivedAt: time.Unix(2, 0), Sender: testfixtures.MSISDN, Body: "code 123456"}}, nil
}
func (f *contractLegacy) DeleteAllSMS(context.Context) error                    { return nil }
func (f *contractLegacy) SetOperatingMode(context.Context, OperatingMode) error { return nil }
func (f *contractLegacy) GetOperatingMode(context.Context) (OperatingMode, error) {
	return ModeOnline, nil
}
func (f *contractLegacy) Reboot(context.Context) error                            { return nil }
func (f *contractLegacy) OpenLogicalChannel(context.Context, string) (int, error) { return 1, nil }
func (f *contractLegacy) CloseLogicalChannel(context.Context, int) error          { return nil }
func (f *contractLegacy) TransmitAPDU(context.Context, int, string) (string, error) {
	return "9000", nil
}
func (f *contractLegacy) Events(_ context.Context) (<-chan BackendEvent, error) { return f.events, nil }

func TestBusinessAdapterExposesCommonContractAndLegacyPorts(t *testing.T) {
	events := make(chan BackendEvent, 1)
	legacy := &contractLegacy{events: events}
	adapter := Adapt(legacy)
	radio, err := adapter.Radio(context.Background())
	if err != nil || radio.RadioBand != "LTE BAND 3" {
		t.Fatalf("radio=%+v err=%v", radio, err)
	}

	identity, err := adapter.Identity(context.Background())
	if err != nil || identity.IMEI != "123456789012345" {
		t.Fatalf("identity=%+v err=%v", identity, err)
	}
	if !adapter.Capabilities(context.Background()).Has(device.CapabilitySIM) ||
		!adapter.Capabilities(context.Background()).Has(device.CapabilityAPDU) {
		t.Fatalf("capabilities=%v", adapter.Capabilities(context.Background()))
	}
	message, err := adapter.ReadSMS(context.Background(), NewSMSRef{Index: 7})
	if err != nil || message.Body != "code 123456" {
		t.Fatalf("message=%+v err=%v", message, err)
	}
	got, err := adapter.Events(context.Background())
	if err != nil || got != events {
		t.Fatalf("events channel was not forwarded: got=%v err=%v", got, err)
	}
}

func TestProtocolBackendsCanBeAdaptedWithoutRawAT(t *testing.T) {
	mbim := Adapt(NewMBIMBackend("/dev/cdc-wdm0", &fakeMBIMSource{}))
	if mbim.Mode() != BackendMBIM || mbim.Capabilities(context.Background()).Has(device.CapabilityRawAT) {
		t.Fatalf("MBIM adapter mode/capabilities=%s/%v", mbim.Mode(), mbim.Capabilities(context.Background()))
	}
	if _, err := mbim.RawAT(context.Background(), "AT"); err == nil {
		t.Fatal("MBIM raw AT unexpectedly succeeded")
	}

	qmiBackend, err := NewQMIBackend("/dev/cdc-wdm0", &qmiBackendSendSourceStub{})
	if err != nil {
		t.Fatal(err)
	}
	qmi := Adapt(qmiBackend)
	if qmi.Mode() != BackendQMI || qmi.Capabilities(context.Background()).Has(device.CapabilityRawAT) {
		t.Fatalf("QMI adapter mode/capabilities=%s/%v", qmi.Mode(), qmi.Capabilities(context.Background()))
	}
	if _, err := qmi.RawAT(context.Background(), "AT"); err == nil {
		t.Fatal("QMI raw AT unexpectedly succeeded")
	}
}
