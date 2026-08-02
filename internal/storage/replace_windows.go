//go:build windows

package storage

import (
	"errors"
	"os"
)

func replaceFile(source, target string) error {
	err := os.Rename(source, target)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return err
	}
	if removeErr := os.Remove(target); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return err
	}
	return os.Rename(source, target)
}
