//go:build darwin

package ui

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
)

func setLaunchAtStartup(enabled bool) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("find user home directory: %w", err)
	}
	agentPath := filepath.Join(homeDir, "Library", "LaunchAgents", "fi.mau.gomuks-desktop.plist")
	if !enabled {
		if err = os.Remove(agentPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove launch agent: %w", err)
		}
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find application executable: %w", err)
	}
	if err = os.MkdirAll(filepath.Dir(agentPath), 0o700); err != nil {
		return fmt.Errorf("create launch agent directory: %w", err)
	}
	content := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>fi.mau.gomuks-desktop</string>
<key>ProgramArguments</key><array><string>` + xmlEscape(executable) + `</string></array>
<key>RunAtLoad</key><true/>
</dict></plist>
`
	if err = os.WriteFile(agentPath, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write launch agent: %w", err)
	}
	return nil
}

func xmlEscape(value string) string {
	return html.EscapeString(value)
}
