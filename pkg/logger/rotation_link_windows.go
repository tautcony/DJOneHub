//go:build windows

package logger

import (
	"os"
	"path/filepath"
	"sync"

	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
)

var windowsCurrentLogMu sync.Mutex

// Windows symlink creation requires SeCreateSymbolicLinkPrivilege. A hard
// link keeps the stable app.log entry without requiring elevated privileges.
func currentLogOptions(string) []rotatelogs.Option {
	return nil
}

func newPlatformRotationHandler(filename string) rotatelogs.Handler {
	return windowsCurrentLogHandler{filename: filename}
}

type windowsCurrentLogHandler struct {
	filename string
}

func (h windowsCurrentLogHandler) Handle(event rotatelogs.Event) {
	rotated, ok := event.(*rotatelogs.FileRotatedEvent)
	if !ok || rotated.CurrentFile() == "" {
		return
	}

	if err := updateWindowsCurrentLog(h.filename, rotated.CurrentFile()); err != nil {
		Warn("windows current log link unavailable", "file", h.filename, "target", rotated.CurrentFile(), "error", err)
	}
}

func updateWindowsCurrentLog(filename, target string) error {
	windowsCurrentLogMu.Lock()
	defer windowsCurrentLogMu.Unlock()

	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return err
	}

	temporary := filename + ".tmp-link"
	_ = os.Remove(temporary)
	if err := os.Link(target, temporary); err != nil {
		return err
	}
	if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, filename); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}
