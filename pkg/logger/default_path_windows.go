//go:build windows

package logger

import (
	"os"
	"path/filepath"
)

func platformLogDirectory(appName string) string {
	localAppData, err := os.UserCacheDir()
	if err != nil || localAppData == "" {
		return ""
	}
	return filepath.Join(localAppData, appName, "Logs")
}
