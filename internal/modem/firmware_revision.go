package modem

import (
	"fmt"
	"strings"
	"time"
)

// FirmwareRevision records the normalized revision and the command that
// produced it. Live is false only for values retained by a higher-level cache.
type FirmwareRevision struct {
	Value  string
	Source string
	Live   bool
}

// FirmwareRevisionExecutor is the minimal AT command seam used by the shared
// QGMR-first revision policy.
type FirmwareRevisionExecutor func(string, time.Duration) (string, error)

// ProbeFirmwareRevision sends QGMR first and uses CGMR only when the first
// response is unusable. It never manufactures a revision from another field.
func ProbeFirmwareRevision(exec FirmwareRevisionExecutor) (FirmwareRevision, error) {
	if exec == nil {
		return FirmwareRevision{}, fmt.Errorf("firmware revision executor is unavailable")
	}
	for _, command := range []struct {
		name   string
		prefix string
	}{
		{name: "AT+QGMR", prefix: "+QGMR:"},
		{name: "AT+CGMR", prefix: "+CGMR:"},
	} {
		response, err := exec(command.name, 2*time.Second)
		if err != nil {
			continue
		}
		value, ok := ParseFirmwareRevision(response, command.name, command.prefix)
		if ok {
			return FirmwareRevision{Value: value, Source: command.name, Live: true}, nil
		}
	}
	return FirmwareRevision{}, fmt.Errorf("modem returned no unambiguous firmware revision")
}

// ParseFirmwareRevision accepts prefixed and unprefixed revision lines while
// excluding command echo, terminal status, and unrelated unsolicited reports.
// Exactly one plausible value is required.
func ParseFirmwareRevision(response, command, prefix string) (string, bool) {
	command = strings.TrimSpace(command)
	prefix = strings.TrimSpace(prefix)
	values := make([]string, 0, 1)
	for _, raw := range splitLines(response) {
		line := strings.TrimSpace(raw)
		if line == "" || strings.EqualFold(line, "OK") || strings.EqualFold(line, "ERROR") {
			continue
		}
		echo := ""
		if len(prefix) > 1 {
			echo = "AT" + prefix[:len(prefix)-1]
		}
		if strings.EqualFold(line, command) || (echo != "" && strings.EqualFold(line, echo)) {
			continue
		}
		upper := strings.ToUpper(line)
		if strings.HasPrefix(upper, "+CME ERROR:") || strings.HasPrefix(upper, "+CMS ERROR:") {
			continue
		}
		candidate := ""
		if prefix != "" && strings.HasPrefix(strings.ToUpper(line), strings.ToUpper(prefix)) {
			candidate = strings.TrimSpace(line[len(prefix):])
		} else if strings.HasPrefix(line, "+") {
			// Any other + line is an unrelated URC.
			continue
		} else if strings.HasPrefix(strings.ToUpper(line), "AT") {
			continue
		} else {
			candidate = line
		}
		candidate = strings.TrimSpace(strings.Trim(candidate, "\""))
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || strings.EqualFold(candidate, "OK") || strings.EqualFold(candidate, "ERROR") {
			continue
		}
		if len(candidate) > 256 {
			continue
		}
		values = append(values, candidate)
	}
	if len(values) != 1 {
		return "", false
	}
	return values[0], true
}
