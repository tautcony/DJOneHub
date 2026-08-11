package storage

import (
	"path/filepath"
	"testing"
)

func newUpstreamProxyTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "upstream-proxy.sqlite3"))
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestUpstreamProxyCRUD(t *testing.T) {
	store := newUpstreamProxyTestStore(t)

	if err := store.UpsertUpstreamProxy(UpstreamProxy{ID: "proxy-a", Addr: "127.0.0.1:1080", Enabled: true}); err != nil {
		t.Fatalf("UpsertUpstreamProxy() error = %v", err)
	}
	if err := store.UpsertUpstreamProxy(UpstreamProxy{ID: "proxy-b", Addr: "127.0.0.1:1081"}); err != nil {
		t.Fatalf("UpsertUpstreamProxy() error = %v", err)
	}
	// 空 addr / 空 id 拒绝。
	if err := store.UpsertUpstreamProxy(UpstreamProxy{ID: "bad", Addr: " "}); err == nil {
		t.Fatal("UpsertUpstreamProxy() with empty addr should fail")
	}
	if err := store.UpsertUpstreamProxy(UpstreamProxy{Addr: "127.0.0.1:1082"}); err == nil {
		t.Fatal("UpsertUpstreamProxy() with empty id should fail")
	}

	list, err := store.ListUpstreamProxies()
	if err != nil {
		t.Fatalf("ListUpstreamProxies() error = %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListUpstreamProxies() = %d items, want 2", len(list))
	}

	got, err := store.GetUpstreamProxyByID("proxy-a")
	if err != nil {
		t.Fatalf("GetUpstreamProxyByID() error = %v", err)
	}
	if got == nil || !got.Enabled || got.Addr != "127.0.0.1:1080" {
		t.Fatalf("GetUpstreamProxyByID() = %+v, want enabled proxy-a", got)
	}
	missing, err := store.GetUpstreamProxyByID("nope")
	if err != nil || missing != nil {
		t.Fatalf("GetUpstreamProxyByID(missing) = %v, %v; want nil, nil", missing, err)
	}

	// 更新覆盖且保留 CreatedAt。
	createdAt := got.CreatedAt
	if err := store.UpsertUpstreamProxy(UpstreamProxy{ID: "proxy-a", Addr: "10.0.0.1:1080", Enabled: false}); err != nil {
		t.Fatalf("UpsertUpstreamProxy(update) error = %v", err)
	}
	updated, _ := store.GetUpstreamProxyByID("proxy-a")
	if updated == nil || updated.Enabled || updated.Addr != "10.0.0.1:1080" {
		t.Fatalf("GetUpstreamProxyByID(after update) = %+v", updated)
	}
	if !updated.CreatedAt.Equal(createdAt) {
		t.Fatalf("CreatedAt changed on update: %v → %v", createdAt, updated.CreatedAt)
	}

	// 删除（连带国家规则）。
	if err := store.UpsertUpstreamProxyCountryRule(UpstreamProxyCountryRule{CountryCode: "US", UpstreamProxyID: "proxy-b", Enabled: true}); err != nil {
		t.Fatalf("UpsertUpstreamProxyCountryRule() error = %v", err)
	}
	if err := store.DeleteUpstreamProxy("proxy-b"); err != nil {
		t.Fatalf("DeleteUpstreamProxy() error = %v", err)
	}
	rules, err := store.ListUpstreamProxyCountryRules()
	if err != nil {
		t.Fatalf("ListUpstreamProxyCountryRules() error = %v", err)
	}
	if len(rules) != 0 {
		t.Fatalf("country rules after proxy delete = %d, want 0", len(rules))
	}
}

func TestUpstreamProxyCountryRules(t *testing.T) {
	store := newUpstreamProxyTestStore(t)
	if err := store.UpsertUpstreamProxy(UpstreamProxy{ID: "p1", Addr: "127.0.0.1:1080", Enabled: true}); err != nil {
		t.Fatalf("UpsertUpstreamProxy() error = %v", err)
	}

	// 国家码归一化。
	if err := store.UpsertUpstreamProxyCountryRule(UpstreamProxyCountryRule{CountryCode: " us ", UpstreamProxyID: "p1", Enabled: true}); err != nil {
		t.Fatalf("UpsertUpstreamProxyCountryRule() error = %v", err)
	}
	got, err := store.GetCountryUpstreamProxy("US")
	if err != nil {
		t.Fatalf("GetCountryUpstreamProxy() error = %v", err)
	}
	if got == nil || got.ID != "p1" {
		t.Fatalf("GetCountryUpstreamProxy(US) = %+v, want p1", got)
	}

	// 规则未启用 → 不命中；代理未启用 → 不命中。
	if err := store.UpsertUpstreamProxyCountryRule(UpstreamProxyCountryRule{CountryCode: "CN", UpstreamProxyID: "p1", Enabled: false}); err != nil {
		t.Fatalf("UpsertUpstreamProxyCountryRule(CN) error = %v", err)
	}
	if got, _ := store.GetCountryUpstreamProxy("CN"); got != nil {
		t.Fatalf("GetCountryUpstreamProxy(CN) = %+v, want nil (rule disabled)", got)
	}
	if err := store.UpsertUpstreamProxyCountryRule(UpstreamProxyCountryRule{CountryCode: "JP", UpstreamProxyID: "p1", Enabled: true}); err != nil {
		t.Fatalf("UpsertUpstreamProxyCountryRule(JP) error = %v", err)
	}
	if err := store.UpsertUpstreamProxy(UpstreamProxy{ID: "p1", Addr: "127.0.0.1:1080", Enabled: false}); err != nil {
		t.Fatalf("UpsertUpstreamProxy(disabled) error = %v", err)
	}
	if got, _ := store.GetCountryUpstreamProxy("JP"); got != nil {
		t.Fatalf("GetCountryUpstreamProxy(JP) = %+v, want nil (proxy disabled)", got)
	}

	if err := store.DeleteUpstreamProxyCountryRule("US"); err != nil {
		t.Fatalf("DeleteUpstreamProxyCountryRule() error = %v", err)
	}
	if got, _ := store.GetCountryUpstreamProxy("US"); got != nil {
		t.Fatalf("GetCountryUpstreamProxy(US after delete) = %+v, want nil", got)
	}
}
