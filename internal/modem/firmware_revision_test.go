package modem

import (
	"errors"
	"testing"
	"time"
)

func TestParseFirmwareRevision(t *testing.T) {
	tests := []struct {
		name string
		resp string
		cmd  string
		pref string
		want string
		ok   bool
	}{
		{name: "qgmr prefixed", resp: "AT+QGMR\r\n+QGMR: \"QGMR-1.2.3\"\r\nOK\r\n", cmd: "AT+QGMR", pref: "+QGMR:", want: "QGMR-1.2.3", ok: true},
		{name: "cgmr unprefixed", resp: "AT+CGMR\nCGMR-4.5\nOK\n", cmd: "AT+CGMR", pref: "+CGMR:", want: "CGMR-4.5", ok: true},
		{name: "ignores urc", resp: "AT+QGMR\n+CREG: 0,1\n+QGMR: REV-A\nOK\n", cmd: "AT+QGMR", pref: "+QGMR:", want: "REV-A", ok: true},
		{name: "rejects duplicate", resp: "+QGMR: REV-A\nREV-B\nOK\n", cmd: "AT+QGMR", pref: "+QGMR:", ok: false},
		{name: "rejects missing", resp: "AT+QGMR\n+QGMR:\nERROR\n", cmd: "AT+QGMR", pref: "+QGMR:", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseFirmwareRevision(tt.resp, tt.cmd, tt.pref)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("ParseFirmwareRevision()=(%q,%v), want=(%q,%v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestProbeFirmwareRevisionQGMRFirst(t *testing.T) {
	var commands []string
	got, err := ProbeFirmwareRevision(func(command string, _ time.Duration) (string, error) {
		commands = append(commands, command)
		return "+QGMR: REV-Q\r\nOK\r\n", nil
	})
	if err != nil || got.Value != "REV-Q" || got.Source != "AT+QGMR" || !got.Live {
		t.Fatalf("revision=%+v err=%v", got, err)
	}
	if len(commands) != 1 || commands[0] != "AT+QGMR" {
		t.Fatalf("commands=%v, want only QGMR", commands)
	}
}

func TestProbeFirmwareRevisionFallsBackAfterInvalidQGMR(t *testing.T) {
	var commands []string
	got, err := ProbeFirmwareRevision(func(command string, _ time.Duration) (string, error) {
		commands = append(commands, command)
		if command == "AT+QGMR" {
			return "AT+QGMR\r\nERROR\r\n", nil
		}
		return "AT+CGMR\r\n+CGMR: REV-C\r\nOK\r\n", nil
	})
	if err != nil || got.Value != "REV-C" || got.Source != "AT+CGMR" {
		t.Fatalf("revision=%+v err=%v", got, err)
	}
	if len(commands) != 2 || commands[0] != "AT+QGMR" || commands[1] != "AT+CGMR" {
		t.Fatalf("commands=%v, want QGMR then CGMR", commands)
	}
}

func TestProbeFirmwareRevisionFallsBackAfterTransportError(t *testing.T) {
	got, err := ProbeFirmwareRevision(func(command string, _ time.Duration) (string, error) {
		if command == "AT+QGMR" {
			return "", errors.New("timeout")
		}
		return "REV-C", nil
	})
	if err != nil || got.Value != "REV-C" || got.Source != "AT+CGMR" {
		t.Fatalf("revision=%+v err=%v", got, err)
	}
}
