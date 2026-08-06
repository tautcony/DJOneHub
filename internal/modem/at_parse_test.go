package modem

import (
	"errors"
	"strings"
	"testing"
)

func TestParseServingCellLTE(t *testing.T) {
	tests := []struct {
		name     string
		resp     string
		wantRSRP int
		wantRSRQ int
		wantOK   bool
	}{
		{
			name:     "extracts lte rsrp and rsrq from trailing servingcell fields",
			resp:     "\r\n+QENG: \"servingcell\",\"NOCONN\",\"LTE\",\"FDD\",460,01,8401A29,132,3740,8,3,3,-95,5992,-75,-8,-50,11,44\r\n\r\nOK\r\n",
			wantRSRP: -75,
			wantRSRQ: -8,
			wantOK:   true,
		},
		{
			name:     "rejects non lte servingcell response",
			resp:     "\r\n+QENG: \"servingcell\",\"NOCONN\",\"WCDMA\",460,01\r\n\r\nOK\r\n",
			wantRSRP: 0,
			wantRSRQ: 0,
			wantOK:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRSRP, gotRSRQ, gotOK := parseServingCellLTE(tt.resp)
			if gotRSRP != tt.wantRSRP || gotRSRQ != tt.wantRSRQ || gotOK != tt.wantOK {
				t.Fatalf("parseServingCellLTE()=(%d,%d,%v) want=(%d,%d,%v)", gotRSRP, gotRSRQ, gotOK, tt.wantRSRP, tt.wantRSRQ, tt.wantOK)
			}
		})
	}
}

func TestParseRegistrationDomains(t *testing.T) {
	tests := []struct {
		name       string
		response   string
		prefix     string
		wantStatus int
		wantLAC    string
		wantCellID string
	}{
		{name: "EPS roaming", response: "+CEREG: 0,5,\"00AF\",\"1234\",7", prefix: "+CEREG:", wantStatus: 5, wantLAC: "00AF", wantCellID: "1234"},
		{name: "packet registered", response: "+CGREG: 0,1", prefix: "+CGREG:", wantStatus: 1},
		{name: "status only", response: "+CREG: 1", prefix: "+CREG:", wantStatus: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, lac, cellID, ok := parseRegistration(tt.response, tt.prefix)
			if !ok || status != tt.wantStatus || lac != tt.wantLAC || cellID != tt.wantCellID {
				t.Fatalf("parseRegistration()=(%d,%q,%q,%v), want=(%d,%q,%q,true)", status, lac, cellID, ok, tt.wantStatus, tt.wantLAC, tt.wantCellID)
			}
		})
	}
}

func TestParseQCCID(t *testing.T) {
	tests := []struct {
		name string
		resp string
		want string
	}{
		{
			name: "plain digits",
			resp: "\r\n+QCCID: 8986001234567890123\r\n\r\nOK\r\n",
			want: "8986001234567890123",
		},
		{
			name: "trim trailing uppercase F",
			resp: "\r\n+QCCID: 8986001234567890123F\r\n\r\nOK\r\n",
			want: "8986001234567890123",
		},
		{
			name: "trim trailing lowercase f",
			resp: "\r\n+QCCID: 8986001234567890123f\r\n\r\nOK\r\n",
			want: "8986001234567890123",
		},
		{
			name: "quoted value with padding",
			resp: "\r\n+QCCID: \"8986001234567890123F\"\r\n\r\nOK\r\n",
			want: "8986001234567890123",
		},
		{
			name: "missing qccid line",
			resp: "\r\nOK\r\n",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseQCCID(tt.resp)
			if got != tt.want {
				t.Fatalf("parseQCCID()=%q want=%q", got, tt.want)
			}
		})
	}
}

