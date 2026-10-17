package main

import (
	"fmt"
	"os"

	"mychat/tui"
)

func main() {
	if err := tui.Main(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
