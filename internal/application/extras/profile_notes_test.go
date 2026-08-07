package extras

import (
	"encoding/json"
	"testing"
)

type notesTestStore struct{ value []byte }

func (s *notesTestStore) Read(target any) error {
	if len(s.value) == 0 {
		return nil
	}
	return json.Unmarshal(s.value, target)
}

func (s *notesTestStore) Write(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	s.value = encoded
	return nil
}

func TestProfileNotesReadWriteAndValidation(t *testing.T) {
	store := &notesTestStore{}
	service := NewService(nil, nil, nil, store)
	if err := service.SaveNote(nil, "8901000000000000000", ProfileNote{Label: "Work", Phone: "+1", Tags: "travel"}); err != nil {
		t.Fatalf("save note: %v", err)
	}
	notes, err := service.Notes(nil)
	if err != nil {
		t.Fatalf("read notes: %v", err)
	}
	if notes["8901000000000000000"].Label != "Work" {
		t.Fatalf("notes = %#v", notes)
	}
	if err := service.SaveNote(nil, "8901000000000000000", ProfileNote{}); err != nil {
		t.Fatalf("clear note: %v", err)
	}
	notes, err = service.Notes(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Fatalf("cleared notes = %#v", notes)
	}
	if err := service.SaveNote(nil, "", ProfileNote{}); err == nil {
		t.Fatal("missing ICCID must fail")
	}
}
