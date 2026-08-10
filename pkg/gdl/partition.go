package gdl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	mibibMagic1       = 0x55ee73aa
	mibibMagic2       = 0xe35ebddb
	mibibHeaderSize   = 16
	mibibEntrySize    = 28
	maxMIBIBEntries   = 4096
	maxMIBIBScanBytes = 16 << 20
)

var ErrPartitionTable = errors.New("invalid MIBIB partition table")

// Partition describes one Qualcomm NAND MIBIB entry. Offset and Length are
// byte values. StartBlock and BlockCount use the negotiated NAND block size.
type Partition struct {
	Name       string `json:"name"`
	StartBlock uint64 `json:"start_block"`
	BlockCount uint64 `json:"block_count"`
	Offset     uint64 `json:"offset"`
	Length     uint64 `json:"length"`
	Attr1      byte   `json:"attr1"`
	Attr2      byte   `json:"attr2"`
	Attr3      byte   `json:"attr3"`
	Flash      byte   `json:"flash"`
}

// ParseMIBIB returns the first valid Qualcomm NAND MIBIB table in data.
func ParseMIBIB(data []byte, geometry Geometry) ([]Partition, error) {
	if err := geometry.Validate(); err != nil {
		return nil, err
	}
	if len(data) > maxMIBIBScanBytes {
		data = data[:maxMIBIBScanBytes]
	}
	magic := []byte{0xaa, 0x73, 0xee, 0x55, 0xdb, 0xbd, 0x5e, 0xe3}
	searchAt := 0
	var candidateErr error
	for searchAt < len(data) {
		relative := bytes.Index(data[searchAt:], magic)
		if relative < 0 {
			break
		}
		offset := searchAt + relative
		partitions, err := parseMIBIBAt(data, offset, geometry)
		if err == nil {
			return partitions, nil
		}
		candidateErr = err
		searchAt = offset + 1
	}
	if candidateErr != nil {
		return nil, candidateErr
	}
	return nil, fmt.Errorf("%w: magic was not found", ErrPartitionTable)
}

func parseMIBIBAt(data []byte, offset int, geometry Geometry) ([]Partition, error) {
	if offset < 0 || offset > len(data)-mibibHeaderSize {
		return nil, fmt.Errorf("%w: truncated header", ErrPartitionTable)
	}
	header := data[offset : offset+mibibHeaderSize]
	if binary.LittleEndian.Uint32(header[0:4]) != mibibMagic1 || binary.LittleEndian.Uint32(header[4:8]) != mibibMagic2 {
		return nil, fmt.Errorf("%w: magic does not match", ErrPartitionTable)
	}
	count := uint64(binary.LittleEndian.Uint32(header[12:16]))
	if count == 0 || count > maxMIBIBEntries {
		return nil, fmt.Errorf("%w: partition count %d is outside the supported range", ErrPartitionTable, count)
	}
	entriesBytes := count * mibibEntrySize
	available := uint64(len(data) - offset - mibibHeaderSize)
	if entriesBytes > available {
		return nil, fmt.Errorf("%w: %d partition entries are truncated", ErrPartitionTable, count)
	}

	partitions := make([]Partition, 0, count)
	names := make(map[string]struct{}, count)
	for index := uint64(0); index < count; index++ {
		entryOffset := uint64(offset+mibibHeaderSize) + index*mibibEntrySize
		entry := data[entryOffset : entryOffset+mibibEntrySize]
		nameBytes := entry[:16]
		if end := bytes.IndexByte(nameBytes, 0); end >= 0 {
			nameBytes = nameBytes[:end]
		}
		if !utf8.Valid(nameBytes) {
			return nil, fmt.Errorf("%w: entry %d has an invalid UTF-8 name", ErrPartitionTable, index)
		}
		name := strings.TrimSpace(string(nameBytes))
		name = strings.TrimPrefix(name, "0:")
		if name == "" {
			return nil, fmt.Errorf("%w: entry %d has an empty name", ErrPartitionTable, index)
		}
		if _, duplicate := names[name]; duplicate {
			return nil, fmt.Errorf("%w: duplicate partition name %q", ErrPartitionTable, name)
		}

		startBlock := uint64(binary.LittleEndian.Uint32(entry[16:20]))
		blockCount := uint64(binary.LittleEndian.Uint32(entry[20:24]) & 0xffff)
		if blockCount == 0 {
			return nil, fmt.Errorf("%w: partition %q has zero blocks", ErrPartitionTable, name)
		}
		if startBlock >= geometry.TotalBlocks || blockCount > geometry.TotalBlocks-startBlock {
			return nil, fmt.Errorf("%w: partition %q exceeds NAND storage", ErrPartitionTable, name)
		}
		if startBlock > ^uint64(0)/geometry.BlockSize || blockCount > ^uint64(0)/geometry.BlockSize {
			return nil, fmt.Errorf("%w: partition %q byte range overflows", ErrPartitionTable, name)
		}

		partition := Partition{
			Name:       name,
			StartBlock: startBlock,
			BlockCount: blockCount,
			Offset:     startBlock * geometry.BlockSize,
			Length:     blockCount * geometry.BlockSize,
			Attr1:      entry[24],
			Attr2:      entry[25],
			Attr3:      entry[26],
			Flash:      entry[27],
		}
		names[name] = struct{}{}
		partitions = append(partitions, partition)
	}
	return partitions, nil
}
