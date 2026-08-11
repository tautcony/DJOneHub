package vowifi

import (
	"path/filepath"
	"testing"

	"github.com/iniwex5/vohive/internal/storage"
)

func newCardPolicyTestStore(t *testing.T) *CardPolicyStore {
	t.Helper()
	database, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "card-policy.sqlite3"))
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return NewCardPolicyStore(database)
}

func TestCardPolicyStoreAllowsByDefault(t *testing.T) {
	store := newCardPolicyTestStore(t)

	if !store.AllowsVoWiFi("89014103211118510720") {
		t.Fatal("AllowsVoWiFi() with no policy row = false, want true (默认允许)")
	}
	if _, ok := store.Get("89014103211118510720"); ok {
		t.Fatal("Get() with no policy row = found, want missing")
	}
	if len(store.List()) != 0 {
		t.Fatal("List() with no policy rows should be empty")
	}
	// 未注入 store 的 Service 同样视为允许。
	svc := &Service{}
	if !svc.cardPolicies.AllowsVoWiFi("x") {
		t.Fatal("nil store should allow")
	}
}

func TestCardPolicyStoreUpsertAndGate(t *testing.T) {
	store := newCardPolicyTestStore(t)

	if err := store.Upsert(CardPolicy{ICCID: "89014103211118510720", VoWiFiEnabled: false}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if store.AllowsVoWiFi("89014103211118510720") {
		t.Fatal("AllowsVoWiFi() with VoWiFiEnabled=false = true, want false")
	}

	pol, ok := store.Get("89014103211118510720")
	if !ok || pol.VoWiFiEnabled || pol.Source != "user" || pol.UpdatedAt.IsZero() {
		t.Fatalf("Get() = %+v (ok=%v), want user-sourced disabled policy", pol, ok)
	}

	if err := store.Upsert(CardPolicy{ICCID: "89014103211118510720", VoWiFiEnabled: true}); err != nil {
		t.Fatalf("Upsert(enable) error = %v", err)
	}
	if !store.AllowsVoWiFi("89014103211118510720") {
		t.Fatal("AllowsVoWiFi() after enable = false, want true")
	}

	// 多卡隔离。
	if err := store.Upsert(CardPolicy{ICCID: "89860031234567890123", VoWiFiEnabled: false}); err != nil {
		t.Fatalf("Upsert(second card) error = %v", err)
	}
	if !store.AllowsVoWiFi("89014103211118510720") {
		t.Fatal("second card policy should not affect first card")
	}
	if len(store.List()) != 2 {
		t.Fatalf("List() = %d, want 2", len(store.List()))
	}
}

func TestCanonicalICCID(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"89014103211118510720", "89014103211118510720"},
		{" 89014103211118510720F ", "89014103211118510720"},
		{"\"89860031234567890123\"", "89860031234567890123"},
		{"89014103211118510720FFff", "89014103211118510720"},
	}
	for _, c := range cases {
		if got := canonicalICCID(c.in); got != c.want {
			t.Errorf("canonicalICCID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCardPolicyStoreEmptyICCIDRejected(t *testing.T) {
	store := newCardPolicyTestStore(t)
	if err := store.Upsert(CardPolicy{ICCID: "  ", VoWiFiEnabled: false}); err == nil {
		t.Fatal("Upsert() with empty ICCID should fail")
	}
	if err := store.Upsert(CardPolicy{ICCID: "FFFF", VoWiFiEnabled: false}); err == nil {
		t.Fatal("Upsert() with ICCID canonicalizing to empty should fail")
	}
}

func TestServiceCardPolicyMethods(t *testing.T) {
	store := newCardPolicyTestStore(t)
	svc := &Service{cardPolicies: store}

	ok, err := svc.CardPolicySet("89014103211118510720", false)
	if err != nil || !ok {
		t.Fatalf("CardPolicySet() = %v, %v; want true, nil", ok, err)
	}
	pol, found := svc.CardPolicyGet("89014103211118510720")
	if !found || pol.VoWiFiEnabled {
		t.Fatalf("CardPolicyGet() = %+v (found=%v), want disabled policy", pol, found)
	}
	if list := svc.CardPolicyList(); len(list) != 1 {
		t.Fatalf("CardPolicyList() = %d, want 1", len(list))
	}

	// 未注入 store 的 Service 返回 false/nil，不报错。
	empty := &Service{}
	if ok, err := empty.CardPolicySet("x", true); ok || err != nil {
		t.Fatalf("CardPolicySet() without store = %v, %v; want false, nil", ok, err)
	}
}
