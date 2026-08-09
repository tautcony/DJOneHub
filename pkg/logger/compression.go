package logger

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
)

type compressionHandler struct {
	filename string
	maxAge   time.Duration
}

func newCompressionHandler(filename string, maxAgeDays int) rotatelogs.Handler {
	return compressionHandler{
		filename: filename,
		maxAge:   time.Duration(maxAgeDays) * 24 * time.Hour,
	}
}

type rotationHandler struct {
	handlers        []rotatelogs.Handler
	initialComplete chan struct{}
	initialOnce     sync.Once
}

func (h *rotationHandler) Handle(event rotatelogs.Event) {
	for _, handler := range h.handlers {
		handler.Handle(event)
	}
	h.initialOnce.Do(func() { close(h.initialComplete) })
}

func newRotationHandler(filename string, maxAgeDays int, compress bool) *rotationHandler {
	handlers := make([]rotatelogs.Handler, 0, 2)
	if compress {
		handlers = append(handlers, newCompressionHandler(filename, maxAgeDays))
	}
	if handler := newPlatformRotationHandler(filename); handler != nil {
		handlers = append(handlers, handler)
	}
	if len(handlers) == 0 {
		return nil
	}
	return &rotationHandler{handlers: handlers, initialComplete: make(chan struct{})}
}

func (h compressionHandler) Handle(event rotatelogs.Event) {
	rotated, ok := event.(*rotatelogs.FileRotatedEvent)
	if !ok || rotated.PreviousFile() == "" {
		return
	}
	if err := compressLogFile(rotated.PreviousFile()); err != nil {
		// Keep the uncompressed source on failure. The warning itself goes
		// through the active logger and therefore remains observable.
		Warn("log compression failed", "file", rotated.PreviousFile(), "error", err)
		return
	}
	if err := pruneCompressedLogs(h.filename, h.maxAge); err != nil {
		Warn("compressed log cleanup failed", "error", err)
	}
}

func compressLogFile(source string) error {
	info, err := os.Stat(source)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	target := source + ".gz"
	if _, err := os.Stat(target); err == nil {
		return os.Remove(source)
	} else if !os.IsNotExist(err) {
		return err
	}

	input, err := os.Open(source)
	if err != nil {
		return err
	}
	inputClosed := false
	closeInput := func() error {
		if inputClosed {
			return nil
		}
		inputClosed = true
		return input.Close()
	}
	defer closeInput()

	temporary, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".tmp-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryName)
	}

	writer := gzip.NewWriter(temporary)
	writer.Name = filepath.Base(source)
	writer.ModTime = info.ModTime()
	if _, err = io.Copy(writer, input); err != nil {
		_ = writer.Close()
		cleanup()
		return err
	}
	if err = writer.Close(); err != nil {
		cleanup()
		return err
	}
	if err = closeInput(); err != nil {
		cleanup()
		return err
	}
	if err = temporary.Sync(); err != nil {
		cleanup()
		return err
	}
	if err = temporary.Close(); err != nil {
		_ = os.Remove(temporaryName)
		return err
	}
	if err = os.Chmod(temporaryName, info.Mode().Perm()); err != nil {
		_ = os.Remove(temporaryName)
		return err
	}
	if err = os.Rename(temporaryName, target); err != nil {
		_ = os.Remove(temporaryName)
		return err
	}
	if err = os.Chtimes(target, info.ModTime(), info.ModTime()); err != nil {
		return err
	}
	return os.Remove(source)
}

func pruneCompressedLogs(filename string, maxAge time.Duration) error {
	if maxAge <= 0 {
		return nil
	}
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	matches, err := filepath.Glob(base + "-*" + ext + ".gz")
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-maxAge)
	for _, path := range matches {
		info, statErr := os.Stat(path)
		if statErr != nil || info.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
