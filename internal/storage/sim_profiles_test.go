package storage

import (
	"testing"
	"time"
)

func TestSimProfileObservationPreservesLocalMetadata(t *testing.T) {
	store := openTestStore(t)

	if err := store.InsertSimProfile(SimProfileRecord{
		ICCID: "89860120010000000001", Name: "Work", LocalPhone: "+8613800000000",
		Notes: "travel", Tags: "primary", ProfileType: SimProfileUnknown,
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := store.UpsertSimProfileObserved(SimProfileRecord{
		ICCID: "89860120010000000001", IMSI: "460001234", MSISDN: "+8613900000000",
		ProfileType: SimProfilePhysical,
	}); err != nil {
		t.Fatalf("observe: %v", err)
	}
	if err := store.UpsertSimProfileObserved(SimProfileRecord{
		ICCID: "89860120010000000001", ProfileType: SimProfileUnknown,
	}); err != nil {
		t.Fatalf("observe empty: %v", err)
	}

	records, err := store.ListSimProfiles()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("count = %d, want 1", len(records))
	}
	record := records[0]
	if record.ICCID != "89860120010000000001" || record.IMSI != "460001234" || record.MSISDN != "+8613900000000" {
		t.Fatalf("observed fields = %+v", record)
	}
	if record.Name != "Work" || record.LocalPhone != "+8613800000000" || record.Notes != "travel" || record.Tags != "primary" {
		t.Fatalf("local metadata was overwritten: %+v", record)
	}
	if record.ProfileType != SimProfilePhysical || record.FirstSeen.IsZero() || record.LastSeen.Before(record.FirstSeen) {
		t.Fatalf("profile identity = %+v", record)
	}
}

func TestSimProfileInsertRequiresValidICCID(t *testing.T) {
	store := openTestStore(t)
	for _, iccid := range []string{"", "12345678901234567890123"} {
		if err := store.InsertSimProfile(SimProfileRecord{ICCID: iccid}); err == nil {
			t.Fatalf("insert with ICCID %q must fail", iccid)
		}
	}
}

func TestSimProfileMetaUpdateKeepsObservedIdentity(t *testing.T) {
	store := openTestStore(t)
	if err := store.InsertSimProfile(SimProfileRecord{
		ICCID: "89860120010000000002", IMSI: "460001234", MSISDN: "+8613800000002",
		Name: "old", LocalPhone: "+8613800000099", Notes: "old notes", Tags: "old",
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	updated, err := store.UpdateSimProfileMeta(
		"89860120010000000002", "new name", "+8613800000003", "new notes", "travel",
	)
	if err != nil || !updated {
		t.Fatalf("update = %v, %v", updated, err)
	}
	records, err := store.ListSimProfiles()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	record := records[0]
	if record.Name != "new name" || record.LocalPhone != "+8613800000003" || record.Notes != "new notes" || record.Tags != "travel" {
		t.Fatalf("metadata = %+v", record)
	}
	if record.IMSI != "460001234" || record.MSISDN != "+8613800000002" {
		t.Fatalf("observed identity changed: %+v", record)
	}

	updated, err = store.UpdateSimProfileMeta("missing", "", "", "", "")
	if err != nil || updated {
		t.Fatalf("missing update = %v, %v, want false, nil", updated, err)
	}
}

func TestSimProfileDelete(t *testing.T) {
	store := openTestStore(t)
	if err := store.InsertSimProfile(SimProfileRecord{ICCID: "89860120010000000004"}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	deleted, err := store.DeleteSimProfile("89860120010000000004")
	if err != nil || !deleted {
		t.Fatalf("delete = %v, %v", deleted, err)
	}
	deleted, err = store.DeleteSimProfile("89860120010000000004")
	if err != nil || deleted {
		t.Fatalf("second delete = %v, %v, want false", deleted, err)
	}
}
