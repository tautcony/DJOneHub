package rawat

import "testing"

const diagnosticPDU = "079144872000302320048102020000625061028204401AD9775D0E72D7DBE2B21C949E8360B75A4E7683D16AB71B"

func TestParseSMSDiagnosticsCMGL(t *testing.T) {
	response := "AT+CMGL=4\r\n+CMGL: 7,\"REC UNREAD\",,42\r\n" + diagnosticPDU + "FFFF\r\nOK\r\n"
	got := ParseSMSDiagnostics("AT+CMGL=4", response)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Index != 7 || got[0].Status != "REC UNREAD" || got[0].TPDULength != 42 {
		t.Fatalf("metadata = %+v", got[0])
	}
	if got[0].Sender == "" || got[0].Body == "" || got[0].ReceivedAt == "" || got[0].DecodeError != "" {
		t.Fatalf("decoded = %+v", got[0])
	}
}

func TestParseSMSDiagnosticsCMGRDecodeError(t *testing.T) {
	got := ParseSMSDiagnostics("at+cmgr=19", "+CMGR: 0,,3\r\nNOT-A-PDU\r\nOK\r\n")
	if len(got) != 1 || got[0].Index != 19 || got[0].DecodeError == "" {
		t.Fatalf("got = %+v", got)
	}
}

func TestParseSMSDiagnosticsIgnoresOtherCommands(t *testing.T) {
	if got := ParseSMSDiagnostics("AT+CSQ", "+CSQ: 20,99\r\nOK\r\n"); got != nil {
		t.Fatalf("got = %+v, want nil", got)
	}
}

func TestAggregateMultipartDiagnostics(t *testing.T) {
	messages := []SMSDiagnostic{
		{Index: 11, Sender: "10010", Body: "first ", ConcatRef: 7, PartNumber: 1, TotalParts: 3},
		{Index: 13, Sender: "10010", Body: "third", ConcatRef: 7, PartNumber: 3, TotalParts: 3},
		{Index: 12, Sender: "10010", Body: "second ", ConcatRef: 7, PartNumber: 2, TotalParts: 3},
	}
	got := aggregateMultipartDiagnostics(messages)
	if len(got) != 1 || got[0].Body != "first second third" {
		t.Fatalf("got = %+v", got)
	}
	if len(got[0].Indexes) != 3 || len(got[0].MissingParts) != 0 || got[0].PartNumber != 0 {
		t.Fatalf("multipart metadata = %+v", got[0])
	}
}

func TestAggregateMultipartDiagnosticsReportsMissingParts(t *testing.T) {
	got := aggregateMultipartDiagnostics([]SMSDiagnostic{
		{Index: 21, Sender: "10086", Body: "first", ConcatRef: 9, PartNumber: 1, TotalParts: 3},
		{Index: 23, Sender: "10086", Body: "third", ConcatRef: 9, PartNumber: 3, TotalParts: 3},
	})
	if len(got) != 1 || got[0].Body != "firstthird" || len(got[0].MissingParts) != 1 || got[0].MissingParts[0] != 2 {
		t.Fatalf("got = %+v", got)
	}
}
