package darwin

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeDIAGEndpoint struct {
	writes  [][]byte
	reads   [][]byte
	drained bool
}

func (f *fakeDIAGEndpoint) Write(_ context.Context, payload []byte, _ time.Duration) error {
	f.writes = append(f.writes, append([]byte(nil), payload...))
	return nil
}
func (f *fakeDIAGEndpoint) Read(_ context.Context, payload []byte, _ time.Duration) (int, error) {
	if !f.drained {
		f.drained = true
		return 0, errors.New("drained")
	}
	if len(f.reads) == 0 {
		return 0, errors.New("empty")
	}
	data := f.reads[0]
	f.reads = f.reads[1:]
	copy(payload, data)
	return len(data), nil
}

func TestRunDIAGSwitchWritesExactFrameAndChecksEcho(t *testing.T) {
	endpoint := &fakeDIAGEndpoint{reads: [][]byte{{diagEDLFrame[0], diagEDLFrame[1], diagEDLFrame[2], diagEDLFrame[3], diagEDLFrame[4], diagEDLFrame[5], diagEDLFrame[6]}}}
	if err := runDIAGSwitch(context.Background(), endpoint, time.Second); err != nil {
		t.Fatal(err)
	}
	if len(endpoint.writes) != 1 || string(endpoint.writes[0]) != string(diagEDLFrame) {
		t.Fatalf("writes=%v", endpoint.writes)
	}
}

func TestRunDIAGSwitchAcceptsHDLCWrappedEcho(t *testing.T) {
	wrapped := []byte{0x4b, 0x65, 0x01, 0x00, 0x54, 0x0f, 0x7d, 0x5e, 0x35, 0x5c, 0x7e}
	endpoint := &fakeDIAGEndpoint{reads: [][]byte{wrapped[:5], wrapped[5:]}}
	if err := runDIAGSwitch(context.Background(), endpoint, time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeDIAGFrameRejectsInvalidCRC(t *testing.T) {
	frame := []byte{0x4b, 0x65, 0x01, 0x00, 0x54, 0x0f, 0x7d, 0x5e, 0x00, 0x00, 0x7e}
	if _, err := decodeDIAGFrame(frame); err == nil {
		t.Fatal("decodeDIAGFrame() accepted an invalid CRC")
	}
}

func TestRunDIAGSwitchRejectsEchoMismatch(t *testing.T) {
	endpoint := &fakeDIAGEndpoint{reads: [][]byte{{0, 1, 2, 3, 4, 5, 0x7e}}}
	if err := runDIAGSwitch(context.Background(), endpoint, 200*time.Millisecond); !errors.Is(err, errDIAGEchoMismatch) {
		t.Fatalf("err=%v, want echo mismatch", err)
	}
}