func TestExtractSMSPDUAfterPrefixTrimsCMGRPadding(t *testing.T) {
	valid := "079144872000302320048102020000625061028204401AD9775D0E72D7DBE2B21C949E8360B75A4E7683D16AB71B"
	resp := "\r\n+CMGR: 0,,38\r\n" + valid + strings.Repeat("00", 64) + "\r\n\r\nOK\r\n"

	got, ok := extractSMSPDUAfterPrefix(resp, "+CMGR:")
	if !ok {
		t.Fatal("extractSMSPDUAfterPrefix ok=false, want true")
	}
	if got != valid {
		t.Fatalf("got %s want %s", got, valid)
	}
}

func TestExtractAllSMSPDUsAfterPrefixTrimsCMGLPadding(t *testing.T) {
	valid := "079144872000302320048102020000625061028204401AD9775D0E72D7DBE2B21C949E8360B75A4E7683D16AB71B"
	resp := "\r\n+CMGL: 3,1,,38\r\n" + valid + strings.Repeat("00", 64) + "\r\n\r\nOK\r\n"

	got := extractAllSMSPDUsAfterPrefix(resp, "+CMGL:")
	if len(got) != 1 {
		t.Fatalf("len(got)=%d want 1", len(got))
	}
	if got[0] != valid {
		t.Fatalf("got %s want %s", got[0], valid)
	}
}

// TestExtractAllSMSEntriesPreservesTrueStorageIndexes: 列表解析保留模组的真实
// 存储索引（而非合成循环下标），供后续 list/read/delete 使用。
func TestExtractAllSMSEntriesPreservesTrueStorageIndexes(t *testing.T) {
	valid := "079144872000302320048102020000625061028204401AD9775D0E72D7DBE2B21C949E8360B75A4E7683D16AB71B"
	resp := "\r\n+CMGL: 7,1,,38\r\n" + valid + "\r\n+CMGL: 3,0,,38\r\n" + valid + "\r\n+CMGL: 42,1,,38\r\n" + valid + "\r\n\r\nOK\r\n"

	got := extractAllSMSEntriesAfterPrefix(resp, "+CMGL:")
	if len(got) != 3 {
		t.Fatalf("len(got)=%d want 3", len(got))
	}
	wantIndexes := []uint32{7, 3, 42}
	for i, entry := range got {
		if entry.Index != wantIndexes[i] {
			t.Fatalf("entry %d index=%d want %d", i, entry.Index, wantIndexes[i])
		}
		if entry.PDU != valid {
			t.Fatalf("entry %d pdu mismatched", i)
		}
	}
}

func TestParseServingCellLTEInfoIncludesRadio(t *testing.T) {
	info, ok := parseServingCellLTEInfo("\r\n+QENG: \"servingcell\",\"NOCONN\",\"LTE\",\"FDD\",460,01,8401A29,132,3740,8,3,3,-95,5992,-75,-8,-50,11,44\r\n\r\nOK\r\n")
	if !ok {
		t.Fatal("parseServingCellLTEInfo() ok=false")
	}
	if info.RSRP != -75 || info.RSRQ != -8 || info.SINR != 11 || info.Duplex != "FDD" || info.Band != "LTE BAND 8" || info.Channel != 3740 {
		t.Fatalf("parseServingCellLTEInfo()=%+v", info)
	}
}

func TestParseServingCellLTEInfoAllowsUnknownTrailingField(t *testing.T) {
	response := "\r\n+QENG: \"servingcell\",\"NOCONN\",\"LTE\",\"FDD\",460,11,AA092E,283,1552,3,5,5,E859,-92,-8,-61,19,-\r\n\r\nOK\r\n"
	info, ok := parseServingCellLTEInfo(response)
	if !ok {
		t.Fatal("parseServingCellLTEInfo() ok=false")
	}
	if info.RSRP != -92 || info.RSRQ != -8 || info.SINR != 19 || info.Duplex != "FDD" || info.Band != "LTE BAND 3" || info.Channel != 1552 {
		t.Fatalf("parseServingCellLTEInfo()=%+v", info)
	}
}

