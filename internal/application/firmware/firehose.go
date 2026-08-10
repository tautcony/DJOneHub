package firmware

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/iniwex5/vohive/internal/domain/device"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
	"github.com/iniwex5/vohive/internal/transport"
)

const maxFirehoseOutput = 64 * 1024

// FirehoseCommandRunner is a host-safe seam for testing argument construction
// and bounded command output without starting a real EDL client.
type FirehoseCommandRunner func(context.Context, string, []string, string) ([]byte, []byte, error)

// CommandFirehose implements read-only Firehose operations. It invokes the
// configured executable directly and uses the same loader and target identity
// for read and reset.
type CommandFirehose struct {
	ClientPath string
	LoaderPath string
	// Prefix contains interpreter/tool arguments (for example, "uv run edl")
	// and is applied only when starting the real process. The test runner sees
	// the logical Firehose arguments without this prefix.
	Prefix    []string
	Dir       string
	PageSize  uint64
	BlockSize uint64
	Runner    FirehoseCommandRunner
	// Output receives bounded process output as it is produced. The callback
	// must not retain sensitive output beyond the operation log policy.
	Output func(string)
}

func (r *CommandFirehose) ReadNAND(ctx context.Context, candidate device.Candidate, req transport.FirehoseReadRequest) (transport.FirehoseReadResult, error) {
	if err := r.validate(req); err != nil {
		return transport.FirehoseReadResult{}, err
	}
	if strings.TrimSpace(req.ClientPath) == "" {
		req.ClientPath = r.ClientPath
	}
	if strings.TrimSpace(req.LoaderPath) == "" {
		req.LoaderPath = r.LoaderPath
	}
	output := filepath.Clean(req.OutputPath)
	if output == "." || output == "" {
		return transport.FirehoseReadResult{}, derrors.New(derrors.InvalidRequest, "backup output path is required", false, nil)
	}
	if info, err := os.Stat(output); err == nil && info.Mode().IsRegular() {
		return transport.FirehoseReadResult{}, derrors.New(derrors.InvalidRequest, "backup output already exists", false, nil)
	} else if err != nil && !os.IsNotExist(err) {
		return transport.FirehoseReadResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return transport.FirehoseReadResult{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(output), ".djonehub-nand-*.tmp")
	if err != nil {
		return transport.FirehoseReadResult{}, err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath)

	args := r.readArgs(req, tmpPath)
	stdout, stderr, runErr := r.run(ctx, req.ClientPath, args, candidate)
	if ctx.Err() != nil {
		return transport.FirehoseReadResult{}, ctx.Err()
	}
	if info, statErr := os.Stat(tmpPath); statErr != nil || info.Size() == 0 {
		if runErr != nil {
			return transport.FirehoseReadResult{}, fmt.Errorf("Firehose NAND read failed: %w: %s", runErr, boundedToolOutput(stdout, stderr))
		}
		return transport.FirehoseReadResult{}, errors.New("Firehose NAND read produced an empty image")
	}
	info, err := os.Stat(tmpPath)
	if err != nil {
		return transport.FirehoseReadResult{}, err
	}
	if r.PageSize > 0 && info.Size()%int64(r.PageSize) != 0 {
		return transport.FirehoseReadResult{}, fmt.Errorf("NAND image size %d is not aligned to page size %d", info.Size(), r.PageSize)
	}
	if err := validateMIBIB(tmpPath); err != nil {
		return transport.FirehoseReadResult{}, err
	}
	if runErr != nil {
		// A non-zero exit is accepted only when a complete, geometry-valid image
		// exists. Keep the diagnostic bounded and make the valid output explicit.
		if len(stdout)+len(stderr) > maxFirehoseOutput {
			return transport.FirehoseReadResult{}, fmt.Errorf("Firehose NAND read exited with %w and oversized output", runErr)
		}
	}
	if err := os.Rename(tmpPath, output); err != nil {
		return transport.FirehoseReadResult{}, err
	}
	return transport.FirehoseReadResult{OutputPath: output, Bytes: uint64(info.Size()), Valid: true}, nil
}

func validateMIBIB(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 16<<20))
	if err != nil {
		return err
	}
	magic := []byte{0xaa, 0x73, 0xee, 0x55, 0xdb, 0xbd, 0x5e, 0xe3}
	for offset := 0; offset+16 <= len(data); offset++ {
		if !bytes.Equal(data[offset:offset+8], magic) {
			continue
		}
		count := binary.LittleEndian.Uint32(data[offset+12 : offset+16])
		if count == 0 || count > 4096 {
			continue
		}
		end := uint64(offset) + 16 + uint64(count)*28
		if end <= uint64(len(data)) {
			return nil
		}
	}
	return errors.New("NAND image does not contain a valid MIBIB partition table")
}

