package storage

import "testing"

func TestJSONStoreRoundTrip(t *testing.T) {
	path := t.TempDir() + "/state.json"
	store := NewJSONStore(path)
	if err := store.Write(map[string]string{"device": "slot-1"}); err != nil { t.Fatal(err) }
	var value map[string]string
	if err := store.Read(&value); err != nil { t.Fatal(err) }
	if value["device"] != "slot-1" { t.Fatalf("value = %#v", value) }
}
