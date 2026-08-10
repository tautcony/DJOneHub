package gdl

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	firehoseCommandTimeout = 5 * time.Second
	firehoseReadTimeout    = 2 * time.Second
	// Firehose can take several seconds to start after Sahara accepts the
	// loader. qdl waits for this first response before sending XML commands.
	firehoseGreetingTimeout = 5 * time.Second
	maxSectorReadBytes      = 64 << 20
)

var mibibScanSectors = [...]uint64{0x280, 0x400, 0x800}

type firehoseMessage struct {
	response map[string]string
	logs     []string
}

func (s *Session) attachFirehose(ctx context.Context) error {
	if err := s.requestStorageInfo(ctx); err == nil {
		if err := s.configureFirehose(ctx); err != nil {
			return fmt.Errorf("configure active Firehose: %w", err)
		}
		if err := s.requestStorageInfo(ctx); err != nil {
			return fmt.Errorf("refresh Firehose storage information: %w", err)
		}
		s.log("Attached to the active Firehose storage configuration\n")
		return nil
	} else if !errors.Is(err, ErrGeometry) && !errors.Is(err, ErrTimeout) && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	s.log("Active Firehose storage probe did not return geometry; running configure\n")
	return s.initializeFirehose(ctx, false)
}

func (s *Session) initializeFirehose(ctx context.Context, probeStorage bool) error {
	// A newly uploaded loader usually emits an unsolicited capabilities packet.
	// It is safe to continue when the loader does not emit one.
	if message, err := s.readFirehoseMessage(ctx, firehoseGreetingTimeout); err == nil {
		s.emitLogs(message.logs)
	} else if !errors.Is(err, ErrTimeout) && !errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("read Firehose greeting: %w", err)
	}
	if probeStorage {
		// NAND loaders commonly require one sector read to initialize the storage
		// driver before configure is accepted. This matches the qdl reference flow.
		probeAttributes := map[string]string{
			"SECTOR_SIZE_IN_BYTES":      strconv.FormatUint(s.options.PageSize, 10),
			"num_partition_sectors":     "1",
			"physical_partition_number": "0",
			"start_sector":              "0",
		}
		if err := s.readRawCommand(ctx, probeAttributes, s.options.PageSize, "storage probe"); err != nil {
			return fmt.Errorf("read first storage sector: %w", err)
		}
	}

	if err := s.configureFirehose(ctx); err != nil {
		return fmt.Errorf("configure Firehose: %w", err)
	}
	if err := s.requestStorageInfo(ctx); err != nil {
		return fmt.Errorf("get Firehose storage information: %w", err)
	}
	return nil
}

func (s *Session) configureFirehose(ctx context.Context) error {
	attributes := map[string]string{
		"MemoryName":                    s.options.MemoryName,
		"Verbose":                       "0",
		"AlwaysValidate":                "0",
		"MaxDigestTableSizeInBytes":     "2048",
		"MaxPayloadSizeToTargetInBytes": strconv.FormatUint(s.maxPayload, 10),
		"ZLPAwareHost":                  "1",
		"SkipStorageInit":               "0",
		"SkipWrite":                     "0",
	}
	for attempt := 0; attempt < 2; attempt++ {
		attributes["MaxPayloadSizeToTargetInBytes"] = strconv.FormatUint(s.maxPayload, 10)
		message, err := s.exchange(ctx, "configure", attributes, firehoseCommandTimeout)
		if err == nil {
			if supported := parseUint(message.response["MaxPayloadSizeToTargetInBytesSupported"]); supported > 0 {
				if supported > 64<<20 {
					return fmt.Errorf("%w: target payload size %d", ErrProtocol, supported)
				}
				s.maxPayload = supported
			}
			return nil
		}
		if supported := parseUint(message.response["MaxPayloadSizeToTargetInBytesSupported"]); supported > 0 && supported < s.maxPayload {
			s.maxPayload = supported
			continue
		}
		return err
	}
	return fmt.Errorf("%w: configure retry limit reached", ErrProtocol)
}