func (r *CommandFirehose) Reset(ctx context.Context, candidate device.Candidate) error {
	if strings.TrimSpace(r.ClientPath) == "" {
		return derrors.New(derrors.InvalidRequest, "Firehose client is required for reset", false, nil)
	}
	args := []string{"reset", "--resetmode=reset", "--vid=0x05c6", "--pid=0x9008"}
	if strings.TrimSpace(r.LoaderPath) != "" {
		args = append(args, "--loader="+r.LoaderPath)
	}
	stdout, stderr, err := r.run(ctx, r.ClientPath, args, candidate)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("Firehose reset failed: %w: %s", err, boundedToolOutput(stdout, stderr))
	}
	return nil
}

func (r *CommandFirehose) validate(req transport.FirehoseReadRequest) error {
	client := strings.TrimSpace(req.ClientPath)
	if client == "" {
		client = strings.TrimSpace(r.ClientPath)
	}
	loader := strings.TrimSpace(req.LoaderPath)
	if loader == "" {
		loader = strings.TrimSpace(r.LoaderPath)
	}
	if client == "" {
		return derrors.New(derrors.InvalidRequest, "Firehose client is required", false, nil)
	}
	if info, err := os.Stat(client); err != nil || !info.Mode().IsRegular() {
		return derrors.New(derrors.InvalidRequest, "Firehose client is not a regular file", false, nil)
	} else if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return derrors.New(derrors.InvalidRequest, "Firehose client is not executable", false, nil)
	}
	if loader != "" {
		if info, err := os.Stat(loader); err != nil || !info.Mode().IsRegular() {
			return derrors.New(derrors.InvalidRequest, "Firehose loader is not a regular file", false, nil)
		}
	}
	if req.PageSize == 0 || req.BlockSize == 0 || req.BlockSize%req.PageSize != 0 {
		return derrors.New(derrors.InvalidRequest, "NAND geometry is invalid", false, nil)
	}
	return nil
}

func (r *CommandFirehose) readArgs(req transport.FirehoseReadRequest, output string) []string {
	loader := req.LoaderPath
	if strings.TrimSpace(loader) == "" {
		loader = r.LoaderPath
	}
	args := []string{"rf", output, "--memory=NAND", "--pagesperblock=" + fmt.Sprint(req.BlockSize/req.PageSize), "--sectorsize=" + fmt.Sprint(req.PageSize), "--vid=0x05c6", "--pid=0x9008"}
	if strings.TrimSpace(loader) != "" {
		args = append(args, "--loader="+loader)
	}
	if req.Start > 0 {
		args = append(args, "--startsector="+fmt.Sprint(req.Start/req.PageSize))
	}
	if req.Size > 0 {
		args = append(args, "--sectors="+fmt.Sprint(req.Size/req.PageSize))
	}
	return args
}

func (r *CommandFirehose) run(ctx context.Context, client string, args []string, candidate device.Candidate) ([]byte, []byte, error) {
	if r.Runner != nil {
		stdout, stderr, err := r.Runner(ctx, client, args, candidate.Identity.PhysicalLocation)
		if r.Output != nil {
			if len(stdout) > 0 {
				r.Output(string(stdout))
			}
			if len(stderr) > 0 {
				r.Output(string(stderr))
			}
		}
		return stdout, stderr, err
	}
	commandCtx := ctx
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) <= 0 {
		var cancel context.CancelFunc
		commandCtx, cancel = context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()
	}
	command := append([]string{client}, r.Prefix...)
	command = append(command, args...)
	cmd := exec.CommandContext(commandCtx, command[0], command[1:]...)
	cmd.Dir = r.Dir
	if cmd.Dir == "" {
		cmd.Dir = filepath.Dir(client)
	}
	var stdout, stderr boundedBuffer
	cmd.Stdout = io.MultiWriter(&stdout, outputWriter{write: r.Output})
	cmd.Stderr = io.MultiWriter(&stderr, outputWriter{write: r.Output})
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

type outputWriter struct{ write func(string) }

func (w outputWriter) Write(p []byte) (int, error) {
	if w.write != nil && len(p) > 0 {
		w.write(string(p))
	}
	return len(p), nil
}

type boundedBuffer struct{ data []byte }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	written := len(p)
	if len(b.data) < maxFirehoseOutput {
		remaining := maxFirehoseOutput - len(b.data)
		if len(p) > remaining {
			p = p[:remaining]
		}
		b.data = append(b.data, p...)
	}
	return written, nil
}
func (b *boundedBuffer) Bytes() []byte { return append([]byte(nil), b.data...) }

func boundedToolOutput(stdout, stderr []byte) string {
	combined := append(append([]byte(nil), stdout...), stderr...)
	if len(combined) > maxFirehoseOutput {
		combined = combined[:maxFirehoseOutput]
	}
	return strings.TrimSpace(cleanTerminalOutput(string(combined)))
}

var _ transport.FirehosePort = (*CommandFirehose)(nil)
var _ io.Writer = (*boundedBuffer)(nil)
