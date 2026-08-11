package darwin

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/iniwex5/vohive/internal/domain/device"
)

type fakeSaharaEndpoint struct {
	writes  [][]byte
	reads   [][]byte
	data    [][]byte
	readErr error
}

func (f *fakeSaharaEndpoint) WritePacket(packet []byte) error {
	f.writes = append(f.writes, append([]byte(nil), packet...))
	return nil
}
func (f *fakeSaharaEndpoint) ReadPacket() ([]byte, error) {
	if len(f.reads) == 0 {
		if f.readErr != nil {
			return nil, f.readErr
		}
		return nil, errors.New("no packet")
	}
	packet := f.reads[0]
	f.reads = f.reads[1:]
	return packet, nil
}

func TestObserveSaharaReportsDisconnectAsRecoveryRequired(t *testing.T) {
	disconnected := errors.New("USB disconnected")
	observation, err := observeSahara(&fakeSaharaEndpoint{readErr: disconnected})
	if !errors.Is(err, disconnected) || observation.State != device.EDLStateRecoveryRequired || observation.Protocol != "sahara" || observation.Source != "usb" || !observation.RecoveryNeeded {
		t.Fatalf("observation=%+v err=%v", observation, err)
	}
}

func TestObserveSaharaReportsMalformedCommandReady(t *testing.T) {
	endpoint := &fakeSaharaEndpoint{reads: [][]byte{saharaHelloRequest(0), {1, 2, 3}}}
	observation, err := observeSahara(endpoint)
	if !errors.Is(err, errSaharaProtocol) || observation.State != device.EDLStateRecoveryRequired {
		t.Fatalf("observation=%+v err=%v", observation, err)
	}
}
func (f *fakeSaharaEndpoint) ReadData(length int) ([]byte, error) {
	if len(f.data) == 0 || len(f.data[0]) != length {
		return nil, errors.New("data length mismatch")
	}
	value := append([]byte(nil), f.data[0]...)
	f.data = f.data[1:]
	return value, nil
}

func saharaHelloRequest(mode uint32) []byte {
	packet := make([]byte, 48)
	binary.LittleEndian.PutUint32(packet[0:4], saharaHelloReq)
	binary.LittleEndian.PutUint32(packet[4:8], 48)
	binary.LittleEndian.PutUint32(packet[8:12], 2)
	binary.LittleEndian.PutUint32(packet[12:16], 1)
	binary.LittleEndian.PutUint32(packet[16:20], 0x1000)
	binary.LittleEndian.PutUint32(packet[20:24], mode)
	return packet
}

func saharaReady() []byte {
	packet := make([]byte, 8)
	binary.LittleEndian.PutUint32(packet[0:4], saharaCmdReady)
	binary.LittleEndian.PutUint32(packet[4:8], 8)
	return packet
}

func saharaExecuteResponse(command uint32, length int) []byte {
	packet := make([]byte, 16)
	binary.LittleEndian.PutUint32(packet[0:4], saharaExecuteResp)
	binary.LittleEndian.PutUint32(packet[4:8], 16)
	binary.LittleEndian.PutUint32(packet[8:12], command)
	binary.LittleEndian.PutUint32(packet[12:16], uint32(length))
	return packet
}

func TestObserveSaharaReadsBoundedFacts(t *testing.T) {
	serial := make([]byte, 4)
	binary.LittleEndian.PutUint32(serial, 0x12345678)
	sbl := make([]byte, 4)
	binary.LittleEndian.PutUint32(sbl, 1)
	endpoint := &fakeSaharaEndpoint{
		reads: [][]byte{
			saharaHelloRequest(0), saharaReady(),
			saharaExecuteResponse(saharaCmdGetSerial, len(serial)),
			saharaExecuteResponse(saharaCmdGetMSMHWID, 8),
			saharaExecuteResponse(saharaCmdGetPKHash, 4),
			saharaExecuteResponse(saharaCmdGetSBL, len(sbl)),
		},
		data: [][]byte{serial, {1, 2, 3, 4, 5, 6, 7, 8}, {0xaa, 0xbb, 0xcc, 0xdd}, sbl},
	}
	observation, err := observeSahara(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if observation.State != device.EDLStateSaharaIdentified || observation.Protocol != "sahara" || observation.Source != "usb" || observation.SerialNumber != "12345678" || observation.HardwareID != "0807060504030201" || observation.SBLVersion != "00000001" {
		t.Fatalf("observation = %+v", observation)
	}
	if len(endpoint.writes) != 10 {
		t.Fatalf("writes=%d, want command-mode hello, four execute/data pairs, and mode switch", len(endpoint.writes))
	}
	response := endpoint.writes[0]
	if binary.LittleEndian.Uint32(response[16:20]) != 0 || binary.LittleEndian.Uint32(response[20:24]) != saharaModeCommand {
		t.Fatalf("HELLO response status/mode = %#x/%#x", binary.LittleEndian.Uint32(response[16:20]), binary.LittleEndian.Uint32(response[20:24]))
	}
}

func TestObserveSaharaStartsFromCommandReady(t *testing.T) {
	serial := make([]byte, 4)
	binary.LittleEndian.PutUint32(serial, 0x12345678)
	sbl := make([]byte, 4)
	endpoint := &fakeSaharaEndpoint{
		reads: [][]byte{
			saharaReady(),
			saharaExecuteResponse(saharaCmdGetSerial, len(serial)),
			saharaExecuteResponse(saharaCmdGetMSMHWID, 8),
			saharaExecuteResponse(saharaCmdGetPKHash, 4),
			saharaExecuteResponse(saharaCmdGetSBL, len(sbl)),
		},
		data: [][]byte{serial, {1, 2, 3, 4, 5, 6, 7, 8}, {0xaa, 0xbb, 0xcc, 0xdd}, sbl},
	}
	observation, err := observeSahara(endpoint)
	if err != nil || observation.State != device.EDLStateSaharaIdentified {
		t.Fatalf("observation=%+v err=%v", observation, err)
	}
	if len(endpoint.writes) != 9 {
		t.Fatalf("writes=%d, want four execute/data pairs and mode switch", len(endpoint.writes))
	}
}

func TestObserveSaharaRecognizesFirehose(t *testing.T) {
	endpoint := &fakeSaharaEndpoint{reads: [][]byte{[]byte(`<?xml version="1.0"?><data><nop /></data>`)}}
	observation, err := observeSahara(endpoint)
	if err != nil || observation.State != device.EDLStateFirehoseReady {
		t.Fatalf("observation=%+v err=%v", observation, err)
	}
}

func TestParseSaharaRejectsOversizedPacket(t *testing.T) {
	packet := make([]byte, saharaMaxPacketSize+1)
	if _, err := parseSaharaHello(packet); !errors.Is(err, errSaharaProtocol) {
		t.Fatalf("parseSaharaHello() error = %v", err)
	}
}

func TestParseSaharaRejectsOversizedExecuteValue(t *testing.T) {
	packet := saharaExecuteResponse(saharaCmdGetSerial, saharaMaxValueSize+1)
	if _, err := parseSaharaExecuteLength(packet, saharaCmdGetSerial); !errors.Is(err, errSaharaProtocol) {
		t.Fatalf("parseSaharaExecuteLength() error = %v", err)
	}
}

func TestParseSaharaDoesNotCallUnknownCommandFirmware(t *testing.T) {
	if _, err := parseSaharaValue(0xff, []byte{1}); err == nil {
		t.Fatal("parseSaharaValue accepted unknown command")
	}
}
