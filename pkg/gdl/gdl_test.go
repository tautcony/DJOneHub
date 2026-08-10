package gdl

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeTransport struct {
	mu        sync.Mutex
	reads     []byte
	readChunk int
	writes    [][]byte
	closed    int
	readErr   error
	timeouts  int
}

func (f *fakeTransport) Read(ctx context.Context, output []byte, _ time.Duration) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.timeouts > 0 {
		f.timeouts--
		return 0, ErrTimeout
	}
	if len(f.reads) == 0 {
		if f.readErr != nil {
			return 0, f.readErr
		}
		return 0, ErrTimeout
	}
	limit := len(output)
	if f.readChunk > 0 && limit > f.readChunk {
		limit = f.readChunk
	}
	if limit > len(f.reads) {
		limit = len(f.reads)
	}
	copy(output, f.reads[:limit])
	f.reads = f.reads[limit:]
	return limit, nil
}

func (f *fakeTransport) Write(ctx context.Context, payload []byte, _ time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	f.writes = append(f.writes, append([]byte(nil), payload...))
	f.mu.Unlock()
	return nil
}

func (f *fakeTransport) Close() error {
	f.mu.Lock()
	f.closed++
	f.mu.Unlock()
	return nil
}

func TestSessionUploadsLoaderReadsNANDAndResetsOnOneTransport(t *testing.T) {
	loader := []byte("synthetic-firehose-loader")
	raw := make([]byte, 2048)
	copy(raw, []byte{0xaa, 0x73, 0xee, 0x55, 0xdb, 0xbd, 0x5e, 0xe3})
	probe := make([]byte, 512)
	stream := bytes.Join([][]byte{
		saharaHelloPacket(),
		saharaReadPacket(0, uint32(len(loader))),
		saharaEndPacket(0),
		saharaDonePacket(0),
		firehoseResponse("ACK", nil),
		firehoseResponse("ACK", map[string]string{"rawmode": "true"}),
		probe,
		firehoseResponse("ACK", map[string]string{"rawmode": "false"}),
		firehoseResponse("ACK", map[string]string{"MaxPayloadSizeToTargetInBytesSupported": "1048576"}),
		firehoseStorageResponse(2, 1024, 512),
		firehoseResponse("ACK", map[string]string{"rawmode": "true"}),
		raw,
		firehoseResponse("ACK", map[string]string{"rawmode": "false"}),
		firehoseResponse("ACK", nil),
	}, nil)
	transport := &fakeTransport{reads: stream, readChunk: 7}
	var logs strings.Builder
	session, err := Connect(context.Background(), transport, bytes.NewReader(loader), int64(len(loader)), Options{
		PageSize: 512, PagesPerBlock: 2, Log: func(value string) { logs.WriteString(value) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := session.Geometry(); got.TotalBlocks != 2 || got.BlockSize != 1024 || got.PageSize != 512 || got.PagesPerBlock != 2 {
		t.Fatalf("geometry = %+v", got)
	}
	var output bytes.Buffer
	var lastDone, lastTotal uint64
	written, err := session.ReadFullNAND(context.Background(), &output, func(done, total uint64) {
		lastDone, lastTotal = done, total
	})
	if err != nil {
		t.Fatal(err)
	}
	if written != uint64(len(raw)) || !bytes.Equal(output.Bytes(), raw) {
		t.Fatalf("NAND output bytes=%d len=%d", written, output.Len())
	}
	if lastDone != uint64(len(raw)) || lastTotal != uint64(len(raw)) {
		t.Fatalf("progress=%d/%d", lastDone, lastTotal)
	}
	if err := session.Reset(context.Background()); err != nil {
		t.Fatal(err)
	}
	if transport.closed != 1 {
		t.Fatalf("transport close count = %d", transport.closed)
	}
	writes := joinWrites(transport.writes)
	if bytes.Count(writes, loader) != 1 {
		t.Fatalf("loader upload count = %d", bytes.Count(writes, loader))
	}
	for _, expected := range []string{
		`<configure`, `MemoryName="NAND"`, `<getstorageinfo`, `<read`,
		`SECTOR_SIZE_IN_BYTES="512"`, `<power value="reset"`,
	} {
		if !bytes.Contains(writes, []byte(expected)) {
			t.Fatalf("writes do not contain %q: %s", expected, writes)
		}
	}
	if !strings.Contains(logs.String(), "Firehose loader uploaded") || !strings.Contains(logs.String(), "Firehose storage") {
		t.Fatalf("logs = %q", logs.String())
	}
}

func TestSessionAttachesToActiveFirehoseWithoutLoader(t *testing.T) {
	transport := &fakeTransport{reads: bytes.Join([][]byte{
		firehoseResponse("ACK", nil),
		firehoseResponse("ACK", nil),
		firehoseStorageResponse(1, 1024, 512),
	}, nil), readChunk: 19}
	session, err := Connect(context.Background(), transport, nil, 0, Options{PageSize: 512, PagesPerBlock: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if bytes.Contains(joinWrites(transport.writes), []byte("loader")) {
		t.Fatal("active Firehose attachment uploaded a loader")
	}
}

func TestSessionProbesActiveFirehoseWithoutGreeting(t *testing.T) {
	transport := &fakeTransport{timeouts: 1, reads: bytes.Join([][]byte{
		firehoseResponse("ACK", nil),
		firehoseStorageResponse(1, 1024, 512),
		firehoseResponse("NAK", map[string]string{"MaxPayloadSizeToTargetInBytesSupported": "16384"}),
		firehoseResponse("ACK", map[string]string{"MaxPayloadSizeToTargetInBytesSupported": "16384"}),
		firehoseStorageResponse(1, 1024, 512),
	}, nil)}
	session, err := Connect(context.Background(), transport, nil, 0, Options{PageSize: 512, PagesPerBlock: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if geometry := session.Geometry(); geometry.TotalBlocks != 1 || geometry.PageSize != 512 {
		t.Fatalf("geometry = %+v", geometry)
	}
	if count := bytes.Count(joinWrites(transport.writes), []byte(`<configure`)); count != 2 {
		t.Fatalf("configure command count = %d", count)
	}
}

func TestSessionRejectsDeviceControlledLoaderRange(t *testing.T) {
	loader := []byte("small")
	transport := &fakeTransport{reads: append(saharaHelloPacket(), saharaRead64Packet(0, maxLoaderChunk+1)...)}
	_, err := Connect(context.Background(), transport, bytes.NewReader(loader), int64(len(loader)), Options{})
	if err == nil || !errors.Is(err, ErrProtocol) {
		t.Fatalf("Connect() error = %v", err)
	}
	if transport.closed != 1 {
		t.Fatalf("transport close count = %d", transport.closed)
	}
}

func TestResetSaharaState(t *testing.T) {
	transport := &fakeTransport{}
	if err := ResetSaharaState(context.Background(), transport); err != nil {
		t.Fatal(err)
	}
	writes := joinWrites(transport.writes)
	if len(writes) != 8 || binary.LittleEndian.Uint32(writes[0:4]) != saharaResetState || binary.LittleEndian.Uint32(writes[4:8]) != 8 {
		t.Fatalf("reset packet = %x", writes)
	}
}

func TestSessionRejectsUnboundedStorageGeometry(t *testing.T) {
	transport := &fakeTransport{reads: bytes.Join([][]byte{
		firehoseResponse("ACK", nil),
		firehoseResponse("ACK", nil),
		firehoseStorageResponse(1<<40, 1<<30, 512),
	}, nil)}
	_, err := Connect(context.Background(), transport, nil, 0, Options{PageSize: 512, PagesPerBlock: 1 << 21})
	if err == nil || !errors.Is(err, ErrGeometry) {
		t.Fatalf("Connect() error = %v", err)
	}
}

func TestReadFullNANDHonorsCancellation(t *testing.T) {
	transport := &fakeTransport{reads: bytes.Join([][]byte{
		firehoseResponse("ACK", nil),
		firehoseResponse("ACK", nil),
		firehoseStorageResponse(2, 1024, 512),
		firehoseResponse("ACK", nil),
	}, nil), readErr: context.Canceled}
	session, err := Connect(context.Background(), transport, nil, 0, Options{PageSize: 512, PagesPerBlock: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = session.ReadFullNAND(ctx, io.Discard, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ReadFullNAND() error = %v", err)
	}
}

func TestReadPartitionTableTwiceOnOneSession(t *testing.T) {
	marker := make([]byte, 512)
	copy(marker, []byte{0xaa, 0x73, 0xee, 0x55, 0xdb, 0xbd, 0x5e, 0xe3})
	table := make([]byte, 1024)
	writeSyntheticMIBIB(table, []syntheticPartition{
		{name: "boot", start: 0, length: 10},
		{name: "system", start: 10, length: 20},
	})
	readResult := func(payload []byte) [][]byte {
		return [][]byte{
			firehoseResponse("ACK", map[string]string{"rawmode": "true"}),
			payload,
			firehoseResponse("ACK", map[string]string{"rawmode": "false"}),
		}
	}
	reads := [][]byte{
		firehoseStorageResponse(1024, 1024, 512),
		firehoseResponse("ACK", map[string]string{"MaxPayloadSizeToTargetInBytesSupported": "1048576"}),
		firehoseStorageResponse(1024, 1024, 512),
	}
	for attempt := 0; attempt < 2; attempt++ {
		reads = append(reads, readResult(marker)...)
		reads = append(reads, readResult(table)...)
	}
	transport := &fakeTransport{
		timeouts:  1,
		readChunk: 17,
		reads:     bytes.Join(reads, nil),
	}
	session, err := Connect(context.Background(), transport, nil, 0, Options{PageSize: 512, PagesPerBlock: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for attempt := 0; attempt < 2; attempt++ {
		partitions, readErr := session.ReadPartitionTable(context.Background())
		if readErr != nil {
			t.Fatalf("attempt %d: %v", attempt+1, readErr)
		}
		if len(partitions) != 2 || partitions[0].Name != "boot" || partitions[1].Name != "system" {
			t.Fatalf("attempt %d partitions = %+v", attempt+1, partitions)
		}
	}
	writes := joinWrites(transport.writes)
	if count := bytes.Count(writes, []byte(`<read`)); count != 4 {
		t.Fatalf("read command count = %d", count)
	}
	for _, expected := range []string{
		`SECTOR_SIZE_IN_BYTES="512"`, `num_partition_sectors="1"`, `num_partition_sectors="2"`,
		`start_sector="640"`, `start_sector="641"`,
	} {
		if !bytes.Contains(writes, []byte(expected)) {
			t.Fatalf("writes do not contain %q", expected)
		}
	}
}

func TestReadSectorsRejectsStorageOverrun(t *testing.T) {
	transport := &fakeTransport{timeouts: 1, reads: bytes.Join([][]byte{
		firehoseStorageResponse(2, 1024, 512),
		firehoseResponse("ACK", map[string]string{"MaxPayloadSizeToTargetInBytesSupported": "1048576"}),
		firehoseStorageResponse(2, 1024, 512),
	}, nil)}
	session, err := Connect(context.Background(), transport, nil, 0, Options{PageSize: 512, PagesPerBlock: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	_, err = session.ReadSectors(context.Background(), 3, 2)
	if err == nil || !errors.Is(err, ErrProtocol) {
		t.Fatalf("ReadSectors() error = %v", err)
	}
}

func saharaPacket(command uint32, length int) []byte {
	packet := make([]byte, length)
	binary.LittleEndian.PutUint32(packet[0:4], command)
	binary.LittleEndian.PutUint32(packet[4:8], uint32(length))
	return packet
}

func saharaHelloPacket() []byte {
	packet := saharaPacket(saharaHello, 0x30)
	binary.LittleEndian.PutUint32(packet[8:12], 2)
	binary.LittleEndian.PutUint32(packet[12:16], 1)
	binary.LittleEndian.PutUint32(packet[16:20], 1<<20)
	binary.LittleEndian.PutUint32(packet[20:24], 0)
	return packet
}

func saharaReadPacket(offset, length uint32) []byte {
	packet := saharaPacket(saharaReadData, 0x14)
	binary.LittleEndian.PutUint32(packet[8:12], 13)
	binary.LittleEndian.PutUint32(packet[12:16], offset)
	binary.LittleEndian.PutUint32(packet[16:20], length)
	return packet
}

func saharaRead64Packet(offset, length uint64) []byte {
	packet := saharaPacket(saharaReadData64, 0x20)
	binary.LittleEndian.PutUint64(packet[8:16], 13)
	binary.LittleEndian.PutUint64(packet[16:24], offset)
	binary.LittleEndian.PutUint64(packet[24:32], length)
	return packet
}

func saharaEndPacket(status uint32) []byte {
	packet := saharaPacket(saharaEndImage, 0x10)
	binary.LittleEndian.PutUint32(packet[8:12], 13)
	binary.LittleEndian.PutUint32(packet[12:16], status)
	return packet
}

func saharaDonePacket(status uint32) []byte {
	packet := saharaPacket(saharaDoneReply, 0x0c)
	binary.LittleEndian.PutUint32(packet[8:12], status)
	return packet
}

func firehoseResponse(value string, attributes map[string]string) []byte {
	var extra strings.Builder
	for key, item := range attributes {
		fmt.Fprintf(&extra, ` %s="%s"`, key, item)
	}
	return []byte(fmt.Sprintf(`<?xml version="1.0" ?><data><response value="%s"%s/></data>`, value, extra.String()))
}

func firehoseStorageResponse(totalBlocks, blockSize, pageSize uint64) []byte {
	value := fmt.Sprintf(`{"storage_info":{"total_blocks":%d,"block_size":%d,"page_size":%d,"mem_type":"NAND"}}`, totalBlocks, blockSize, pageSize)
	value = strings.NewReplacer(`"`, `&quot;`).Replace(value)
	return []byte(fmt.Sprintf(`<?xml version="1.0" ?><data><log value="INFO: %s"/><response value="ACK"/></data>`, value))
}

func joinWrites(writes [][]byte) []byte { return bytes.Join(writes, nil) }