// requestStorageInfo consumes unsolicited capability documents until the
// getstorageinfo response contains the storage report.
func (s *Session) requestStorageInfo(ctx context.Context) error {
	payload, err := marshalFirehoseCommand("getstorageinfo", map[string]string{"physical_partition_number": "0"})
	if err != nil {
		return err
	}
	if err := s.transport.Write(ctx, payload, firehoseCommandTimeout); err != nil {
		return err
	}
	var lastErr error
	for attempts := 0; attempts < 16; attempts++ {
		message, readErr := s.readFirehoseMessage(ctx, firehoseCommandTimeout)
		if readErr != nil {
			return readErr
		}
		s.emitLogs(message.logs)
		hasStorageReport := false
		for _, line := range message.logs {
			if strings.Contains(line, "storage_info") {
				hasStorageReport = true
				break
			}
		}
		if geometryErr := s.setStorageGeometry(message.logs); geometryErr == nil {
			if len(message.response) > 0 && !isACK(message.response) {
				return fmt.Errorf("%w: getstorageinfo response %v", ErrProtocol, message.response)
			}
			return nil
		} else {
			lastErr = geometryErr
			if hasStorageReport {
				return geometryErr
			}
		}
		if len(message.response) > 0 && !isACK(message.response) {
			return fmt.Errorf("%w: getstorageinfo response %v", ErrProtocol, message.response)
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("%w: storage report is missing", ErrGeometry)
}

func (s *Session) readRawCommand(ctx context.Context, attributes map[string]string, byteCount uint64, label string) error {
	if byteCount == 0 || byteCount > maxSectorReadBytes {
		return fmt.Errorf("%w: %s size %d", ErrProtocol, label, byteCount)
	}
	if _, err := s.exchange(ctx, "read", attributes, firehoseCommandTimeout); err != nil {
		return fmt.Errorf("start %s: %w", label, err)
	}
	if _, err := s.copyRaw(ctx, io.Discard, byteCount, nil); err != nil {
		return fmt.Errorf("read %s payload: %w", label, err)
	}
	message, err := s.readFirehoseMessage(ctx, firehoseCommandTimeout)
	if err != nil {
		return fmt.Errorf("finish %s: %w", label, err)
	}
	s.emitLogs(message.logs)
	if !isACK(message.response) {
		return fmt.Errorf("%w: %s final response was not ACK: %v", ErrProtocol, label, message.response)
	}
	return nil
}

func (s *Session) setStorageGeometry(logs []string) error {
	geometry, err := parseStorageGeometry(logs)
	if err != nil {
		return err
	}
	if geometry.PagesPerBlock == 0 {
		geometry.PagesPerBlock = geometry.BlockSize / geometry.PageSize
	}
	if err := geometry.Validate(); err != nil {
		return err
	}
	if s.options.PageSize != 0 && geometry.PageSize != s.options.PageSize {
		return fmt.Errorf("%w: page size %d does not match expected %d", ErrGeometry, geometry.PageSize, s.options.PageSize)
	}
	if s.options.PagesPerBlock != 0 && geometry.PagesPerBlock != s.options.PagesPerBlock {
		return fmt.Errorf("%w: pages per block %d does not match expected %d", ErrGeometry, geometry.PagesPerBlock, s.options.PagesPerBlock)
	}
	if _, err := geometry.TotalBytes(); err != nil {
		return err
	}
	s.geometry = geometry
	s.log(fmt.Sprintf("Firehose storage: %d blocks, %d-byte blocks, %d-byte pages\n", geometry.TotalBlocks, geometry.BlockSize, geometry.PageSize))
	return nil
}

func (s *Session) ReadFullNAND(ctx context.Context, output io.Writer, progress func(uint64, uint64)) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, ErrClosed
	}
	if output == nil {
		return 0, errors.New("NAND output writer is required")
	}
	total, err := s.geometry.TotalBytes()
	if err != nil {
		return 0, err
	}
	sectors := total / s.geometry.PageSize
	attributes := map[string]string{
		"SECTOR_SIZE_IN_BYTES":      strconv.FormatUint(s.geometry.PageSize, 10),
		"num_partition_sectors":     strconv.FormatUint(sectors, 10),
		"physical_partition_number": "0",
		"start_sector":              "0",
	}
	if _, err := s.exchange(ctx, "read", attributes, firehoseCommandTimeout); err != nil {
		return 0, fmt.Errorf("start NAND read: %w", err)
	}
	if progress != nil {
		progress(0, total)
	}
	written, err := s.copyRaw(ctx, output, total, progress)
	if err != nil {
		return written, err
	}
	message, err := s.readFirehoseMessage(ctx, firehoseCommandTimeout)
	if err != nil {
		return written, fmt.Errorf("finish NAND read: %w", err)
	}
	s.emitLogs(message.logs)
	if !isACK(message.response) {
		return written, fmt.Errorf("%w: NAND read final response was not ACK", ErrProtocol)
	}
	return written, nil
}

