package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"

	"mychat/matrix"
	"mychat/ui"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func main() {
	if runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		log.Print("no graphical display found; in the VNC desktop run `export DISPLAY=:1` before starting Matrix")
		return
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	dataDir := filepath.Join(configDir, "gomuks-desktop")
	legacyDir := filepath.Join(configDir, "fyne-matrix")
	if _, err = os.Stat(dataDir); err != nil && legacyDir != dataDir {
		if _, legacyErr := os.Stat(legacyDir); legacyErr == nil {
			log.Printf("using existing Matrix data directory at %s", legacyDir)
			dataDir = legacyDir
		}
	}

	client, err := matrix.New(filepath.Join(dataDir, "matrix.db"))
	if err != nil {
		log.Fatal(err)
	}
	if cacheDir, cacheErr := os.UserCacheDir(); cacheErr == nil && cacheDir != "" {
		client.SetMediaCacheDir(filepath.Join(cacheDir, "gomuks-desktop", "media"))
	}

	ui.NewApp(client).Run()
}
