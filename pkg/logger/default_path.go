package logger

import (
	"os"
	"path/filepath"
	"strings"
)

// DefaultFilename returns an absolute, per-user log filename for the current
// platform. Setup creates the directory, so callers do not depend on it
// already existing.
func DefaultFilename(appName string) string {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		appName = "app"
	}
	directory := platformLogDirectory(appName)
	if directory == "" {
		directory = filepath.Join(os.TempDir(), appName, "logs")
	}
	return filepath.Join(directory, "app.log")
}
