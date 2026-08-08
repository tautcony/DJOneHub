package rawat

import (
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/iniwex5/vohive/pkg/smscodec"
)

// SMSDiagnostic is the decoded view of one CMGL/CMGR storage record. The raw
// response remains available beside it for protocol-level troubleshooting.
type SMSDiagnostic struct {
	Index        int    `json:"index"`
	Indexes      []int  `json:"indexes,omitempty"`
	Status       string `json:"status,omitempty"`
	TPDULength   int    `json:"tpdu_length,omitempty"`
	Sender       string `json:"sender,omitempty"`
	Body         string `json:"body,omitempty"`
	ReceivedAt   string `json:"received_at,omitempty"`
	ConcatRef    int    `json:"concat_ref,omitempty"`
	PartNumber   int    `json:"part_number,omitempty"`
	TotalParts   int    `json:"total_parts,omitempty"`
	MissingParts []int  `json:"missing_parts,omitempty"`
	DecodeError  string `json:"decode_error,omitempty"`
}

func ParseSMSDiagnostics(command, response string) []SMSDiagnostic {
	command = strings.ToUpper(strings.TrimSpace(command))
	switch {
	case strings.HasPrefix(command, "AT+CMGL="):
		return aggregateMultipartDiagnostics(parseCMGLDiagnostics(response))
	case strings.HasPrefix(command, "AT+CMGR="):
		index, _ := strconv.Atoi(strings.TrimPrefix(command, "AT+CMGR="))
		return parseCMGRDiagnostics(response, index)
	default:
		return nil
	}
}

type multipartDiagnosticKey struct {
	sender string
	ref    int
	total  int
}

type multipartDiagnosticGroup struct {
	outputIndex int
	segments    map[int]string
}

func aggregateMultipartDiagnostics(messages []SMSDiagnostic) []SMSDiagnostic {
	out := make([]SMSDiagnostic, 0, len(messages))
	groups := make(map[multipartDiagnosticKey]*multipartDiagnosticGroup)
	for _, message := range messages {
		if message.TotalParts <= 1 || message.ConcatRef == 0 || message.DecodeError != "" {
			out = append(out, message)
			continue
		}
		key := multipartDiagnosticKey{sender: message.Sender, ref: message.ConcatRef, total: message.TotalParts}
		partNumber, partBody := message.PartNumber, message.Body
		group := groups[key]
		if group == nil {
			message.Indexes = []int{message.Index}
			message.Body = ""
			message.PartNumber = 0
			out = append(out, message)
			group = &multipartDiagnosticGroup{outputIndex: len(out) - 1, segments: make(map[int]string)}
			groups[key] = group
		} else {
			out[group.outputIndex].Indexes = append(out[group.outputIndex].Indexes, message.Index)
		}
		if partNumber > 0 && partNumber <= message.TotalParts {
			group.segments[partNumber] = partBody
		}
	}

	for key, group := range groups {
		message := &out[group.outputIndex]
		var body strings.Builder
		for part := 1; part <= key.total; part++ {
			segment, ok := group.segments[part]
			if !ok {
				message.MissingParts = append(message.MissingParts, part)
				continue
			}
			body.WriteString(segment)
		}
		message.Body = body.String()
	}
	return out
}

func parseCMGLDiagnostics(response string) []SMSDiagnostic {
	lines := smsResponseLines(response)
	out := make([]SMSDiagnostic, 0)
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(strings.ToUpper(lines[i]), "+CMGL:") {
			continue
		}
		fields := parseATCSV(strings.TrimSpace(lines[i][len("+CMGL:"):]))
		entry := SMSDiagnostic{Index: -1}
		if len(fields) > 0 {
			entry.Index, _ = strconv.Atoi(fields[0])
		}
		if len(fields) > 1 {
			entry.Status = fields[1]
		}
		if len(fields) > 3 {
			entry.TPDULength, _ = strconv.Atoi(fields[len(fields)-1])
		}
		if i+1 >= len(lines) || isATResultLine(lines[i+1]) || strings.HasPrefix(lines[i+1], "+") {
			entry.DecodeError = "response does not contain a PDU line"
			out = append(out, entry)
			continue
		}
		decodeSMSDiagnostic(&entry, lines[i], lines[i+1])
		i++
		out = append(out, entry)
	}
	return out
}

func parseCMGRDiagnostics(response string, index int) []SMSDiagnostic {
	lines := smsResponseLines(response)
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(strings.ToUpper(lines[i]), "+CMGR:") {
			continue
		}
		fields := parseATCSV(strings.TrimSpace(lines[i][len("+CMGR:"):]))
		entry := SMSDiagnostic{Index: index}
		if len(fields) > 0 {
			entry.Status = fields[0]
		}
		if len(fields) > 2 {
			entry.TPDULength, _ = strconv.Atoi(fields[len(fields)-1])
		}
		if i+1 >= len(lines) || isATResultLine(lines[i+1]) {
			entry.DecodeError = "response does not contain a PDU line"
			return []SMSDiagnostic{entry}
		}
		decodeSMSDiagnostic(&entry, lines[i], lines[i+1])
		return []SMSDiagnostic{entry}
	}
	return nil
}

func decodeSMSDiagnostic(entry *SMSDiagnostic, header, rawPDU string) {
	pdu, _ := smscodec.TrimFullPDUHexByATHeader(strings.TrimSpace(rawPDU), header)
	sender, body, receivedAt, concat, err := smscodec.DecodeDeliverPDUHex(pdu)
	if err != nil {
		entry.DecodeError = fmt.Sprintf("PDU decode failed: %v", err)
		return
	}
	entry.Sender = sender
	entry.Body = body
	if !receivedAt.IsZero() {
		entry.ReceivedAt = receivedAt.Format(time.RFC3339)
	}
	if concat.IsConcat {
		entry.ConcatRef = concat.Ref
		entry.PartNumber = concat.Seq
		entry.TotalParts = concat.Total
	}
}

func smsResponseLines(response string) []string {
	parts := strings.FieldsFunc(response, func(r rune) bool { return r == '\r' || r == '\n' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		line := strings.TrimSpace(part)
		if line == "" || strings.HasPrefix(strings.ToUpper(line), "AT+CMG") {
			continue
		}
		out = append(out, line)
	}
	return out
}

func parseATCSV(value string) []string {
	reader := csv.NewReader(strings.NewReader(value))
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1
	fields, err := reader.Read()
	if err != nil {
		return strings.Split(value, ",")
	}
	for i := range fields {
		fields[i] = strings.TrimSpace(fields[i])
	}
	return fields
}

func isATResultLine(line string) bool {
	upper := strings.ToUpper(strings.TrimSpace(line))
	return upper == "OK" || upper == "ERROR" || strings.HasPrefix(upper, "+CME ERROR") || strings.HasPrefix(upper, "+CMS ERROR")
}
