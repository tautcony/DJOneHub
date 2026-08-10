// Package gdl implements the read-only Qualcomm Sahara and Firehose subset
// used by DJOneHub. The protocol flow is based on the BSD-3-Clause qdl
// reference in qdl-master. This package intentionally exposes no flash, erase,
// patch, provisioning, or unrestricted XML operation.
package gdl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

const (
	defaultMemoryName    = "NAND"
	defaultMaxPayload    = 1 << 20
	defaultPagesPerBlock = 64
	maxXMLBytes          = 1 << 20
	maxLoaderChunk       = 16 << 20
	maxTransferBytes     = uint64(1) << 40
)

var (
	ErrClosed          = errors.New("gdl session is closed")
	ErrTimeout         = errors.New("gdl transport timeout")
	ErrProtocol        = errors.New("gdl protocol error")
	ErrGeometry        = errors.New("invalid Firehose storage geometry")
	ErrLoaderRequired  = errors.New("a Firehose loader is required in Sahara mode")
	ErrUnsupportedMode = errors.New("unsupported Qualcomm download mode")
)

// Transport is one claimed Qualcomm EDL bulk interface. Implementations must
// apply the supplied timeout and return promptly when ctx is cancelled.
type Transport interface {
	Read(context.Context, []byte, time.Duration) (int, error)
	Write(context.Context, []byte, time.Duration) error
	Close() error
}

type Geometry struct {
	MemoryName    string
	TotalBlocks   uint64
	BlockSize     uint64
	PageSize      uint64
	PagesPerBlock uint64
}

func (g Geometry) TotalBytes() (uint64, error) {
	if err := g.Validate(); err != nil {
		return 0, err
	}
	if g.TotalBlocks > maxTransferBytes/g.BlockSize {
		return 0, fmt.Errorf("%w: total image exceeds %d bytes", ErrGeometry, maxTransferBytes)
	}
	return g.TotalBlocks * g.BlockSize, nil
}

func (g Geometry) Validate() error {
	if g.MemoryName != "NAND" {
		return fmt.Errorf("%w: memory type %q is not NAND", ErrGeometry, g.MemoryName)
	}
	if g.PageSize < 512 || g.PageSize > 1<<20 || g.PageSize&(g.PageSize-1) != 0 {
		return fmt.Errorf("%w: page size %d", ErrGeometry, g.PageSize)
	}
	if g.BlockSize < g.PageSize || g.BlockSize > 1<<30 || g.BlockSize%g.PageSize != 0 {
		return fmt.Errorf("%w: block size %d", ErrGeometry, g.BlockSize)
	}
	if g.TotalBlocks == 0 || g.TotalBlocks > 1<<32 {
		return fmt.Errorf("%w: total blocks %d", ErrGeometry, g.TotalBlocks)
	}
	pages := g.BlockSize / g.PageSize
	if g.PagesPerBlock != 0 && g.PagesPerBlock != pages {
		return fmt.Errorf("%w: pages per block %d does not match %d", ErrGeometry, g.PagesPerBlock, pages)
	}
	return nil
}

type Options struct {
	MemoryName    string
	PageSize      uint64
	PagesPerBlock uint64
	MaxPayload    uint64
	Log           func(string)
}

// Session owns one USB transport from the initial Sahara/Firehose detection
// until Reset or Close. ReadFullNAND and Reset are serialized.
type Session struct {
	mu         sync.Mutex
	transport  Transport
	options    Options
	geometry   Geometry
	maxPayload uint64
	pending    []byte
	closed     bool
}

func Connect(ctx context.Context, transport Transport, loader io.ReaderAt, loaderSize int64, options Options) (*Session, error) {
	if transport == nil {
		return nil, errors.New("gdl transport is required")
	}
	if options.MemoryName == "" {
		options.MemoryName = defaultMemoryName
	}
	if options.PageSize == 0 {
		options.PageSize = 2048
	}
	if options.PagesPerBlock == 0 {
		options.PagesPerBlock = defaultPagesPerBlock
	}
	if options.MaxPayload == 0 {
		options.MaxPayload = defaultMaxPayload
	}
	if options.MaxPayload < options.PageSize || options.MaxPayload > 64<<20 {
		return nil, fmt.Errorf("%w: max payload %d", ErrProtocol, options.MaxPayload)
	}
	s := &Session{transport: transport, options: options, maxPayload: options.MaxPayload}
	if err := s.connect(ctx, loader, loaderSize); err != nil {
		_ = transport.Close()
		return nil, err
	}
	return s, nil
}

func (s *Session) Geometry() Geometry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.geometry
}

func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeLocked()
}

func (s *Session) closeLocked() error {
	if s.closed {
		return nil
	}
	s.closed = true
	s.pending = nil
	return s.transport.Close()
}

func (s *Session) log(message string) {
	if s.options.Log != nil {
		s.options.Log(message)
	}
}
