package darwin

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/iniwex5/vohive/internal/domain/device"
)

const (
	saharaHelloReq      uint32 = 0x1
	saharaHelloResp     uint32 = 0x2
	saharaCmdReady      uint32 = 0xb
	saharaSwitchMode    uint32 = 0xc
	saharaExecute       uint32 = 0xd
	saharaExecuteResp   uint32 = 0xe
	saharaExecuteData   uint32 = 0xf
	saharaModeCommand   uint32 = 0x3
	saharaMaxPacketSize        = 4096
	saharaMaxValueSize         = 1024
	saharaCmdGetSerial  uint32 = 0x1
	saharaCmdGetMSMHWID uint32 = 0x2
	saharaCmdGetPKHash  uint32 = 0x3
	saharaCmdGetSBL     uint32 = 0x7
)

var errSaharaProtocol = errors.New("Sahara protocol error")

type saharaEndpoint interface {
	WritePacket([]byte) error
	ReadPacket() ([]byte, error)
	ReadData(int) ([]byte, error)
}

type saharaHello struct {
	Version   uint32
	Min       uint32
	Mode      uint32
	MaxCmdLen uint32
}

func parseSaharaHello(packet []byte) (saharaHello, error) {
	if len(packet) < 48 || len(packet) > saharaMaxPacketSize {
		return saharaHello{}, fmt.Errorf("%w: hello request length %d", errSaharaProtocol, len(packet))
	}
	if binary.LittleEndian.Uint32(packet[0:4]) != saharaHelloReq {
		return saharaHello{}, fmt.Errorf("%w: expected hello request", errSaharaProtocol)
	}
	declared := binary.LittleEndian.Uint32(packet[4:8])
	if declared != 48 || declared > uint32(len(packet)) {
		return saharaHello{}, fmt.Errorf("%w: invalid hello packet length %d", errSaharaProtocol, declared)
	}
	return saharaHello{
		Version:   binary.LittleEndian.Uint32(packet[8:12]),
		Min:       binary.LittleEndian.Uint32(packet[12:16]),
		MaxCmdLen: binary.LittleEndian.Uint32(packet[16:20]),
		Mode:      binary.LittleEndian.Uint32(packet[20:24]),
	}, nil
}

func encodeSaharaHelloResponse(hello saharaHello) []byte {
	packet := make([]byte, 48)
	binary.LittleEndian.PutUint32(packet[0:4], saharaHelloResp)
	binary.LittleEndian.PutUint32(packet[4:8], uint32(len(packet)))
	binary.LittleEndian.PutUint32(packet[8:12], hello.Version)
	binary.LittleEndian.PutUint32(packet[12:16], hello.Min)
	// Sahara HELLO response offset 16 is the status field, not the request's
	// max command length. A non-zero value makes Qualcomm EDL reject command
	// mode and never emit CMD_READY.
	binary.LittleEndian.PutUint32(packet[16:20], 0)
	binary.LittleEndian.PutUint32(packet[20:24], saharaModeCommand)
	return packet
}

func encodeSaharaCommand(commandID, value uint32) []byte {
	packet := make([]byte, 12)
	binary.LittleEndian.PutUint32(packet[0:4], commandID)
	binary.LittleEndian.PutUint32(packet[4:8], uint32(len(packet)))
	binary.LittleEndian.PutUint32(packet[8:12], value)
	return packet
}

func parseSaharaExecuteLength(packet []byte, command uint32) (int, error) {
	if len(packet) != 16 {
		return 0, fmt.Errorf("%w: execute response length %d", errSaharaProtocol, len(packet))
	}
	if binary.LittleEndian.Uint32(packet[0:4]) != saharaExecuteResp || binary.LittleEndian.Uint32(packet[4:8]) != 16 {
		return 0, fmt.Errorf("%w: invalid execute response header", errSaharaProtocol)
	}
	if binary.LittleEndian.Uint32(packet[8:12]) != command {
		return 0, fmt.Errorf("%w: execute command mismatch", errSaharaProtocol)
	}
	length := binary.LittleEndian.Uint32(packet[12:16])
	if length == 0 || length > saharaMaxValueSize {
		return 0, fmt.Errorf("%w: execute value length %d", errSaharaProtocol, length)
	}
	return int(length), nil
}

func parseSaharaValue(command uint32, value []byte) (device.EDLObservation, error) {
	if len(value) == 0 || len(value) > saharaMaxValueSize {
		return device.EDLObservation{}, fmt.Errorf("%w: execute value length %d", errSaharaProtocol, len(value))
	}
	observation := device.EDLObservation{Protocol: "sahara", Source: "usb", State: device.EDLStateSaharaIdentified}
	switch command {
	case saharaCmdGetSerial:
		if len(value) != 4 {
			return device.EDLObservation{}, fmt.Errorf("%w: serial value length %d", errSaharaProtocol, len(value))
		}
		observation.SerialNumber = fmt.Sprintf("%08x", binary.LittleEndian.Uint32(value))
	case saharaCmdGetMSMHWID:
		if len(value) < 8 {
			return device.EDLObservation{}, fmt.Errorf("%w: HWID value length %d", errSaharaProtocol, len(value))
		}
		observation.HardwareID = fmt.Sprintf("%016x", binary.LittleEndian.Uint64(value[:8]))
	case saharaCmdGetPKHash:
		observation.PKHash = formatSaharaHex(value)
	case saharaCmdGetSBL:
		if len(value) != 4 {
			return device.EDLObservation{}, fmt.Errorf("%w: SBL version length %d", errSaharaProtocol, len(value))
		}
		observation.SBLVersion = fmt.Sprintf("%08x", binary.LittleEndian.Uint32(value))
	default:
		return device.EDLObservation{}, fmt.Errorf("%w: unsupported execute command 0x%x", errSaharaProtocol, command)
	}
	return observation, nil
}

