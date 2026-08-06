//go:build !windows

package main

import "runtime"

func inspectSystem() (machineReport, limitReport) {
	logical := runtime.NumCPU()
	connections := logical * 16
	if connections < 64 {
		connections = 64
	}
	if connections > 256 {
		connections = 256
	}
	return machineReport{OS: runtime.GOOS, Arch: runtime.GOARCH, GoVersion: runtime.Version(), LogicalCores: logical, CoreProbe: "not implemented on this OS", GOMAXPROCS: runtime.GOMAXPROCS(0), MemoryProbe: "not implemented on this OS"},
		limitReport{OpenFiles: 1024, OpenFilesProbe: "conservative scheduler budget; OS rlimit probe not implemented", Connections: connections, ConnectionProbe: "conservative calibration cap"}
}
