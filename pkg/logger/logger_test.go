package logger

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap/zapcore"
)

func TestDefaultFilenameIsAbsoluteAndAppScoped(t *testing.T) {
	filename := DefaultFilename("DJOneHub")
	if !filepath.IsAbs(filename) {
		t.Fatalf("DefaultFilename() = %q, want absolute path", filename)
	}
	if filepath.Base(filename) != "app.log" {
		t.Fatalf("DefaultFilename() = %q, want app.log filename", filename)
	}
	if !strings.Contains(filename, string(os.PathSeparator)+"DJOneHub"+string(os.PathSeparator)) {
		t.Fatalf("DefaultFilename() = %q, want app-scoped directory", filename)
	}
}

// TestSetupRoutesDeviceLayerLogsToConfiguredOutput verifies that after
// logger.Setup a device-layer log statement reaches the configured file
// output, replacing the Nop logger that would discard it.
func TestSetupRoutesDeviceLayerLogsToConfiguredOutput(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "app.log")
	Setup(LogConfig{Filename: path, Debug: true})

	Info("device layer log line", "device", "test-device")

	deadline := time.Now().Add(2 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), "device layer log line") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("log line never reached %s (err=%v)", path, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestSetupRedirectsStandardLogToFile verifies that after logger.Setup the
// standard library log package is routed through zap instead of raw stderr,
// so legacy log.Printf lines share the file output and level formatting.
func TestSetupRedirectsStandardLogToFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "app.log")
	Setup(LogConfig{Filename: path, Debug: true})

	log.Printf("legacy standard log line %d", 42)

	deadline := time.Now().Add(2 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), "legacy standard log line 42") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("standard log line never reached %s (err=%v)", path, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestClassifyLineParsesShortfileCaller(t *testing.T) {
	var caller zapcore.EntryCaller
	message := classifyLine("operation/manager.go:95: operation started id=1", &caller)
	if message != "operation started id=1" {
		t.Errorf("message = %q, want %q", message, "operation started id=1")
	}
	if !caller.Defined || caller.File != "operation/manager.go" || caller.Line != 95 {
		t.Errorf("caller = %+v, want operation/manager.go:95", caller)
	}

	caller = zapcore.EntryCaller{}
	message = classifyLine("no shortfile prefix here", &caller)
	if message != "no shortfile prefix here" {
		t.Errorf("message = %q, want the untouched line", message)
	}
	if caller.Defined {
		t.Errorf("caller = %+v, must stay undefined without a parseable prefix", caller)
	}
}

func TestHeuristicLevelMapsKeywords(t *testing.T) {
	tests := []struct {
		line string
		want zapcore.Level
	}{
		{line: "operation started id=1", want: zapcore.InfoLevel},
		{line: "DJOneHub listening on http://127.0.0.1:7576", want: zapcore.InfoLevel},
		{line: "http request method=GET path=/api/status status=200", want: zapcore.InfoLevel},
		{line: "operation cancelled id=1 error=context canceled", want: zapcore.WarnLevel},
		{line: "http error code=1 error=boom", want: zapcore.ErrorLevel},
		{line: "operation failed id=1 error=boom", want: zapcore.ErrorLevel},
		{line: "firmware EDL detection failed: nope", want: zapcore.ErrorLevel},
		{line: "shutdown: worker timeout", want: zapcore.ErrorLevel},
		{line: "listen address is invalid", want: zapcore.ErrorLevel},
	}
	for _, test := range tests {
		if got := heuristicLevel(test.line); got != test.want {
			t.Errorf("heuristicLevel(%q) = %v, want %v", test.line, got, test.want)
		}
	}
}
