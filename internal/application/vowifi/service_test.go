package vowifi

import (
	"context"
	"testing"

	"github.com/iniwex5/vohive/internal/application/device"
	"github.com/iniwex5/vohive/internal/application/operation"
	"github.com/iniwex5/vohive/internal/runtime"
	"github.com/iniwex5/vowifi-go/runtimehost"
)

func TestStatusExposesStartupFailure(t *testing.T) {
	rt := &runtime.Runtime{}
	service := NewService(device.NewService(rt), operation.NewManager(rt.Events()), rt)
	claim := service.manager.BeginStart(voWiFiDeviceID)
	service.manager.FailStart(voWiFiDeviceID, claim.Epoch, runtimehost.State{}, context.Canceled)

	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status["state"] != "failed" || status["last_error"] != context.Canceled.Error() || status["reason"] != context.Canceled.Error() {
		t.Fatalf("status = %#v", status)
	}
}
