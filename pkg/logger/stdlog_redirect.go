package logger

import (
	"bytes"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap/zapcore"
)

// stdLogRedirect routes the standard library log package through the zap
// pipeline so legacy log.Printf call sites (operation manager, HTTP server,
// main) emit the same console, file, and SSE output as the structured logger
// instead of raw stderr lines with a different format.
//
// Setup installs it with log.Lshortfile so every line carries its real call
// site, which the redirect parses back into zapcore.Entry.Caller.
type stdLogRedirect struct {
	core zapcore.Core

	mu      sync.Mutex
	pending bytes.Buffer
}

// linePattern matches the log.Lshortfile prefix "file.go:123: message".
var linePattern = regexp.MustCompile(`^(.*\.go):(\d+): (.*)$`)

func installStdLogRedirect(core zapcore.Core) {
	log.SetFlags(log.Lshortfile)
	log.SetOutput(&stdLogRedirect{core: core})
}

// Write implements io.Writer. The standard logger issues one Write per Print
// call, but the Writer contract allows partial writes, so incomplete lines
// are buffered until their newline arrives.
func (w *stdLogRedirect) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending.Write(p)
	for {
		data := w.pending.Bytes()
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			break
		}
		line := string(data[:idx])
		rest := data[idx+1:]
		w.pending.Reset()
		w.pending.Write(rest)
		w.routeLine(strings.TrimSuffix(line, "\r"))
	}
	return len(p), nil
}

func (w *stdLogRedirect) routeLine(line string) {
	entry := zapcore.Entry{Level: heuristicLevel(line), Time: time.Now()}
	entry.Message = classifyLine(line, &entry.Caller)
	if ce := w.core.Check(entry, nil); ce != nil {
		ce.Write()
	}
}

// classifyLine strips the Lshortfile "file.go:123: " prefix into Caller and
// returns the remaining message.
func classifyLine(line string, caller *zapcore.EntryCaller) string {
	m := linePattern.FindStringSubmatch(line)
	if m == nil {
		return line
	}
	lineNum, err := strconv.Atoi(m[2])
	if err != nil || lineNum <= 0 {
		return line
	}
	*caller = zapcore.EntryCaller{Defined: true, File: m[1], Line: lineNum}
	return m[3]
}

// heuristicLevel maps a standard-log message to a zap level. The legacy call
// sites carry no level, so keywords decide; the fallback is Info.
func heuristicLevel(line string) zapcore.Level {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "cancel"):
		return zapcore.WarnLevel
	case strings.Contains(lower, "error"),
		strings.Contains(lower, "failed"),
		strings.Contains(lower, "invalid"),
		strings.Contains(lower, "shutdown"),
		strings.Contains(lower, "fatal"):
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}
