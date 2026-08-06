//go:build !windows

package storage

import (
	"errors"
	"syscall"
)

func processAlive(pid int) (bool, bool) {
	err := syscall.Kill(pid, 0)
	if err == nil || errors.Is(err, syscall.EPERM) {
		return true, true
	}
	if errors.Is(err, syscall.ESRCH) {
		return false, true
	}
	return false, false
}
