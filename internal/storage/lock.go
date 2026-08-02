package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Lock struct {
	path string
	file *os.File
}

func (l Layout) AcquireLock(key string, timeout time.Duration) (*Lock, error) {
	if err := os.MkdirAll(l.Locks, 0o755); err != nil {
		return nil, err
	}
	hash := sha256.Sum256([]byte(key))
	path := filepath.Join(l.Locks, hex.EncodeToString(hash[:16])+".lock")
	deadline := time.Now().Add(timeout)
	for {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			_, _ = fmt.Fprintf(file, "pid=%d\ncreated=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
			_ = file.Sync()
			return &Lock{path: path, file: file}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > 30*time.Minute {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout waiting for cache lock %s", filepath.Base(path))
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (l *Lock) Release() error {
	if l == nil {
		return nil
	}
	if l.file != nil {
		_ = l.file.Close()
	}
	return os.Remove(l.path)
}
