//go:build darwin

package logger

import (
	"os"
	"path/filepath"
)

func platformLogDirectory(appName string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, "Library", "Logs", appName)
}
