package windows

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
)

func TestObserveEDLReturnsStructuredUnsupported(t *testing.T) {
	_, err := New().ObserveEDL(context.Background(), device.Candidate{})
	var structured *derrors.Error
	if !errors.As(err, &structured) || structured.Code != derrors.CapabilityNotSupported {
		t.Fatalf("ObserveEDL() error=%v", err)
	}
}

func TestWindowsPortScorePrefersLowerCOMNumber(t *testing.T) {
	if windowsPortScore("COM12") >= windowsPortScore("COM3") {
		t.Fatal("lower COM number should be preferred")
	}
	if windowsPortScore("ttyUSB0") != 0 {
		t.Fatal("non-Windows port should not receive a COM score")
	}
}

func TestDiscoverReturnsATCandidateFromFirstResponsivePort(t *testing.T) {
	adapter := New()
	adapter.listPorts = func() ([]string, error) { return []string{"COM8", "COM3", "COM3"}, nil }
	var probed []string
	adapter.probeIMEI = func(port string, _ time.Duration) (string, error) {
		probed = append(probed, port)
		if port == "COM3" {
			return "867530900000001", nil
		}
		return "", errors.New("not an AT port")
	}

	candidates, err := adapter.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidate count=%d, want 1", len(candidates))
	}
	if candidates[0].ATPort != "COM3" || candidates[0].Identity.IMEI != "867530900000001" {
		t.Fatalf("candidate=%+v", candidates[0])
	}
	if len(probed) != 1 || probed[0] != "COM3" {
		t.Fatalf("probed ports=%v, want [COM3]", probed)
	}
}

func TestDiscoverCachesFailedPortProbes(t *testing.T) {
	adapter := New()
	adapter.listPorts = func() ([]string, error) { return []string{"COM4"}, nil }
	probes := 0
	adapter.probeIMEI = func(string, time.Duration) (string, error) {
		probes++
		return "", errors.New("no response")
	}

	if _, err := adapter.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if probes != 1 {
		t.Fatalf("probe count=%d, want 1 after failure cache", probes)
	}
}

func TestDiscoverHonorsCancellation(t *testing.T) {
	adapter := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	adapter.listPorts = func() ([]string, error) { return []string{"COM1"}, nil }
	if _, err := adapter.Discover(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v, want context.Canceled", err)
	}
}
