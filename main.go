package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"mychat/desktop"
	"mychat/matrix"
	"mychat/migration"
	"mychat/tui"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

//go:embed all:frontend/dist
var frontendAssets embed.FS

func main() {
	// Check for TUI subcommand
	if len(os.Args) > 1 && os.Args[1] == "tui" {
		if err := tui.Main(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	paths, err := migration.CurrentPaths()
	if err != nil {
		log.Fatal(err)
	}
	migrationReport, err := migration.MigrateLegacy(paths)
	if err != nil {
		log.Fatal(err)
	}
	if migrationReport.DatabaseMigrated {
		log.Printf("migrated Matrix data into %s", paths.DataDir)
	}
	if migrationReport.PreferencesWarning != nil {
		log.Printf("could not migrate legacy preferences: %v", migrationReport.PreferencesWarning)
	}

	client, err := matrix.New(filepath.Join(paths.DataDir, "matrix.db"))
	if err != nil {
		log.Fatal(err)
	}
	if cacheDir, cacheErr := os.UserCacheDir(); cacheErr == nil && cacheDir != "" {
		client.SetMediaCacheDir(filepath.Join(cacheDir, "gomuks-desktop", "media"))
	}

	if err = desktop.Run(client, frontendAssets, paths.PreferencesFile); err != nil {
		log.Fatal(err)
	}
}
