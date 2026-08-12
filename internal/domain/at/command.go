// Package at contains protocol-independent AT command diagnostics.
package at

import "strings"

// CommandClass returns a low-cardinality diagnostic domain for an AT command.
// It never returns the command name or its arguments.
func CommandClass(command string) string {
	command = strings.ToUpper(strings.TrimSpace(command))
	switch {
	case command == "ATA", command == "ATH", strings.HasPrefix(command, "ATD"):
		return "call"
	case command == "AT":
		return "basic"
	case !strings.HasPrefix(command, "AT+"):
		return "other"
	}

	name := strings.TrimPrefix(command, "AT+")
	if index := strings.IndexFunc(name, func(r rune) bool {
		return (r < 'A' || r > 'Z') && (r < '0' || r > '9')
	}); index >= 0 {
		name = name[:index]
	}
	switch name {
	case "CSIM", "CCHO", "CGLA", "CCHC":
		return "apdu"
	case "CGSN", "CIMI", "QCCID", "CNUM", "QGMR", "CGMR":
		return "identity"
	case "CEREG", "CGREG", "CREG", "COPS", "QNWINFO", "CSQ", "QENG":
		return "radio"
	case "QSIMSTAT", "CPIN":
		return "sim"
	case "CMGS", "CMGF", "CMGL", "CMGR", "CMGD", "CPMS":
		return "sms"
	case "QCFG":
		return "configuration"
	default:
		return "other"
	}
}
