//go:build windows

package main

import (
	"runtime"
	"syscall"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")

func inspectSystem() (machineReport, limitReport) {
	physical, coreProbe := physicalCoreCount()
	total, available, memoryProbe := physicalMemory()
	logical := runtime.NumCPU()
	connections := logical * 16
	if connections < 64 {
		connections = 64
	}
	if connections > 256 {
		connections = 256
	}
	return machineReport{
			OS: runtime.GOOS, Arch: runtime.GOARCH, GoVersion: runtime.Version(), LogicalCores: logical,
			PhysicalCores: physical, CoreProbe: coreProbe, GOMAXPROCS: runtime.GOMAXPROCS(0),
			TotalMemory: total, AvailableMemory: available, MemoryProbe: memoryProbe,
		}, limitReport{
			OpenFiles:       1024,
			OpenFilesProbe:  "conservative scheduler budget; Windows has no process RLIMIT_NOFILE and handles are dynamically allocated",
			Connections:     connections,
			ConnectionProbe: "conservative calibration cap min(256,max(64,logicalCores*16)); scheduler must further constrain using BDP and errors",
		}
}

func physicalCoreCount() (int, string) {
	proc := kernel32.NewProc("GetLogicalProcessorInformationEx")
	var size uint32
	const relationProcessorCore = 0
	proc.Call(relationProcessorCore, 0, uintptr(unsafe.Pointer(&size)))
	if size == 0 {
		return 0, "GetLogicalProcessorInformationEx unavailable"
	}
	buf := make([]byte, size)
	r1, _, _ := proc.Call(relationProcessorCore, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r1 == 0 {
		return 0, "GetLogicalProcessorInformationEx failed"
	}
	count := 0
	for offset := uint32(0); offset+8 <= size; {
		relation := *(*uint32)(unsafe.Pointer(&buf[offset]))
		entrySize := *(*uint32)(unsafe.Pointer(&buf[offset+4]))
		if entrySize < 8 || offset+entrySize > size {
			return 0, "GetLogicalProcessorInformationEx returned malformed data"
		}
		if relation == relationProcessorCore {
			count++
		}
		offset += entrySize
	}
	return count, "GetLogicalProcessorInformationEx(RelationProcessorCore)"
}

func physicalMemory() (uint64, uint64, string) {
	type memoryStatusEx struct {
		Length, MemoryLoad                                 uint32
		TotalPhys, AvailPhys, TotalPageFile, AvailPageFile uint64
		TotalVirtual, AvailVirtual, AvailExtendedVirtual   uint64
	}
	status := memoryStatusEx{Length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	proc := kernel32.NewProc("GlobalMemoryStatusEx")
	r1, _, _ := proc.Call(uintptr(unsafe.Pointer(&status)))
	if r1 == 0 {
		return 0, 0, "GlobalMemoryStatusEx failed"
	}
	return status.TotalPhys, status.AvailPhys, "GlobalMemoryStatusEx"
}
