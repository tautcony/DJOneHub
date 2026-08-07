package smscodec

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/warthog618/sms/encoding/tpdu"
)

func TestTrimFullPDUHexByTPDULengthRemovesStoragePadding(t *testing.T) {
	valid := syntheticDeliverPDU(t, false)
	padded := valid + strings.Repeat("00", 128)
	total, _ := hex.DecodeString(valid)
	got, trimmed := TrimFullPDUHexByTPDULength(padded, len(total)-1)
	if !trimmed {
		t.Fatal("TrimFullPDUHexByTPDULength trimmed=false, want true")
	}
	if got != valid {
		t.Fatalf("trimmed PDU mismatch\ngot  %s\nwant %s", got, valid)
	}
}

func TestTrimFullPDUHexByTPDULengthFallsBackToTPDUDeclaredLength(t *testing.T) {
	valid := syntheticDeliverPDU(t, true)
	padded := valid + strings.Repeat("00", 128)
	total, _ := hex.DecodeString(valid)
	// 固定槽存储的模组会把整个槽长度报进 AT header，声明的 tpduLen 大于真实内容长度。
	// 只按 header 截断仍会残留填充，必须回退到 TPDU 自身声明的 UDL 才能剥干净。
	oversizedTPDULen := len(total) - 1 + 64
	got, trimmed := TrimFullPDUHexByTPDULength(padded, oversizedTPDULen)
	if !trimmed {
		t.Fatal("TrimFullPDUHexByTPDULength trimmed=false, want true")
	}
	if want := valid; got != want {
		t.Fatalf("trimmed PDU mismatch\ngot  %s\nwant %s", got, want)
	}
}

func TestDecodeDeliverTPDUTrimsFixedSlotPadding(t *testing.T) {
	valid := syntheticDeliverPDU(t, true)
	b, err := hexStringToBytesForTest(valid)
	if err != nil {
		t.Fatal(err)
	}
	smscLen := int(b[0])
	tpduBytes := b[1+smscLen:]

	_, text, _, concat, err := DecodeDeliverTPDU(tpduBytes)
	if err != nil {
		t.Fatalf("DecodeDeliverTPDU() error = %v", err)
	}
	if text == "" {
		t.Fatal("DecodeDeliverTPDU() text is empty")
	}
	if !concat.IsConcat || concat.Total != 4 || concat.Seq != 2 {
		t.Fatalf("concat=%+v, want total=4 seq=2", concat)
	}
}

func TestDecodeDeliverTPDUAcceptsNonZeroGSM7SpareBits(t *testing.T) {
	pduBytes, err := hexStringToBytesForTest(syntheticDeliverPDU(t, false))
	if err != nil {
		t.Fatal(err)
	}
	pduBytes[len(pduBytes)-1] |= 0x80

	sender, text, _, concat, err := DecodeDeliverTPDU(pduBytes[1:])
	if err != nil {
		t.Fatalf("DecodeDeliverTPDU() error = %v", err)
	}
	if sender == "" {
		t.Fatal("sender is empty")
	}
	if text != "SYNTHETIC TEST MESSAGE" {
		t.Fatalf("text=%q", text)
	}
	if concat.IsConcat {
		t.Fatalf("concat=%+v, want non-concat", concat)
	}
}

func TestParseATSMSHeaderTPDULengthUsesLastNumericField(t *testing.T) {
	got, ok := ParseATSMSHeaderTPDULength(`+CMGL: 7,1,,38`)
	if !ok || got != 38 {
		t.Fatalf("ParseATSMSHeaderTPDULength()=(%d,%v), want (38,true)", got, ok)
	}
}

func TestTrimFullPDUHexByATHeaderKeepsRawWhenHeaderLengthMissing(t *testing.T) {
	padded := syntheticDeliverPDU(t, false) + "00"
	got, trimmed := TrimFullPDUHexByATHeader(padded, `+CMGR: 0`)
	if trimmed {
		t.Fatal("TrimFullPDUHexByATHeader trimmed=true, want false")
	}
	if got != padded {
		t.Fatalf("got %q want original %q", got, padded)
	}
}

func hexStringToBytesForTest(s string) ([]byte, error) {
	return hex.DecodeString(s)
}

func syntheticDeliverPDU(t *testing.T, concat bool) string {
	t.Helper()
	p := tpdu.TPDU{
		Direction:  tpdu.MT,
		FirstOctet: 0x04,
		OA:         tpdu.Address{Addr: "15551234567", TOA: 0x91},
		SCTS:       tpdu.Timestamp{Time: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)},
		UD:         []byte("SYNTHETIC TEST MESSAGE"),
	}
	if concat {
		p.FirstOctet |= 0x40
		p.UDH = tpdu.UserDataHeader{{ID: 0x00, Data: []byte{0x21, 0x04, 0x02}}}
	}
	b, err := p.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	return strings.ToUpper(hex.EncodeToString(append([]byte{0}, b...)))
}
