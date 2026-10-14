//go:build linux

package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func setLaunchAtStartup(enabled bool) error {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("find startup configuration directory: %w", err)
	}
	entryPath := filepath.Join(configDir, "autostart", "gomuks-desktop.desktop")
	if !enabled {
		if err = os.Remove(entryPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove desktop startup entry: %w", err)
		}
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find application executable: %w", err)
	}
	if err = os.MkdirAll(filepath.Dir(entryPath), 0o700); err != nil {
		return fmt.Errorf("create desktop startup directory: %w", err)
	}
	execArg := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "%", "%%").Replace(executable)
	content := "[Desktop Entry]\nType=Application\nName=Gomuks Desktop\nExec=\"" + execArg + "\"\nX-GNOME-Autostart-enabled=true\n"
	if err = os.WriteFile(entryPath, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write desktop startup entry: %w", err)
	}
	return nil
}
