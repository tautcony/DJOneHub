//go:build darwin

package startup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDarwinManagerWritesLaunchAgent(t *testing.T) {
	home := t.TempDir()
	executable := "/Applications/DJOneHub.app/Contents/MacOS/djonehub"
	manager := darwinManager{executable: executable, home: home}
	if !manager.isAppBundle() {
		t.Fatal("expected app bundle executable to be supported")
	}
	if err := manager.enable(); err != nil {
		t.Fatalf("enable: %v", err)
	}
	data, err := os.ReadFile(manager.plistPath())
	if err != nil {
		t.Fatalf("read plist: %v", err)
	}
	content := string(data)
	for _, expected := range []string{
		"<key>Label</key><string>com.jamie.djonehub</string>",
		"<key>RunAtLoad</key><true></true>",
		"<key>KeepAlive</key><true></true>",
		executable,
		"127.0.0.1:7575",
	} {
		if !strings.Contains(content, expected) {
			t.Errorf("plist does not contain %q: %s", expected, content)
		}
	}
	if err := manager.disable(); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist")); !os.IsNotExist(err) {
		t.Fatalf("plist still exists, stat error: %v", err)
	}
}
