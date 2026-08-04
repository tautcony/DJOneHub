package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// JSONStore is retained for legacy migration and focused compatibility tests.
// Runtime application state is stored in SQLiteStore.
type JSONStore struct { mu sync.Mutex; path string }

func NewJSONStore(path string) *JSONStore { return &JSONStore{path: path} }

func (s *JSONStore) Read(value any) error {
	s.mu.Lock(); defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) { return nil }
	if err != nil { return fmt.Errorf("read local store: %w", err) }
	if len(data) == 0 { return nil }
	if err := json.Unmarshal(data, value); err != nil { return fmt.Errorf("decode local store: %w", err) }
	return nil
}

func (s *JSONStore) Write(value any) error {
	s.mu.Lock(); defer s.mu.Unlock()
	data, err := json.MarshalIndent(value, "", "  "); if err != nil { return fmt.Errorf("encode local store: %w", err) }
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil { return fmt.Errorf("create local store directory: %w", err) }
	temporary := s.path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil { return fmt.Errorf("write local store: %w", err) }
	if err := os.Rename(temporary, s.path); err != nil { return fmt.Errorf("commit local store: %w", err) }
	return nil
}
