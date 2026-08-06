//go:build windows

package storage

import (
	"syscall"
	"unsafe"
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	openProcess        = kernel32.NewProc("OpenProcess")
	closeHandle        = kernel32.NewProc("CloseHandle")
	getExitCodeProcess = kernel32.NewProc("GetExitCodeProcess")
)

func processAlive(pid int) (bool, bool) {
	const (
		processQueryLimitedInformation = 0x1000
		stillActive                    = 259
	)
	handle, _, callErr := openProcess.Call(processQueryLimitedInformation, 0, uintptr(uint32(pid)))
	if handle == 0 {
		switch callErr {
		case syscall.Errno(87): // ERROR_INVALID_PARAMETER: the PID no longer exists.
			return false, true
		case syscall.Errno(5): // ERROR_ACCESS_DENIED: the process exists but is protected.
			return true, true
		default:
			return false, false
		}
	}
	defer closeHandle.Call(handle)
	var exitCode uint32
	ok, _, _ := getExitCodeProcess.Call(handle, uintptr(unsafe.Pointer(&exitCode)))
	if ok == 0 {
		return false, false
	}
	return exitCode == stillActive, true
}
