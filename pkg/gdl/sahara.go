package gdl

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"
)

const (
	saharaHello      = 0x01
	saharaHelloReply = 0x02
	saharaReadData   = 0x03
	saharaEndImage   = 0x04
	saharaDone       = 0x05
	saharaDoneReply  = 0x06
	saharaReadData64 = 0x12
	saharaResetState = 0x13
	saharaPacketMax  = 1 << 20
	saharaIOTimeout  = 5 * time.Second
)

// ResetSaharaState requests recovery from a Sahara protocol error. The caller
// owns the transport and must reconnect before starting a new session.
func ResetSaharaState(ctx context.Context, transport Transport) error {
	if transport == nil {
		return errors.New("gdl transport is required")
	}
	packet := make([]byte, 8)
	binary.LittleEndian.PutUint32(packet[0:4], saharaResetState)
	binary.LittleEndian.PutUint32(packet[4:8], uint32(len(packet)))
	if err := transport.Write(ctx, packet, saharaIOTimeout); err != nil {
		return fmt.Errorf("reset Sahara state machine: %w", err)
	}
	return nil
}

func (s *Session) connect(ctx context.Context, loader io.ReaderAt, loaderSize int64) error {
	first, err := s.readInitial(ctx)
	if err != nil {
		if errors.Is(err, ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
			s.log("No initial Sahara packet; probing an active Firehose loader\n")
			return s.attachFirehose(ctx)
		}
		return err
	}
	s.pending = append(s.pending, first...)
	if looksLikeXML(s.pending) {
		s.log("Firehose loader is already active\n")
		return s.initializeFirehose(ctx, false)
	}
	if loader == nil || loaderSize <= 0 {
		return ErrLoaderRequired
	}
	if err := s.uploadLoader(ctx, loader, loaderSize); err != nil {
		return err
	}
	return s.initializeFirehose(ctx, true)
}

func (s *Session) readInitial(ctx context.Context) ([]byte, error) {
	buf := make([]byte, 64<<10)
	n, err := s.transport.Read(ctx, buf, saharaIOTimeout)
	if err != nil {
		return nil, fmt.Errorf("read initial EDL packet: %w", err)
	}
	if n <= 0 || n > len(buf) {
		return nil, fmt.Errorf("%w: invalid initial packet length %d", ErrProtocol, n)
	}
	return buf[:n], nil
}

func (s *Session) uploadLoader(ctx context.Context, loader io.ReaderAt, loaderSize int64) error {
	for {
		packet, err := s.readSaharaPacket(ctx)
		if err != nil {
			return err
		}
		command := binary.LittleEndian.Uint32(packet[0:4])
		switch command {
		case saharaHello:
			if len(packet) != 0x30 {
				return fmt.Errorf("%w: Sahara hello length %d", ErrProtocol, len(packet))
			}
			response := make([]byte, 0x30)
			binary.LittleEndian.PutUint32(response[0:4], saharaHelloReply)
			binary.LittleEndian.PutUint32(response[4:8], uint32(len(response)))
			binary.LittleEndian.PutUint32(response[8:12], 2)
			binary.LittleEndian.PutUint32(response[12:16], 1)
			binary.LittleEndian.PutUint32(response[20:24], binary.LittleEndian.Uint32(packet[20:24]))
			if err := s.transport.Write(ctx, response, saharaIOTimeout); err != nil {
				return fmt.Errorf("write Sahara hello response: %w", err)
			}
		case saharaReadData:
			if len(packet) != 0x14 {
				return fmt.Errorf("%w: Sahara read length %d", ErrProtocol, len(packet))
			}
			offset := uint64(binary.LittleEndian.Uint32(packet[12:16]))
			length := uint64(binary.LittleEndian.Uint32(packet[16:20]))
			if err := s.sendLoaderRange(ctx, loader, loaderSize, offset, length); err != nil {
				return err
			}
		case saharaReadData64:
			if len(packet) != 0x20 {
				return fmt.Errorf("%w: Sahara read64 length %d", ErrProtocol, len(packet))
			}
			offset := binary.LittleEndian.Uint64(packet[16:24])
			length := binary.LittleEndian.Uint64(packet[24:32])
			if err := s.sendLoaderRange(ctx, loader, loaderSize, offset, length); err != nil {
				return err
			}
		case saharaEndImage:
			if len(packet) != 0x10 {
				return fmt.Errorf("%w: Sahara end-image length %d", ErrProtocol, len(packet))
			}
			if status := binary.LittleEndian.Uint32(packet[12:16]); status != 0 {
				return fmt.Errorf("%w: loader upload status %d", ErrProtocol, status)
			}
			done := make([]byte, 8)
			binary.LittleEndian.PutUint32(done[0:4], saharaDone)
			binary.LittleEndian.PutUint32(done[4:8], uint32(len(done)))
			if err := s.transport.Write(ctx, done, saharaIOTimeout); err != nil {
				return fmt.Errorf("write Sahara done: %w", err)
			}
		case saharaDoneReply:
			if len(packet) != 0x0c {
				return fmt.Errorf("%w: Sahara done response length %d", ErrProtocol, len(packet))
			}
			if status := binary.LittleEndian.Uint32(packet[8:12]); status != 0 {
				return fmt.Errorf("%w: Sahara done status %d", ErrProtocol, status)
			}
			s.log("Firehose loader uploaded\n")
			return nil
		default:
			return fmt.Errorf("%w: unsupported Sahara command 0x%x", ErrUnsupportedMode, command)
		}
	}
}

