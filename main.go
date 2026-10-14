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
	dataDir := filepath.Join(configDir, "fyne-matrix")

	client, err := matrix.New(filepath.Join(dataDir, "matrix.db"))
	if err != nil {
		log.Fatal(err)
	}

	ui.NewApp(client).Run()
}