func TestParseQNWInfoModeAndDuplex(t *testing.T) {
	tests := []struct {
		name       string
		resp       string
		wantMode   string
		wantDuplex string
	}{
		{
			name:       "splits fdd lte into rat and duplex",
			resp:       "\r\n+QNWINFO: \"FDD LTE\",\"46001\",\"LTE BAND 8\",3740\r\n\r\nOK\r\n",
			wantMode:   "LTE",
			wantDuplex: "FDD",
		},
		{
			name:       "splits tdd lte into rat and duplex",
			resp:       "\r\n+QNWINFO: \"TDD LTE\",\"46000\",\"LTE BAND 41\",39150\r\n\r\nOK\r\n",
			wantMode:   "LTE",
			wantDuplex: "TDD",
		},
		{
			name:       "keeps non lte as pure rat",
			resp:       "\r\n+QNWINFO: \"WCDMA\",\"46001\",\"WCDMA 2100\",10688\r\n\r\nOK\r\n",
			wantMode:   "WCDMA",
			wantDuplex: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMode, gotDuplex := parseQNWInfoModeAndDuplex(tt.resp)
			if gotMode != tt.wantMode || gotDuplex != tt.wantDuplex {
				t.Fatalf("parseQNWInfoModeAndDuplex()=(%q,%q) want=(%q,%q)", gotMode, gotDuplex, tt.wantMode, tt.wantDuplex)
			}
		})
	}
}

func TestParseQNWInfoRadioIncludesBandAndChannel(t *testing.T) {
	mode, duplex, band, channel := parseQNWInfoRadio("\r\n+QNWINFO: \"FDD LTE\",\"46001\",\"LTE BAND 8\",3740\r\n\r\nOK\r\n")
	if mode != "LTE" || duplex != "FDD" || band != "LTE BAND 8" || channel != 3740 {
		t.Fatalf("parseQNWInfoRadio()=(%q,%q,%q,%d)", mode, duplex, band, channel)
	}
}

func TestParseCOPSOperatorResponse(t *testing.T) {
	tests := []struct {
		name string
		resp string
		want string
	}{
		{
			name: "numeric format plmn mapped to display name",
			resp: "\r\n+COPS: 0,2,\"46011\",7\r\n\r\nOK\r\n",
			want: "中国电信",
		},
		{
			name: "numeric format unknown plmn falls back to raw code",
			resp: "\r\n+COPS: 0,2,\"99999\",7\r\n\r\nOK\r\n",
			want: "99999",
		},
		{
			name: "long alphanumeric format passes name through",
			resp: "\r\n+COPS: 0,0,\"CHN-UNICOM\",7\r\n\r\nOK\r\n",
			want: "CHN-UNICOM",
		},
		{
			name: "short alphanumeric format passes name through",
			resp: "\r\n+COPS: 0,1,\"UNICOM\",7\r\n\r\nOK\r\n",
			want: "UNICOM",
		},
		{
			name: "missing operator payload",
			resp: "\r\n+COPS: 0,2\r\n\r\nOK\r\n",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCOPSOperatorResponse(tt.resp)
			if err != nil {
				t.Fatalf("parseCOPSOperatorResponse() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("parseCOPSOperatorResponse()=%q want=%q", got, tt.want)
			}
		})
	}
}

func TestParseCOPSOperatorResponseClassifiesUnsupportedFormat(t *testing.T) {
	for name, resp := range map[string]string{
		"format 3":       "\r\n+COPS: 0,3,\"46011\",7\r\n\r\nOK\r\n",
		"format garbage": "\r\n+COPS: 0,xyz,\"46011\",7\r\n\r\nOK\r\n",
		"no cops line":   "\r\nOK\r\n",
		"no format field": "\r\n+COPS: 0\r\n\r\nOK\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := parseCOPSOperatorResponse(resp)
			if err == nil {
				t.Fatalf("parseCOPSOperatorResponse() = %q, want classified error", got)
			}
			if !errors.Is(err, errCOPSFormatUnsupported) {
				t.Fatalf("error = %v, want %v", err, errCOPSFormatUnsupported)
			}
		})
	}
}
