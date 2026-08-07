package backend

import (
	"context"
	"errors"
	"reflect"
	"testing"

	derrors "github.com/iniwex5/vohive/internal/domain/errors"
)

// stubESIMPort 记录调用并返回固定结果，用于验证 ATBackend 的 eSIM 委托。
type stubESIMPort struct {
	eid      string
	profiles []Profile
	err      error
	calls    []string
}

func (p *stubESIMPort) EID(context.Context) (string, error) {
	p.calls = append(p.calls, "eid")
	return p.eid, p.err
}

func (p *stubESIMPort) Profiles(context.Context) ([]Profile, error) {
	p.calls = append(p.calls, "profiles")
	return p.profiles, p.err
}

func (p *stubESIMPort) Download(context.Context, string, string, string, *ESIMDownloadOptions) error {
	p.calls = append(p.calls, "download")
	return p.err
}

func (p *stubESIMPort) Enable(context.Context, string) error {
	p.calls = append(p.calls, "enable")
	return p.err
}

func (p *stubESIMPort) Disable(context.Context, string) error {
	p.calls = append(p.calls, "disable")
	return p.err
}

func (p *stubESIMPort) Rename(context.Context, string, string) error {
	p.calls = append(p.calls, "rename")
	return p.err
}

func (p *stubESIMPort) Delete(context.Context, string) error {
	p.calls = append(p.calls, "delete")
	return p.err
}

func (p *stubESIMPort) ListNotifications(context.Context) ([]NotificationItem, error) {
	p.calls = append(p.calls, "list_notifications")
	return nil, p.err
}

func (p *stubESIMPort) ProcessNotification(context.Context, int64) error {
	p.calls = append(p.calls, "process_notification")
	return p.err
}

func (p *stubESIMPort) RemoveNotification(context.Context, int64) error {
	p.calls = append(p.calls, "remove_notification")
	return p.err
}

func TestATBackendESIMUnavailableWithoutPort(t *testing.T) {
	at := NewATBackend(nil)

	assertUnsupported := func(operation string, err error) {
		t.Helper()
		var target *derrors.Error
		if !errors.As(err, &target) || target.Code != derrors.CapabilityNotSupported {
			t.Fatalf("err = %v, want capability_not_supported", err)
		}
		if target.Details["operation"] != operation {
			t.Fatalf("operation = %v, want %s", target.Details["operation"], operation)
		}
	}

	_, err := at.EID(context.Background())
	assertUnsupported("esim_eid", err)

	_, err = at.Profiles(context.Background())
	assertUnsupported("esim_profiles", err)

	err = at.Download(context.Background(), "LPA:1$smdp.example.com", "", "", nil)
	assertUnsupported("esim_download", err)

	err = at.Enable(context.Background(), "8986012001000000000")
	assertUnsupported("esim_enable", err)

	err = at.Rename(context.Background(), "8986012001000000000", "work")
	assertUnsupported("esim_rename", err)

	err = at.Delete(context.Background(), "8986012001000000000")
	assertUnsupported("esim_delete", err)

	err = at.Disable(context.Background(), "8986012001000000000")
	assertUnsupported("esim_disable", err)

	_, err = at.ListNotifications(context.Background())
	assertUnsupported("esim_notifications", err)

	err = at.ProcessNotification(context.Background(), 1)
	assertUnsupported("esim_notifications", err)

	err = at.RemoveNotification(context.Background(), 1)
	assertUnsupported("esim_notifications", err)
}

func TestATBackendESIMForwardsToPort(t *testing.T) {
	port := &stubESIMPort{
		eid:      "89049032000000000000000000000000",
		profiles: []Profile{{ICCID: "8986012001000000000", State: "enabled"}},
	}
	at := NewATBackend(nil)
	at.SetESIMPort(port)

	eid, err := at.EID(context.Background())
	if err != nil || eid != port.eid {
		t.Fatalf("EID = %q, %v", eid, err)
	}

	profiles, err := at.Profiles(context.Background())
	if err != nil || len(profiles) != 1 || profiles[0].ICCID != "8986012001000000000" {
		t.Fatalf("Profiles = %+v, %v", profiles, err)
	}

	if err := at.Download(context.Background(), "LPA:1$smdp.example.com", "", "", nil); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if err := at.Enable(context.Background(), "8986012001000000000"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if err := at.Rename(context.Background(), "8986012001000000000", "work"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if err := at.Delete(context.Background(), "8986012001000000000"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := at.Disable(context.Background(), "8986012001000000000"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if _, err := at.ListNotifications(context.Background()); err != nil {
		t.Fatalf("ListNotifications: %v", err)
	}
	if err := at.ProcessNotification(context.Background(), 1); err != nil {
		t.Fatalf("ProcessNotification: %v", err)
	}
	if err := at.RemoveNotification(context.Background(), 1); err != nil {
		t.Fatalf("RemoveNotification: %v", err)
	}

	wantCalls := []string{
		"eid", "profiles", "download", "enable", "rename", "delete", "disable",
		"list_notifications", "process_notification", "remove_notification",
	}
	if !reflect.DeepEqual(port.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", port.calls, wantCalls)
	}
}
