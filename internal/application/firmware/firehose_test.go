package firmware

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iniwex5/vohive/internal/domain/device"
	"github.com/iniwex5/vohive/internal/transport"
)

func TestCommandFirehoseReadBuildsBoundedArgsAndPublishesAtomically(t *testing.T) {
	dir := t.TempDir()
	client := filepath.Join(dir, "firehose")
	loader := filepath.Join(dir, "loader.bin")
	if err := os.WriteFile(client, []byte("client"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(loader, []byte("loader"), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "backup.bin")
	var gotArgs []string
	runner := func(_ context.Context, gotClient string, args []string, _ string) ([]byte, []byte, error) {
		if gotClient != client {
			t.Fatalf("client=%s", gotClient)
		}
		gotArgs = append([]string(nil), args...)
		path := args[1]
		image := make([]byte, 4096)
		copy(image, []byte{0xaa, 0x73, 0xee, 0x55, 0xdb, 0xbd, 0x5e, 0xe3})
		binary.LittleEndian.PutUint32(image[12:16], 1)
		if err := os.WriteFile(path, image, 0o600); err != nil {
			t.Fatal(err)
		}
		return []byte("50%\033[2K\n"), []byte(strings.Repeat("x", maxFirehoseOutput*2)), nil
	}
	var streamed strings.Builder
	r := &CommandFirehose{ClientPath: client, LoaderPath: loader, Runner: runner, Output: func(value string) {
		streamed.WriteString(value)
	}}
	result, err := r.ReadNAND(context.Background(), device.Candidate{}, transport.FirehoseReadRequest{OutputPath: output, PageSize: 2048, BlockSize: 131072})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid || result.Bytes != 4096 {
		t.Fatalf("result=%+v", result)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatal(err)
	}
	if len(gotArgs) < 2 || gotArgs[0] != "rf" || !strings.Contains(strings.Join(gotArgs, " "), "--loader="+loader) {
		t.Fatalf("args=%v", gotArgs)
	}
	if !strings.Contains(streamed.String(), "50%") || streamed.Len() <= maxFirehoseOutput {
		t.Fatalf("streamed output length=%d; expected stdout and stderr", streamed.Len())
	}
}

func TestFirehoseOutputReporterPublishesLogsAndScaledProgress(t *testing.T) {
	var logs strings.Builder
	progress := 0
	message := ""
	reporter := firehoseOutputReporter(func(value int, current string) {
		progress = value
		message = current
	}, func(value string) {
		logs.WriteString(value)
	})
	reporter("Reading sector 2048 of 4096 (5")
	reporter("0.0%)\r\n")
	if logs.String() != "Reading sector 2048 of 4096 (50.0%)\r\n" {
		t.Fatalf("logs=%q", logs.String())
	}
	if progress != 50 || message != "Reading sector 2048 of 4096 (50.0%)" {
		t.Fatalf("progress=%d message=%q", progress, message)
	}
}

func TestCommandFirehoseRejectsInvalidGeometry(t *testing.T) {
	r := &CommandFirehose{ClientPath: "/tmp/client", LoaderPath: "/tmp/loader"}
	_, err := r.ReadNAND(context.Background(), device.Candidate{}, transport.FirehoseReadRequest{OutputPath: "/tmp/out", PageSize: 0, BlockSize: 1})
	if err == nil {
		t.Fatal("expected geometry validation error")
	}
}

func TestCommandFirehoseAllowsDefaultLoader(t *testing.T) {
	dir := t.TempDir()
	client := filepath.Join(dir, "firehose")
	if err := os.WriteFile(client, []byte("client"), 0o755); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "backup.bin")
	var gotArgs []string
	runner := func(_ context.Context, _ string, args []string, _ string) ([]byte, []byte, error) {
		gotArgs = append([]string(nil), args...)
		image := make([]byte, 4096)
		copy(image, []byte{0xaa, 0x73, 0xee, 0x55, 0xdb, 0xbd, 0x5e, 0xe3})
		binary.LittleEndian.PutUint32(image[12:16], 1)
		if err := os.WriteFile(args[1], image, 0o600); err != nil {
			t.Fatal(err)
		}
		return nil, nil, nil
	}
	r := &CommandFirehose{ClientPath: client, Runner: runner}
	if _, err := r.ReadNAND(context.Background(), device.Candidate{}, transport.FirehoseReadRequest{OutputPath: output, PageSize: 2048, BlockSize: 131072}); err != nil {
		t.Fatal(err)
	}
	for _, arg := range gotArgs {
		if strings.HasPrefix(arg, "--loader=") {
			t.Fatalf("default-loader args=%v", gotArgs)
		}
	}
}

func TestCommandFirehoseResetAllowsDefaultLoader(t *testing.T) {
	dir := t.TempDir()
	client := filepath.Join(dir, "firehose")
	if err := os.WriteFile(client, []byte("client"), 0o755); err != nil {
		t.Fatal(err)
	}
	var gotArgs []string
	r := &CommandFirehose{ClientPath: client, Runner: func(_ context.Context, _ string, args []string, _ string) ([]byte, []byte, error) {
		gotArgs = append([]string(nil), args...)
		return nil, nil, nil
	}}
	if err := r.Reset(context.Background(), device.Candidate{}); err != nil {
		t.Fatal(err)
	}
	for _, arg := range gotArgs {
		if strings.HasPrefix(arg, "--loader=") {
			t.Fatalf("default-loader reset args=%v", gotArgs)
		}
	}
}
