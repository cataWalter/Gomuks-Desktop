//go:build linux

package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetLaunchAtStartupCreatesAndRemovesDesktopEntry(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	entry := filepath.Join(configDir, "autostart", "gomuks-desktop.desktop")
	if err := setLaunchAtStartup(true); err != nil {
		t.Fatalf("enable startup: %v", err)
	}
	content, err := os.ReadFile(entry)
	if err != nil {
		t.Fatalf("read startup entry: %v", err)
	}
	if !strings.Contains(string(content), "[Desktop Entry]") || !strings.Contains(string(content), "Exec=") {
		t.Fatalf("invalid startup entry: %s", content)
	}
	if err = setLaunchAtStartup(false); err != nil {
		t.Fatalf("disable startup: %v", err)
	}
	if _, err = os.Stat(entry); !os.IsNotExist(err) {
		t.Fatalf("startup entry still exists, stat error = %v", err)
	}
}
