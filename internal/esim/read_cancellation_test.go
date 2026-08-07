package esim

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/damonto/euicc-go/bertlv"
	"github.com/damonto/euicc-go/lpa"
)

// gatedTransmitter blocks APDU transmits until release, so the test can hold
// the scan inside the first AID while the request context is cancelled.
type gatedTransmitter struct {
	fakeProfileOperationTransmitter
	first   chan struct{}
	release chan struct{}
}

func (t gatedTransmitter) Transmit(request bertlv.Marshaler, response bertlv.Unmarshaler) error {
	select {
	case <-t.first:
	default:
		close(t.first)
	}
	<-t.release
	return t.fakeProfileOperationTransmitter.Transmit(request, response)
}

// TestEsimOverviewReadCancellationStopsMidScan verifies a cancelled read
// request stops promptly between per-AID APDU steps instead of completing a
// full profile scan, releasing opMu and the arbiter.
func TestEsimOverviewReadCancellationStopsMidScan(t *testing.T) {
	eid, err := hex.DecodeString(fixtureEID)
	if err != nil {
		t.Fatal(err)
	}
	first := make(chan struct{})
	release := make(chan struct{})
	mgr := newManagerWithChannelFactory("dev-cancel", func(aid []byte) (*lpa.Client, error) {
		return &lpa.Client{APDU: gatedTransmitter{
			fakeProfileOperationTransmitter: fakeProfileOperationTransmitter{eid: eid},
			first:                           first,
			release:                         release,
		}}, nil
	}, nil, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := mgr.GetEsimOverview(ctx)
		done <- err
	}()

	// Hold the scan inside the first AID's APDU, then cancel the request.
	select {
	case <-first:
	case <-time.After(2 * time.Second):
		t.Fatal("scan never started its first APDU")
	}
	cancel()
	close(release)

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("overview error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled overview scan did not stop promptly")
	}
}