func formatSaharaHex(value []byte) string {
	value = bytes.TrimRight(value, "\x00")
	if len(value) == 0 {
		return ""
	}
	const digits = "0123456789abcdef"
	out := make([]byte, len(value)*2)
	for index, item := range value {
		out[index*2] = digits[item>>4]
		out[index*2+1] = digits[item&0xf]
	}
	return string(out)
}

func observeSahara(endpoint saharaEndpoint) (device.EDLObservation, error) {
	if endpoint == nil {
		return device.EDLObservation{}, fmt.Errorf("%w: endpoint unavailable", errSaharaProtocol)
	}
	helloPacket, err := endpoint.ReadPacket()
	if err != nil {
		return recoveryObservation("Sahara hello failed"), err
	}
	if bytes.HasPrefix(bytes.TrimSpace(helloPacket), []byte("<?xml")) {
		return device.EDLObservation{State: device.EDLStateFirehoseReady, Protocol: "firehose", Source: "usb"}, nil
	}
	if !isSaharaCommandReady(helloPacket) {
		hello, parseErr := parseSaharaHello(helloPacket)
		if parseErr != nil {
			return recoveryObservation("invalid Sahara hello request"), parseErr
		}
		if err := endpoint.WritePacket(encodeSaharaHelloResponse(hello)); err != nil {
			return recoveryObservation("Sahara command-mode request failed"), err
		}
		ready, readErr := endpoint.ReadPacket()
		if readErr != nil {
			return recoveryObservation("Sahara command mode did not become ready"), readErr
		}
		if !isSaharaCommandReady(ready) {
			return recoveryObservation("invalid Sahara command-ready response"), fmt.Errorf("%w: command-ready response is invalid", errSaharaProtocol)
		}
	}

	observation := device.EDLObservation{State: device.EDLStateSaharaIdentified, Protocol: "sahara", Source: "usb"}
	for _, command := range []uint32{saharaCmdGetSerial, saharaCmdGetMSMHWID, saharaCmdGetPKHash, saharaCmdGetSBL} {
		if err := endpoint.WritePacket(encodeSaharaCommand(saharaExecute, command)); err != nil {
			return failedSaharaObservation(observation, "Sahara execute request failed"), err
		}
		response, err := endpoint.ReadPacket()
		if err != nil {
			return failedSaharaObservation(observation, "Sahara execute response failed"), err
		}
		length, err := parseSaharaExecuteLength(response, command)
		if err != nil {
			return failedSaharaObservation(observation, "invalid Sahara execute response"), err
		}
		if err := endpoint.WritePacket(encodeSaharaCommand(saharaExecuteData, command)); err != nil {
			return failedSaharaObservation(observation, "Sahara execute-data request failed"), err
		}
		value, err := endpoint.ReadData(length)
		if err != nil {
			return failedSaharaObservation(observation, "Sahara execute-data read failed"), err
		}
		fact, err := parseSaharaValue(command, value)
		if err != nil {
			return failedSaharaObservation(observation, "invalid Sahara execute-data value"), err
		}
		mergeSaharaFact(&observation, fact)
	}
	if err := endpoint.WritePacket(encodeSaharaCommand(saharaSwitchMode, saharaModeCommand)); err != nil {
		return failedSaharaObservation(observation, "Sahara mode switch failed"), err
	}
	return observation, nil
}

func isSaharaCommandReady(packet []byte) bool {
	return len(packet) == 8 && binary.LittleEndian.Uint32(packet[0:4]) == saharaCmdReady && binary.LittleEndian.Uint32(packet[4:8]) == 8
}

func failedSaharaObservation(observation device.EDLObservation, reason string) device.EDLObservation {
	observation.State = device.EDLStateRecoveryRequired
	observation.RecoveryNeeded = true
	observation.Reason = reason
	return observation
}

func recoveryObservation(reason string) device.EDLObservation {
	return device.EDLObservation{State: device.EDLStateRecoveryRequired, Protocol: "sahara", Source: "usb", RecoveryNeeded: true, Reason: reason}
}

func mergeSaharaFact(observation *device.EDLObservation, fact device.EDLObservation) {
	if fact.SerialNumber != "" {
		observation.SerialNumber = fact.SerialNumber
	}
	if fact.HardwareID != "" {
		observation.HardwareID = fact.HardwareID
	}
	if fact.PKHash != "" {
		observation.PKHash = fact.PKHash
	}
	if fact.SBLVersion != "" {
		observation.SBLVersion = fact.SBLVersion
	}
}
