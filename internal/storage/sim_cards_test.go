package storage

import (
	"testing"
	"time"
)

func TestSimCardUpsertSeenAndList(t *testing.T) {
	store := openTestStore(t)

	if err := store.UpsertSimCardSeen(SimCardRecord{ICCID: "89860120010000000001", IMSI: "460001234", MSISDN: "+8613800000001"}); err != nil {
		t.Fatalf("upsert first: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	// 再次插卡：更新最近见到时间，IMSI/MSISDN 保留原值（空值不覆盖）。
	if err := store.UpsertSimCardSeen(SimCardRecord{ICCID: "89860120010000000001"}); err != nil {
		t.Fatalf("upsert second: %v", err)
	}

	records, err := store.ListSimCards()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("count = %d, want 1", len(records))
	}
	record := records[0]
	if record.ICCID != "89860120010000000001" || record.IMSI != "460001234" || record.MSISDN != "+8613800000001" {
		t.Fatalf("record = %+v", record)
	}
	if record.FirstSeen.IsZero() || record.LastSeen.Before(record.FirstSeen) {
		t.Fatalf("seen times invalid: first=%v last=%v", record.FirstSeen, record.LastSeen)
	}
}

func TestSimCardInsertRequiresICCID(t *testing.T) {
	store := openTestStore(t)
	if err := store.InsertSimCard(SimCardRecord{}); err == nil {
		t.Fatal("insert without iccid must fail")
	}
}

func TestSimCardMetaUpdateKeepsICCIDAndEmptyValues(t *testing.T) {
	store := openTestStore(t)
	if err := store.InsertSimCard(SimCardRecord{
		ICCID: "89860120010000000002", MSISDN: "+8613800000002", Name: "old", Notes: "old notes",
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// 空 msisdn 不覆盖已有号码；名称/备注更新。
	if err := store.UpdateSimCardMeta("89860120010000000002", "new name", "new notes", ""); err != nil {
		t.Fatalf("update meta: %v", err)
	}
	records, err := store.ListSimCards()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	record := records[0]
	if record.Name != "new name" || record.Notes != "new notes" || record.MSISDN != "+8613800000002" {
		t.Fatalf("record = %+v", record)
	}

	// 新号码覆盖。
	if err := store.UpdateSimCardMeta("89860120010000000002", "new name", "new notes", "+8613800000003"); err != nil {
		t.Fatalf("update msisdn: %v", err)
	}
	records, err = store.ListSimCards()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if records[0].MSISDN != "+8613800000003" {
		t.Fatalf("msisdn = %q, want +8613800000003", records[0].MSISDN)
	}
}

func TestSimCardDelete(t *testing.T) {
	store := openTestStore(t)
	if err := store.InsertSimCard(SimCardRecord{ICCID: "89860120010000000004"}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	deleted, err := store.DeleteSimCard("89860120010000000004")
	if err != nil || !deleted {
		t.Fatalf("delete = %v, %v", deleted, err)
	}
	deleted, err = store.DeleteSimCard("89860120010000000004")
	if err != nil || deleted {
		t.Fatalf("second delete = %v, %v, want false", deleted, err)
	}
}
