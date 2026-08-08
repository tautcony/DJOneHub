package sms

import "testing"

func TestParseStorageUsage(t *testing.T) {
	got := parseStorageUsage("AT+CPMS?\r\n+CPMS: \"ME\",19,23,\"ME\",19,23,\"ME\",19,23\r\nOK\r\n")
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	for i, usage := range got {
		if usage.Storage != "ME" || usage.Used != 19 || usage.Total != 23 {
			t.Fatalf("usage[%d] = %+v", i, usage)
		}
	}
}

func TestParseStorageUsageRejectsMalformedResponse(t *testing.T) {
	if got := parseStorageUsage("+CPMS: \"ME\",bad,23\r\nOK\r\n"); len(got) != 0 {
		t.Fatalf("got = %+v, want empty", got)
	}
}
