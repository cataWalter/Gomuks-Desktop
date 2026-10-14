//go:build windows

package ui

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

func setLaunchAtStartup(enabled bool) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("open current-user startup registry key: %w", err)
	}
	defer key.Close()
	const valueName = "GomuksDesktop"
	if !enabled {
		if err = key.DeleteValue(valueName); err != nil && err != registry.ErrNotExist {
			return fmt.Errorf("remove startup registry value: %w", err)
		}
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find application executable: %w", err)
	}
	if err = key.SetStringValue(valueName, `"`+executable+`"`); err != nil {
		return fmt.Errorf("write startup registry value: %w", err)
	}
	return nil
}
