package main

import (
	"fmt"
	"os"

	"wowdata/internal/app"
)

func main() {
	cmd := app.NewRootCommand()
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
