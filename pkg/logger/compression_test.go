package logger

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompressLogFileCreatesGzipAndRemovesSource(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "app-2026-08-06.log")
	content := "line one\nline two\n"
	if err := os.WriteFile(source, []byte(content), 0640); err != nil {
		t.Fatal(err)
	}

	if err := compressLogFile(source); err != nil {
		t.Fatalf("compressLogFile: %v", err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source still exists or stat failed: %v", err)
	}

	compressed := source + ".gz"
	f, err := os.Open(compressed)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	reader, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := io.ReadAll(reader)
	closeErr := reader.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("read compressed log: read=%v close=%v", err, closeErr)
	}
	if string(decoded) != content {
		t.Fatalf("decoded content = %q, want %q", decoded, content)
	}
}

func TestPruneCompressedLogsByAge(t *testing.T) {
	directory := t.TempDir()
	filename := filepath.Join(directory, "app.log")
	old := filepath.Join(directory, "app-2026-08-01.log.gz")
	fresh := filepath.Join(directory, "app-2026-08-06.log.gz")
	for _, path := range []string{old, fresh} {
		if err := os.WriteFile(path, []byte("compressed"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(old, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	if err := pruneCompressedLogs(filename, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old compressed log was not removed: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh compressed log was removed: %v", err)
	}
	if !strings.HasSuffix(fresh, ".log.gz") {
		t.Fatalf("unexpected compressed filename %q", fresh)
	}
}
