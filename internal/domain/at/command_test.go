package at

import "testing"

func TestCommandClassDoesNotExposeArguments(t *testing.T) {
	tests := map[string]string{
		`AT+CCHO="A0000005591010FFFFFFFF8900000100"`: "apdu",
		`ATD+8613800000000;`:                         "call",
		`AT+QCCID`:                                   "identity",
		`AT+QENG="servingcell"`:                      "radio",
		`AT+CPIN="1234"`:                             "sim",
		`AT+UNKNOWN="credential"`:                    "other",
	}
	for command, want := range tests {
		if got := CommandClass(command); got != want {
			t.Fatalf("CommandClass(%q)=%q want %q", command, got, want)
		}
	}
}
