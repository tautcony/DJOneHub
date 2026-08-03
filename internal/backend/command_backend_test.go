package backend

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/testfixtures"
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
		"AT+CGSN":                 "AT+CGSN\r\n" + testfixtures.IMEI + "\r\nOK\r\n",
		"AT+CIMI":                 "AT+CIMI\r\n" + testfixtures.IMSI + "\r\nOK\r\n",
		"AT+QCCID":                "AT+QCCID\r\n" + testfixtures.ICCID19 + "\r\nOK\r\n",
		"AT+CNUM":                 "AT+CNUM\r\n+CNUM: \"\",\"" + testfixtures.MSISDN + "\",145\r\nOK\r\n",
		"AT+CGMR":                 "AT+CGMR\r\nsynthetic-firmware-1\r\nOK\r\n",
		"AT+CREG?":                "AT+CREG?\r\n+CREG: 0,1\r\nOK\r\n",
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
	if identity.IMEI != testfixtures.IMEI || identity.Firmware != "synthetic-firmware-1" {
		t.Fatalf("identity = %+v", identity)
	}
	radio, err := backend.Radio(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !radio.Registered || radio.Operator != "TestNet" || radio.SignalDBM != -73 || radio.SignalRSRP != -75 || radio.SignalRSRQ != -8 || radio.SignalSINR != 11 {
		t.Fatalf("radio = %+v", radio)
	}
	sim, err := backend.SIM(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !sim.Inserted || sim.IMSI != testfixtures.IMSI || sim.ICCID != testfixtures.ICCID19 {
		t.Fatalf("sim = %+v", sim)
	}
	if !backend.Capabilities(context.Background()).Has(device.CapabilityDeviceStatus) || !backend.Capabilities(context.Background()).Has(device.CapabilityRawAT) {
		t.Fatalf("capabilities = %+v", backend.Capabilities(context.Background()))
	}
	if len(transport.commands) == 0 {
		t.Fatal("expected read-only AT queries")
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
		"AT+CREG?":           "+CREG: 0,1\r\nOK\r\n",
		"AT+COPS?":           `+COPS: 0,0,"TestNet",7\r\nOK\r\n`,
		"AT+QNWINFO":         `+QNWINFO: "FDD LTE","TestNet","LTE BAND 3",100\r\nOK\r\n`,
		"AT+CSQ":             "+CSQ: 20,99\r\nOK\r\n",
		`AT+QCFG="usbnet",0`: "OK\r\n",
		"AT+CFUN=1,1":        "OK\r\n",
	}}}
	backend := NewCommandBackend(transport, device.Identity{StableID: "synthetic-device-1"})
	status, err := backend.Status(context.Background())
	if err != nil || status["mode"] != "0" {
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
