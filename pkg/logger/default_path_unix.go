//go:build !darwin && !windows

package logger

import (
	"os"
	"path/filepath"
)

func platformLogDirectory(appName string) string {
	if stateHome := os.Getenv("XDG_STATE_HOME"); stateHome != "" && filepath.IsAbs(stateHome) {
		return filepath.Join(stateHome, appName, "logs")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".local", "state", appName, "logs")
	}
	if cache, err := os.UserCacheDir(); err == nil && cache != "" {
		return filepath.Join(cache, appName, "logs")
	}
	return ""
}
