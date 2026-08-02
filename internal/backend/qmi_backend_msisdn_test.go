package backend

import (
	"context"
	"testing"

	"github.com/iniwex5/vohive/internal/testfixtures"
)

func TestQMIBackendGetMSISDNPassThrough(t *testing.T) {
	src := &qmiBackendSendSourceStub{}
	srcMSISDN := testfixtures.MSISDN
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
	if got != testfixtures.MSISDN {
		t.Fatalf("GetMSISDN()=%q want=%q", got, testfixtures.MSISDN)
	}
}
