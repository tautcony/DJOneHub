package darwin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"
)

var diagEDLFrame = []byte{0x4b, 0x65, 0x01, 0x00, 0x54, 0x0f, 0x7e}

var (
	errDIAGEchoMismatch = errors.New("DIAG EDL acknowledgement did not match the request")
	errDIAGTimeout      = errors.New("DIAG EDL acknowledgement timed out")
)

type diagEndpoint interface {
	Write(context.Context, []byte, time.Duration) error
	Read(context.Context, []byte, time.Duration) (int, error)
}

func runDIAGSwitch(ctx context.Context, endpoint diagEndpoint, timeout time.Duration) error {
	if endpoint == nil {
		return errors.New("DIAG endpoint is unavailable")
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	deadline := time.Now().Add(timeout)
	// Drain stale bytes. A short read timeout is intentional and is not a
	// protocol failure; it marks the input queue as empty before the frame.
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining > 80*time.Millisecond {
			remaining = 80 * time.Millisecond
		}
		var scratch [512]byte
		_, err := endpoint.Read(ctx, scratch[:], remaining)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if err != nil {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := endpoint.Write(ctx, diagEDLFrame, time.Until(deadline)); err != nil {
		return fmt.Errorf("write DIAG EDL frame: %w", err)
	}
	ack := make([]byte, 0, 64)
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		var chunk [64]byte
		n, err := endpoint.Read(ctx, chunk[:], remaining)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if err != nil {
			if time.Now().After(deadline) {
				return errDIAGTimeout
			}
			return fmt.Errorf("read DIAG EDL acknowledgement: %w", err)
		}
		if n > 0 {
			ack = append(ack, chunk[:n]...)
			if ack[len(ack)-1] == 0x7e {
				break
			}
			if len(ack) > 512 {
				return fmt.Errorf("%w: response exceeds 512 bytes", errDIAGEchoMismatch)
			}
		}
	}
	if len(ack) == 0 || ack[len(ack)-1] != 0x7e {
		return errDIAGTimeout
	}
	if bytes.Equal(ack, diagEDLFrame) {
		return nil
	}
	payload, err := decodeDIAGFrame(ack)
	if err != nil {
		return fmt.Errorf("%w: %v", errDIAGEchoMismatch, err)
	}
	if !bytes.Equal(payload, diagEDLFrame) {
		return fmt.Errorf("%w: decoded payload length %d", errDIAGEchoMismatch, len(payload))
	}
	return nil
}

func decodeDIAGFrame(frame []byte) ([]byte, error) {
	if len(frame) < 4 || frame[len(frame)-1] != 0x7e {
		return nil, errors.New("DIAG response has no frame terminator")
	}
	encoded := frame[:len(frame)-1]
	if len(encoded) > 0 && encoded[0] == 0x7e {
		encoded = encoded[1:]
	}
	decoded := make([]byte, 0, len(encoded))
	for index := 0; index < len(encoded); index++ {
		value := encoded[index]
		if value != 0x7d {
			decoded = append(decoded, value)
			continue
		}
		index++
		if index >= len(encoded) {
			return nil, errors.New("DIAG response ends with an incomplete escape")
		}
		switch encoded[index] {
		case 0x5e:
			decoded = append(decoded, 0x7e)
		case 0x5d:
			decoded = append(decoded, 0x7d)
		default:
			return nil, fmt.Errorf("DIAG response contains invalid escape 0x%02x", encoded[index])
		}
	}
	if len(decoded) < 3 {
		return nil, errors.New("DIAG response is shorter than payload and CRC")
	}
	payload := decoded[:len(decoded)-2]
	receivedCRC := uint16(decoded[len(decoded)-2]) | uint16(decoded[len(decoded)-1])<<8
	if calculated := diagCRC16(payload); calculated != receivedCRC {
		return nil, fmt.Errorf("DIAG response CRC mismatch: got 0x%04x", receivedCRC)
	}
	return payload, nil
}

func diagCRC16(payload []byte) uint16 {
	crc := uint16(0xffff)
	for _, value := range payload {
		crc ^= uint16(value)
		for range 8 {
			if crc&1 != 0 {
				crc = crc>>1 ^ 0x8408
			} else {
				crc >>= 1
			}
		}
	}
	return ^crc
}
