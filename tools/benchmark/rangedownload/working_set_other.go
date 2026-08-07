//go:build !windows

package main

func currentWorkingSetBytes() uint64 { return 0 }
