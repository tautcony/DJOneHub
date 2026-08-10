package gdl

import (
	"encoding/binary"
	"errors"
	"testing"
)

func TestParseMIBIB(t *testing.T) {
	geometry := Geometry{MemoryName: "NAND", TotalBlocks: 64, BlockSize: 131072, PageSize: 2048, PagesPerBlock: 64}
	data := make([]byte, 4096)
	offset := 512
	writeSyntheticMIBIB(data[offset:], []syntheticPartition{
		{name: "0:boot", start: 0, length: 10, attr1: 0xff, attr2: 1, flash: 0},
		{name: "config", start: 10, length: 0xabcd0003, attr1: 2, attr2: 3, attr3: 4, flash: 1},
	})

	partitions, err := ParseMIBIB(data, geometry)
	if err != nil {
		t.Fatal(err)
	}
	if len(partitions) != 2 {
		t.Fatalf("partition count = %d", len(partitions))
	}
	if got := partitions[0]; got.Name != "boot" || got.Offset != 0 || got.Length != 10*geometry.BlockSize || got.Attr1 != 0xff || got.Attr2 != 1 {
		t.Fatalf("first partition = %+v", got)
	}
	if got := partitions[1]; got.Name != "config" || got.StartBlock != 10 || got.BlockCount != 3 || got.Offset != 10*geometry.BlockSize || got.Length != 3*geometry.BlockSize || got.Attr3 != 4 || got.Flash != 1 {
		t.Fatalf("second partition = %+v", got)
	}
}

func TestParseMIBIBRejectsInvalidTables(t *testing.T) {
	geometry := Geometry{MemoryName: "NAND", TotalBlocks: 64, BlockSize: 131072, PageSize: 2048, PagesPerBlock: 64}
	tests := []struct {
		name  string
		build func() []byte
	}{
		{name: "magic absent", build: func() []byte { return make([]byte, 128) }},
		{name: "truncated header", build: func() []byte {
			data := make([]byte, 8)
			binary.LittleEndian.PutUint32(data[0:4], mibibMagic1)
			binary.LittleEndian.PutUint32(data[4:8], mibibMagic2)
			return data
		}},
		{name: "excessive count", build: func() []byte {
			data := make([]byte, mibibHeaderSize)
			writeSyntheticMIBIBHeader(data, maxMIBIBEntries+1)
			return data
		}},
		{name: "truncated entries", build: func() []byte {
			data := make([]byte, mibibHeaderSize+mibibEntrySize-1)
			writeSyntheticMIBIBHeader(data, 1)
			return data
		}},
		{name: "invalid UTF-8 name", build: func() []byte {
			data := make([]byte, mibibHeaderSize+mibibEntrySize)
			writeSyntheticMIBIB(data, []syntheticPartition{{name: "valid", length: 1}})
			data[mibibHeaderSize] = 0xff
			return data
		}},
		{name: "duplicate name", build: func() []byte {
			data := make([]byte, mibibHeaderSize+2*mibibEntrySize)
			writeSyntheticMIBIB(data, []syntheticPartition{{name: "same", length: 1}, {name: "same", start: 1, length: 1}})
			return data
		}},
		{name: "outside geometry", build: func() []byte {
			data := make([]byte, mibibHeaderSize+mibibEntrySize)
			writeSyntheticMIBIB(data, []syntheticPartition{{name: "outside", start: 63, length: 2}})
			return data
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseMIBIB(test.build(), geometry)
			if err == nil || !errors.Is(err, ErrPartitionTable) {
				t.Fatalf("ParseMIBIB() error = %v", err)
			}
		})
	}
}

type syntheticPartition struct {
	name                string
	start, length       uint32
	attr1, attr2, attr3 byte
	flash               byte
}

func writeSyntheticMIBIB(data []byte, partitions []syntheticPartition) {
	writeSyntheticMIBIBHeader(data, uint32(len(partitions)))
	for index, partition := range partitions {
		entry := data[mibibHeaderSize+index*mibibEntrySize:]
		copy(entry[:16], partition.name)
		binary.LittleEndian.PutUint32(entry[16:20], partition.start)
		binary.LittleEndian.PutUint32(entry[20:24], partition.length)
		entry[24] = partition.attr1
		entry[25] = partition.attr2
		entry[26] = partition.attr3
		entry[27] = partition.flash
	}
}

func writeSyntheticMIBIBHeader(data []byte, count uint32) {
	binary.LittleEndian.PutUint32(data[0:4], mibibMagic1)
	binary.LittleEndian.PutUint32(data[4:8], mibibMagic2)
	binary.LittleEndian.PutUint32(data[8:12], 1)
	binary.LittleEndian.PutUint32(data[12:16], count)
}
