//go:build !windows

package storage

import "os"

func replaceFile(source, target string) error {
	return os.Rename(source, target)
}