// ReadSectors reads a bounded NAND sector range and keeps the Firehose session
// open for the next command.
func (s *Session) ReadSectors(ctx context.Context, startSector, sectorCount uint64) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readSectorsLocked(ctx, startSector, sectorCount)
}

// ReadPartitionTable probes the same NAND table sectors as edl printgpt.
// Repeated calls reuse the current Firehose session and negotiated geometry.
func (s *Session) ReadPartitionTable(ctx context.Context) ([]Partition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	total, err := s.geometry.TotalBytes()
	if err != nil {
		return nil, err
	}
	totalSectors := total / s.geometry.PageSize
	for _, sector := range mibibScanSectors {
		if sector >= totalSectors {
			continue
		}
		marker, readErr := s.readSectorsLocked(ctx, sector, 1)
		if readErr != nil {
			return nil, fmt.Errorf("read MIBIB marker sector 0x%x: %w", sector, readErr)
		}
		if !isMIBIBMarker(marker) {
			continue
		}
		if sector+3 > totalSectors {
			return nil, fmt.Errorf("%w: table at sector 0x%x exceeds NAND storage", ErrPartitionTable, sector)
		}
		table, readErr := s.readSectorsLocked(ctx, sector+1, 2)
		if readErr != nil {
			return nil, fmt.Errorf("read MIBIB table at sector 0x%x: %w", sector+1, readErr)
		}
		return ParseMIBIB(table, s.geometry)
	}
	return nil, fmt.Errorf("%w: marker was not found", ErrPartitionTable)
}

func isMIBIBMarker(data []byte) bool {
	if len(data) < 8 {
		return false
	}
	return bytes.Equal(data[:8], []byte{0xaa, 0x73, 0xee, 0x55, 0xdb, 0xbd, 0x5e, 0xe3}) ||
		bytes.Equal(data[:8], []byte{0xac, 0x9f, 0x56, 0xfe, 0x7a, 0x12, 0x7f, 0xcd})
}

func (s *Session) readSectorsLocked(ctx context.Context, startSector, sectorCount uint64) ([]byte, error) {
	if s.closed {
		return nil, ErrClosed
	}
	if sectorCount == 0 {
		return nil, fmt.Errorf("%w: sector count must be greater than zero", ErrProtocol)
	}
	total, err := s.geometry.TotalBytes()
	if err != nil {
		return nil, err
	}
	totalSectors := total / s.geometry.PageSize
	if startSector >= totalSectors || sectorCount > totalSectors-startSector {
		return nil, fmt.Errorf("%w: sector range %d+%d exceeds %d", ErrProtocol, startSector, sectorCount, totalSectors)
	}
	if sectorCount > ^uint64(0)/s.geometry.PageSize {
		return nil, fmt.Errorf("%w: sector byte count overflows", ErrProtocol)
	}
	byteCount := sectorCount * s.geometry.PageSize
	if byteCount > maxSectorReadBytes {
		return nil, fmt.Errorf("%w: sector read size %d exceeds %d bytes", ErrProtocol, byteCount, maxSectorReadBytes)
	}

	attributes := map[string]string{
		"SECTOR_SIZE_IN_BYTES":      strconv.FormatUint(s.geometry.PageSize, 10),
		"num_partition_sectors":     strconv.FormatUint(sectorCount, 10),
		"physical_partition_number": "0",
		"start_sector":              strconv.FormatUint(startSector, 10),
		"PAGES_PER_BLOCK":           strconv.FormatUint(s.geometry.PagesPerBlock, 10),
	}
	if _, err := s.exchange(ctx, "read", attributes, firehoseCommandTimeout); err != nil {
		return nil, fmt.Errorf("start NAND sector read: %w", err)
	}
	var output bytes.Buffer
	output.Grow(int(byteCount))
	if _, err := s.copyRaw(ctx, &output, byteCount, nil); err != nil {
		return nil, err
	}
	message, err := s.readFirehoseMessage(ctx, firehoseCommandTimeout)
	if err != nil {
		return nil, fmt.Errorf("finish NAND sector read: %w", err)
	}
	s.emitLogs(message.logs)
	if !isACK(message.response) {
		return nil, fmt.Errorf("%w: NAND sector read final response was not ACK", ErrProtocol)
	}
	return output.Bytes(), nil
}

