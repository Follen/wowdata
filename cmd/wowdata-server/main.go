package main

import (
	"fmt"
	"os"

	serverruntime "wowdata/internal/server/runtime"
)

func main() {
	rt := serverruntime.New()
	if rt.ServiceName == "" {
		fmt.Fprintln(os.Stderr, "server runtime name is empty")
		os.Exit(1)
	}
}
