package main

import (
	"log"
	"os"
	"path/filepath"

	"mychat/matrix"
	"mychat/ui"
)

func main() {
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