func (s *Session) Reset(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	_, err := s.exchange(ctx, "power", map[string]string{"value": "reset"}, firehoseCommandTimeout)
	closeErr := s.closeLocked()
	if err != nil {
		return fmt.Errorf("reset Firehose device: %w", err)
	}
	return closeErr
}

func (s *Session) copyRaw(ctx context.Context, output io.Writer, total uint64, progress func(uint64, uint64)) (uint64, error) {
	var written uint64
	write := func(data []byte) error {
		for len(data) > 0 {
			n, err := output.Write(data)
			if err != nil {
				return err
			}
			if n <= 0 || n > len(data) {
				return io.ErrShortWrite
			}
			written += uint64(n)
			data = data[n:]
			if progress != nil {
				progress(written, total)
			}
		}
		return nil
	}
	if uint64(len(s.pending)) > total {
		if err := write(s.pending[:total]); err != nil {
			return written, err
		}
		s.pending = s.pending[total:]
		return written, nil
	}
	if len(s.pending) > 0 {
		if err := write(s.pending); err != nil {
			return written, err
		}
		s.pending = nil
	}
	buffer := make([]byte, 5<<20)
	for written < total {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		remaining := total - written
		readSize := uint64(len(buffer))
		if remaining < readSize {
			readSize = remaining
		}
		n, err := s.transport.Read(ctx, buffer[:readSize], firehoseReadTimeout)
		if err != nil {
			return written, fmt.Errorf("read NAND payload at byte %d: %w", written, err)
		}
		if n == 0 {
			continue
		}
		if n < 0 || n > int(readSize) {
			return written, fmt.Errorf("%w: invalid NAND read length %d", ErrProtocol, n)
		}
		if err := write(buffer[:n]); err != nil {
			return written, fmt.Errorf("write NAND output: %w", err)
		}
	}
	return written, nil
}

func (s *Session) exchange(ctx context.Context, command string, attributes map[string]string, timeout time.Duration) (firehoseMessage, error) {
	payload, err := marshalFirehoseCommand(command, attributes)
	if err != nil {
		return firehoseMessage{}, err
	}
	if err := s.transport.Write(ctx, payload, timeout); err != nil {
		return firehoseMessage{}, err
	}
	for {
		message, err := s.readFirehoseMessage(ctx, timeout)
		if err != nil {
			return firehoseMessage{}, err
		}
		s.emitLogs(message.logs)
		if len(message.response) == 0 {
			continue
		}
		if !isACK(message.response) {
			return message, fmt.Errorf("%w: Firehose %s response %v", ErrProtocol, command, message.response)
		}
		return message, nil
	}
}

