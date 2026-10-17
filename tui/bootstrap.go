package tui

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"mychat/matrix"
	"mychat/migration"
)

// Main runs the terminal client; diagnostics go to tui.log so they do not draw over the screen.
func Main() error {
	paths, err := migration.CurrentPaths()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(paths.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	logFile, err := os.OpenFile(filepath.Join(paths.DataDir, "tui.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open TUI log: %w", err)
	}
	defer logFile.Close()
	log.SetOutput(logFile)

	migrationReport, err := migration.MigrateLegacy(paths)
	if err != nil {
		return err
	}
	if migrationReport.DatabaseMigrated {
		log.Printf("migrated Matrix data into %s", paths.DataDir)
	}
	if migrationReport.PreferencesWarning != nil {
		log.Printf("could not migrate legacy preferences: %v", migrationReport.PreferencesWarning)
	}

	client, err := matrix.NewWithLogOutput(filepath.Join(paths.DataDir, "matrix.db"), logFile)
	if err != nil {
		return err
	}
	if cacheDir, cacheErr := os.UserCacheDir(); cacheErr == nil && cacheDir != "" {
		client.SetMediaCacheDir(filepath.Join(cacheDir, "gomuks-desktop", "media"))
	}
	return Run(client)
}
