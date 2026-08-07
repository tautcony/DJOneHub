package simprofiles

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/iniwex5/vohive/internal/backend"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/storage"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	store, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "sim-profiles.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return NewService(store)
}

func TestServiceOwnsUnifiedMetadata(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	if err := service.Create(ctx, Profile{
		ICCID: "8901000000000000001", IMSI: "460001234", MSISDN: "+8613900000000",
		Name: "Work", LocalPhone: "+8613800000000", Notes: "travel", Tags: "primary",
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.ObserveESIM([]backend.Profile{{
		ICCID: "8901000000000000001", Phone: "+8613900000001",
	}, {
		ICCID: "8901000000000000002",
	}}); err != nil {
		t.Fatal(err)
	}
	profiles, err := service.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 {
		t.Fatalf("profiles = %+v", profiles)
	}
	var first Profile
	for _, profile := range profiles {
		if profile.ICCID == "8901000000000000001" {
			first = profile
		}
	}
	if first.Name != "Work" || first.LocalPhone != "+8613800000000" || first.MSISDN != "+8613900000001" || first.ProfileType != storage.SimProfileESIM {
		t.Fatalf("observed profile = %+v", first)
	}
}

func TestServiceUpdateMissingIsNotFound(t *testing.T) {
	service := newTestService(t)
	err := service.UpdateMeta(context.Background(), "8901000000000000001", "", "", "", "")
	var target *derrors.Error
	if !errors.As(err, &target) || target.Code != derrors.NotFound {
		t.Fatalf("error = %v, want not_found", err)
	}
}
