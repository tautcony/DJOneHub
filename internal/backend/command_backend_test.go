package backend

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/apduarbiter"
	"github.com/iniwex5/vohive/internal/domain/device"
)

type fakeATTransport struct {
	responses map[string]string
	commands  []string
}

type fakeInteractiveATTransport struct {
	fakeATTransport
	promptCommands []string
	promptPayloads [][]byte
}

func (f *fakeInteractiveATTransport) CommandWithPrompt(command string, payload []byte, _ time.Duration) (string, error) {
	f.promptCommands = append(f.promptCommands, command)
	f.promptPayloads = append(f.promptPayloads, append([]byte(nil), payload...))
	return "+CMGS: 1\r\nOK\r\n", nil
}

func (f *fakeATTransport) Command(command string, _ time.Duration) (string, error) {
	f.commands = append(f.commands, command)
	return f.responses[command], nil
}
func (f *fakeATTransport) Close() error { return nil }

func TestCommandBackendReadOnlyStatus(t *testing.T) {
	transport := &fakeATTransport{responses: map[string]string{
		"AT+CGSN":                 "AT+CGSN\r\n" + fixtureIMEI + "\r\nOK\r\n",
		"AT+CIMI":                 "AT+CIMI\r\n" + fixtureIMSI + "\r\nOK\r\n",
		"AT+QCCID":                "AT+QCCID\r\n" + fixtureICCID19 + "\r\nOK\r\n",
		"AT+CNUM":                 "AT+CNUM\r\n+CNUM: \"\",\"" + fixtureMSISDN + "\",145\r\nOK\r\n",
		"AT+CGMR":                 "AT+CGMR\r\nsynthetic-firmware-1\r\nOK\r\n",
		"AT+CEREG?":               "AT+CEREG?\r\n+CEREG: 0,5\r\nOK\r\n",
		"AT+CREG?":                "AT+CREG?\r\n+CREG: 0,0\r\nOK\r\n",
		"AT+COPS?":                "AT+COPS?\r\n+COPS: 0,0,\"TestNet\",7\r\nOK\r\n",
		"AT+QNWINFO":              "AT+QNWINFO\r\n+QNWINFO: \"FDD LTE\",\"TestNet\",\"LTE BAND 3\",100\r\nOK\r\n",
		"AT+CSQ":                  "AT+CSQ\r\n+CSQ: 20,99\r\nOK\r\n",
		"AT+QENG=\"servingcell\"": "AT+QENG=\"servingcell\"\r\n+QENG: \"servingcell\",\"NOCONN\",\"LTE\",\"FDD\",460,01,8401A29,132,3740,8,3,3,-95,5992,-75,-8,-50,11,44\r\nOK\r\n",
		"AT+CPIN?":                "AT+CPIN?\r\n+CPIN: READY\r\nOK\r\n",
	}}
	backend := NewCommandBackend(transport, device.Identity{StableID: "synthetic-device-1"})

	identity, err := backend.Identity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if identity.IMEI != fixtureIMEI || identity.Firmware != "synthetic-firmware-1" {
		t.Fatalf("identity = %+v", identity)
	}
	radio, err := backend.Radio(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !radio.Registered || radio.Operator != "TestNet" || radio.RadioBand != "LTE BAND 3" || radio.SignalDBM != -73 || radio.SignalRSRP != -75 || radio.SignalRSRQ != -8 || radio.SignalSINR != 11 {
		t.Fatalf("radio = %+v", radio)
	}
	sim, err := backend.SIM(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !sim.Inserted || sim.IMSI != fixtureIMSI || sim.ICCID != fixtureICCID19 {
		t.Fatalf("sim = %+v", sim)
	}
	if !backend.Capabilities(context.Background()).Has(device.CapabilityDeviceStatus) || !backend.Capabilities(context.Background()).Has(device.CapabilityRawAT) {
		t.Fatalf("capabilities = %+v", backend.Capabilities(context.Background()))
	}
	if len(transport.commands) == 0 {
		t.Fatal("expected read-only AT queries")
	}
}

func TestCommandBackendIdentityUsesQGMRBeforeCGMR(t *testing.T) {
	transport := &fakeATTransport{responses: map[string]string{
		"AT+CGSN": "AT+CGSN\r\n" + fixtureIMEI + "\r\nOK\r\n",
		"AT+QGMR": "AT+QGMR\r\n+QGMR: QGMR-SYNTHETIC\r\nOK\r\n",
	}}
	backend := NewCommandBackend(transport, device.Identity{StableID: "synthetic-qgmr"})
	identity, err := backend.Identity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if identity.Firmware != "QGMR-SYNTHETIC" || identity.FirmwareSource != "AT+QGMR" || !identity.FirmwareLive {
		t.Fatalf("identity=%+v", identity)
	}
	for _, command := range transport.commands {
		if command == "AT+CGMR" {
			t.Fatal("CGMR was sent after a valid QGMR response")
		}
	}
}

func TestCommandBackendRadioFallsBackAcrossRegistrationDomains(t *testing.T) {
	transport := &fakeATTransport{responses: map[string]string{
		"AT+CEREG?": "+CEREG: 0,0\r\nOK\r\n",
		"AT+CGREG?": "+CGREG: 0,1\r\nOK\r\n",
		"AT+CREG?":  "+CREG: 0,0\r\nOK\r\n",
	}}
	backend := NewCommandBackend(transport, device.Identity{StableID: "synthetic-device-1"})

	radio, err := backend.Radio(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !radio.Registered {
		t.Fatalf("radio = %+v, want registered from CGREG", radio)
	}
}

func TestCommandBackendListsAndDecodesPDU(t *testing.T) {
	transport := &fakeATTransport{responses: map[string]string{
		"AT+CMGF?":               "+CMGF: 0\r\nOK\r\n",
		`AT+CPMS="SM","SM","SM"`: "+CPMS: 0,20,0,20,0,20\r\nOK\r\n",
		`AT+CPMS="ME","ME","ME"`: "+CPMS: 1,20,1,20,1,20\r\nOK\r\n",
		"AT+CMGL=4":              "+CMGL: 7\r\n00040491361900005150713220052308C8303A8C0EA3C3\r\nOK\r\n",
	}}
	backend := NewCommandBackend(transport, device.Identity{StableID: "synthetic-device-1"})
	items, err := backend.ListSMS(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Index != 7 || strings.TrimSpace(items[0].Body) == "" {
		t.Fatalf("items = %+v", items)
	}
	if backend.Capabilities(context.Background()).Has(device.CapabilitySMSSend) {
		t.Fatal("non-interactive transport must not advertise sms_send")
	}
}

func TestCommandBackendClearsOnlyMEStorage(t *testing.T) {
	transport := &fakeATTransport{responses: map[string]string{
		`AT+CPMS="ME","ME","ME"`: "OK\r\n",
		"AT+CMGD=1,4":            "OK\r\n",
	}}
	backend := NewCommandBackend(transport, device.Identity{StableID: "synthetic-device-1"})
	if err := backend.DeleteAllSMS(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(transport.commands, "\n"), `AT+CPMS="SM"`) {
		t.Fatalf("clear selected SM storage: %v", transport.commands)
	}
	if len(transport.commands) != 2 || transport.commands[0] != `AT+CPMS="ME","ME","ME"` || transport.commands[1] != "AT+CMGD=1,4" {
		t.Fatalf("commands = %v", transport.commands)
	}
}

func TestCommandBackendNetworkModeAndInteractiveSMSCapability(t *testing.T) {
	transport := &fakeInteractiveATTransport{fakeATTransport: fakeATTransport{responses: map[string]string{
		"AT+CMGF?":           "+CMGF: 0\r\nOK\r\n",
		`AT+QCFG="usbnet"?`:  `+QCFG: "usbnet",0\r\nOK\r\n`,
		"AT+CEREG?":          "+CEREG: 0,1\r\nOK\r\n",
		"AT+COPS?":           `+COPS: 0,0,"TestNet",7\r\nOK\r\n`,
		"AT+QNWINFO":         `+QNWINFO: "FDD LTE","TestNet","LTE BAND 3",100\r\nOK\r\n`,
		"AT+CSQ":             "+CSQ: 20,99\r\nOK\r\n",
		`AT+QCFG="usbnet",0`: "OK\r\n",
		"AT+CFUN=1,1":        "OK\r\n",
	}}}
	backend := NewCommandBackend(transport, device.Identity{StableID: "synthetic-device-1"})
	status, err := backend.Status(context.Background())
	if err != nil || status["mode"] != "0" || status["radio_band"] != "LTE BAND 3" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	if !backend.Capabilities(context.Background()).Has(device.CapabilitySMSSend) || !backend.Capabilities(context.Background()).Has(device.CapabilityNetworkControl) {
		t.Fatalf("capabilities = %+v", backend.Capabilities(context.Background()))
	}
	if err := backend.SetMode(context.Background(), "0"); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	if len(transport.promptCommands) != 0 {
		t.Fatal("mode query must not use interactive transport")
	}
}

func TestCommandBackendSIMAuth(t *testing.T) {
	transport := &fakeATTransport{responses: map[string]string{
		`AT+CCHO="A0000000871002"`: "+CCHO: 2\r\nOK\r\n",
		`AT+CGLA=2,8,"00A40000"`:   "+CGLA: 8,\"9000\"\r\nOK\r\n",
		"AT+CCHC=2":                "OK\r\n",
	}}
	backend := NewCommandBackend(transport, device.Identity{StableID: "synthetic-sim-auth"})
	channel, err := backend.OpenLogicalChannel(context.Background(), "a0000000871002")
	if err != nil || channel != 2 {
		t.Fatalf("OpenLogicalChannel() = %d, %v", channel, err)
	}
	response, err := backend.TransmitAPDU(context.Background(), channel, "00a40000")
	if err != nil || response != "9000" {
		t.Fatalf("TransmitAPDU() = %q, %v", response, err)
	}
	if err := backend.CloseLogicalChannel(context.Background(), channel); err != nil {
		t.Fatalf("CloseLogicalChannel(): %v", err)
	}
	if !backend.Capabilities(context.Background()).Has(device.CapabilityAPDU) {
		t.Fatalf("capabilities = %+v", backend.Capabilities(context.Background()))
	}
	want := []string{`AT+CCHO="A0000000871002"`, `AT+CGLA=2,8,"00A40000"`, "AT+CCHC=2"}
	if strings.Join(transport.commands, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands = %v, want %v", transport.commands, want)
	}
}

func TestCommandBackendSIMAuthRejectsInvalidResponses(t *testing.T) {
	transport := &fakeATTransport{responses: map[string]string{
		`AT+CCHO="A0000000871002"`: "+CCHO: 0\r\nOK\r\n",
	}}
	backend := NewCommandBackend(transport, device.Identity{StableID: "synthetic-sim-auth-invalid"})
	if _, err := backend.OpenLogicalChannel(context.Background(), "A0000000871002"); err == nil {
		t.Fatal("OpenLogicalChannel() error = nil")
	}
	if _, err := backend.TransmitAPDU(context.Background(), 1, "GG"); err == nil {
		t.Fatal("TransmitAPDU() error = nil for invalid hex")
	}
}

func TestCommandBackendSIMAuthUsesArbiter(t *testing.T) {
	transport := &fakeATTransport{responses: map[string]string{
		`AT+CCHO="A0000000871002"`: "+CCHO: 2\r\nOK\r\n",
	}}
	backend := NewCommandBackend(transport, device.Identity{StableID: "synthetic-arbiter"})
	arbiter := apduarbiter.New("synthetic-arbiter", apduarbiter.Options{})
	backend.SetAPDUArbiter(arbiter)
	if _, err := backend.OpenLogicalChannel(context.Background(), "A0000000871002"); err != nil {
		t.Fatal(err)
	}
	stats := arbiter.Stats()
	if stats.Acquires != 1 || stats.ActiveTransport {
		t.Fatalf("arbiter stats = %+v", stats)
	}
}

func TestCommandBackendVoWiFiNarrowBackendMethods(t *testing.T) {
	transport := &fakeATTransport{responses: map[string]string{
		"AT+CGSN":                 "356789012345678\r\nOK\r\n",
		"AT+CIMI":                 "310280233641503\r\nOK\r\n",
		"AT+QCCID":                "8901000000000000000\r\nOK\r\n",
		"AT+CPIN?":                "+CPIN: READY\r\nOK\r\n",
		"AT+CRSM=176,28589,0,0,4": "+CRSM: 144,0,\"00000003\"\r\nOK\r\n",
		"AT+CFUN?":                "+CFUN: 4\r\nOK\r\n",
		"AT+CFUN=1":               "OK\r\n",
		"AT+CEREG?":               "+CEREG: 0,1\r\nOK\r\n",
		"AT+COPS?":                "+COPS: 0,0,\"TestNet\",7\r\nOK\r\n",
		"AT+QNWINFO":              "+QNWINFO: \"FDD LTE\",\"TestNet\",\"LTE BAND 3\",100\r\nOK\r\n",
		"AT+CSQ":                  "+CSQ: 20,99\r\nOK\r\n",
		`AT+QENG="servingcell"`:   "ERROR\r\n",
	}}
	backend := NewCommandBackend(transport, device.Identity{StableID: "synthetic-vowifi"})
	if imsi, err := backend.GetIMSI(context.Background()); err != nil || imsi != "310280233641503" {
		t.Fatalf("GetIMSI() = %q, %v", imsi, err)
	}
	if mcc, mnc, err := backend.GetNativeMCCMNC(context.Background()); err != nil || mcc != "310" || mnc != "280" {
		t.Fatalf("GetNativeMCCMNC() = %q, %q, %v", mcc, mnc, err)
	}
	if mode, err := backend.GetOperatingMode(context.Background()); err != nil || mode != ModeRFOff {
		t.Fatalf("GetOperatingMode() = %v, %v", mode, err)
	}
	if err := backend.SetOperatingMode(context.Background(), ModeOnline); err != nil {
		t.Fatalf("SetOperatingMode(): %v", err)
	}
}
