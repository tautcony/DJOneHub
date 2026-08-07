package backend

import (
	"context"
	"testing"

)

func TestQMIBackendGetMSISDNPassThrough(t *testing.T) {
	src := &qmiBackendSendSourceStub{}
	srcMSISDN := fixtureMSISDN
	src.getMSISDN = func(ctx context.Context) (string, error) {
		return srcMSISDN, nil
	}

	backend, err := NewQMIBackend("/dev/null", src)
	if err != nil {
		t.Fatalf("NewQMIBackend failed: %v", err)
	}

	got, err := backend.GetMSISDN(context.Background())
	if err != nil {
		t.Fatalf("GetMSISDN failed: %v", err)
	}
	if got != srcMSISDN {
		t.Fatalf("GetMSISDN()=%q want=%q", got, srcMSISDN)
	}
}

func TestQMIBackendGetMSISDNAddsPlusPrefixForBareDigits(t *testing.T) {
	src := &qmiBackendSendSourceStub{}
	src.getMSISDN = func(ctx context.Context) (string, error) {
		return "15551234567", nil
	}

	backend, err := NewQMIBackend("/dev/null", src)
	if err != nil {
		t.Fatalf("NewQMIBackend failed: %v", err)
	}

	got, err := backend.GetMSISDN(context.Background())
	if err != nil {
		t.Fatalf("GetMSISDN failed: %v", err)
	}
	if got != fixtureMSISDN {
		t.Fatalf("GetMSISDN()=%q want=%q", got, fixtureMSISDN)
	}
}