func (s *Session) readSaharaPacket(ctx context.Context) ([]byte, error) {
	if err := s.fillPending(ctx, 8, saharaIOTimeout, saharaPacketMax); err != nil {
		return nil, err
	}
	length := uint64(binary.LittleEndian.Uint32(s.pending[4:8]))
	if length < 8 || length > saharaPacketMax {
		return nil, fmt.Errorf("%w: Sahara packet length %d", ErrProtocol, length)
	}
	if err := s.fillPending(ctx, int(length), saharaIOTimeout, saharaPacketMax); err != nil {
		return nil, err
	}
	packet := append([]byte(nil), s.pending[:length]...)
	s.pending = s.pending[length:]
	return packet, nil
}

func (s *Session) sendLoaderRange(ctx context.Context, loader io.ReaderAt, loaderSize int64, offset, length uint64) error {
	if length == 0 || length > maxLoaderChunk || offset > uint64(loaderSize) || length > uint64(loaderSize)-offset {
		return fmt.Errorf("%w: loader range offset=%d length=%d size=%d", ErrProtocol, offset, length, loaderSize)
	}
	buffer := make([]byte, 64<<10)
	remaining := length
	current := offset
	for remaining > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		chunk := uint64(len(buffer))
		if remaining < chunk {
			chunk = remaining
		}
		n, err := loader.ReadAt(buffer[:chunk], int64(current))
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("read Firehose loader: %w", err)
		}
		if n != int(chunk) {
			return fmt.Errorf("%w: short loader read %d/%d", ErrProtocol, n, chunk)
		}
		if err := s.transport.Write(ctx, buffer[:chunk], saharaIOTimeout); err != nil {
			return fmt.Errorf("write Firehose loader: %w", err)
		}
		current += chunk
		remaining -= chunk
	}
	return nil
}

func (s *Session) fillPending(ctx context.Context, required int, timeout time.Duration, limit int) error {
	for len(s.pending) < required {
		if required > limit || len(s.pending) >= limit {
			return fmt.Errorf("%w: buffered packet exceeds %d bytes", ErrProtocol, limit)
		}
		readSize := 64 << 10
		if remaining := limit - len(s.pending); readSize > remaining {
			readSize = remaining
		}
		buf := make([]byte, readSize)
		n, err := s.transport.Read(ctx, buf, timeout)
		if err != nil {
			return err
		}
		if n == 0 {
			continue
		}
		if n < 0 || n > len(buf) {
			return fmt.Errorf("%w: invalid transport read length %d", ErrProtocol, n)
		}
		s.pending = append(s.pending, buf[:n]...)
	}
	return nil
}