func marshalFirehoseCommand(command string, attributes map[string]string) ([]byte, error) {
	if command == "" {
		return nil, errors.New("Firehose command is required")
	}
	var buffer bytes.Buffer
	buffer.WriteString(`<?xml version="1.0" encoding="UTF-8" ?><data><`)
	buffer.WriteString(command)
	keys := make([]string, 0, len(attributes))
	for key := range attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		buffer.WriteByte(' ')
		buffer.WriteString(key)
		buffer.WriteString(`="`)
		if err := xml.EscapeText(&buffer, []byte(attributes[key])); err != nil {
			return nil, err
		}
		buffer.WriteByte('"')
	}
	buffer.WriteString(`/>
</data>`)
	return buffer.Bytes(), nil
}

func (s *Session) readFirehoseMessage(ctx context.Context, timeout time.Duration) (firehoseMessage, error) {
	const closing = "</data>"
	for {
		if end := bytes.Index(s.pending, []byte(closing)); end >= 0 {
			end += len(closing)
			document := append([]byte(nil), s.pending[:end]...)
			s.pending = s.pending[end:]
			return parseFirehoseMessage(document)
		}
		if len(s.pending) >= maxXMLBytes {
			return firehoseMessage{}, fmt.Errorf("%w: Firehose XML exceeds %d bytes", ErrProtocol, maxXMLBytes)
		}
		buffer := make([]byte, 64<<10)
		n, err := s.transport.Read(ctx, buffer, timeout)
		if err != nil {
			return firehoseMessage{}, err
		}
		if n == 0 {
			continue
		}
		if n < 0 || n > len(buffer) {
			return firehoseMessage{}, fmt.Errorf("%w: invalid Firehose read length %d", ErrProtocol, n)
		}
		s.pending = append(s.pending, buffer[:n]...)
	}
}

func parseFirehoseMessage(document []byte) (firehoseMessage, error) {
	decoder := xml.NewDecoder(bytes.NewReader(bytes.Trim(document, "\x00\r\n \t")))
	message := firehoseMessage{}
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return firehoseMessage{}, fmt.Errorf("%w: parse Firehose XML: %v", ErrProtocol, err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		attributes := make(map[string]string, len(start.Attr))
		for _, attribute := range start.Attr {
			attributes[attribute.Name.Local] = attribute.Value
		}
		switch strings.ToLower(start.Name.Local) {
		case "log":
			if value := attributes["value"]; value != "" {
				message.logs = append(message.logs, value)
			}
		case "response":
			message.response = attributes
		}
	}
	return message, nil
}

func parseStorageGeometry(logs []string) (Geometry, error) {
	for _, line := range logs {
		start := strings.IndexByte(line, '{')
		if start < 0 || !strings.Contains(line[start:], "storage_info") {
			continue
		}
		var payload struct {
			Storage struct {
				TotalBlocks uint64 `json:"total_blocks"`
				BlockSize   uint64 `json:"block_size"`
				PageSize    uint64 `json:"page_size"`
				MemoryType  string `json:"mem_type"`
			} `json:"storage_info"`
		}
		if err := json.Unmarshal([]byte(line[start:]), &payload); err != nil {
			return Geometry{}, fmt.Errorf("%w: parse storage report: %v", ErrGeometry, err)
		}
		geometry := Geometry{
			MemoryName:  payload.Storage.MemoryType,
			TotalBlocks: payload.Storage.TotalBlocks,
			BlockSize:   payload.Storage.BlockSize,
			PageSize:    payload.Storage.PageSize,
		}
		if geometry.PageSize > 0 {
			geometry.PagesPerBlock = geometry.BlockSize / geometry.PageSize
		}
		return geometry, nil
	}
	return Geometry{}, fmt.Errorf("%w: Firehose storage report is missing", ErrGeometry)
}

func looksLikeXML(data []byte) bool {
	trimmed := bytes.TrimLeft(data, "\x00\r\n \t")
	return len(trimmed) > 0 && trimmed[0] == '<'
}

func isACK(response map[string]string) bool {
	return strings.EqualFold(response["value"], "ACK")
}

func parseUint(value string) uint64 {
	parsed, _ := strconv.ParseUint(strings.TrimSpace(value), 0, 64)
	return parsed
}

func (s *Session) emitLogs(logs []string) {
	for _, line := range logs {
		s.log(line + "\n")
	}
}
