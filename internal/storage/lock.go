package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Lock struct {
	path string
	file *os.File
}

func (l Layout) AcquireLock(key string, timeout time.Duration) (*Lock, error) {
	return AcquirePathLock(l.Locks, key, timeout)
}

func AcquirePathLock(root, key string, timeout time.Duration) (*Lock, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	hash := sha256.Sum256([]byte(key))
	path := filepath.Join(root, hex.EncodeToString(hash[:16])+".lock")
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
		if removed, checkErr := removeDeadLock(path); checkErr == nil && removed {
			continue
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

func removeDeadLock(path string) (bool, error) {
	before, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	pid := 0
	for _, line := range strings.Split(string(before), "\n") {
		if strings.HasPrefix(line, "pid=") {
			pid, _ = strconv.Atoi(strings.TrimPrefix(line, "pid="))
			break
		}
	}
	if pid <= 0 {
		return false, nil
	}
	alive, known := processAlive(pid)
	if !known || alive {
		return false, nil
	}
	after, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	afterInfo, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if string(before) != string(after) || info.Size() != afterInfo.Size() || !info.ModTime().Equal(afterInfo.ModTime()) {
		return false, nil
	}
	if err := os.Remove(path); err != nil {
		return false, err
	}
	return true, nil
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
