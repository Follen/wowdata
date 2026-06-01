package rawcache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type FetchFunc func(ctx context.Context, encodingKey string) ([]byte, error)

type Cache struct {
	root string
	mu   sync.Mutex
	in   map[string]*call
}

type call struct {
	done chan struct{}
	body []byte
	err  error
}

func New(root string) *Cache {
	return &Cache{
		root: root,
		in:   map[string]*call{},
	}
}

func (c *Cache) Get(ctx context.Context, region, product, buildKey, encodingKey, expectedSHA256 string, fetch FetchFunc) ([]byte, error) {
	if c == nil {
		return nil, errors.New("raw cache: nil cache")
	}
	if fetch == nil {
		return nil, errors.New("raw cache: nil fetch")
	}
	if expectedSHA256 == "" {
		return nil, errors.New("raw cache: empty expected sha256")
	}

	path, err := Path(c.root, region, product, buildKey, encodingKey)
	if err != nil {
		return nil, err
	}
	if err := rejectSymlinkPath(c.root, path); err != nil {
		return nil, err
	}
	if body, ok := readValid(path, expectedSHA256); ok {
		return body, nil
	}

	key := region + "\x00" + product + "\x00" + buildKey + "\x00" + encodingKey
	c.mu.Lock()
	if existing := c.in[key]; existing != nil {
		c.mu.Unlock()
		select {
		case <-existing.done:
			if existing.err != nil {
				return nil, existing.err
			}
			return existing.body, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	current := &call{done: make(chan struct{})}
	c.in[key] = current
	c.mu.Unlock()

	current.body, current.err = c.fetchAndStore(ctx, path, encodingKey, expectedSHA256, fetch)
	close(current.done)

	c.mu.Lock()
	delete(c.in, key)
	c.mu.Unlock()

	if current.err != nil {
		return nil, current.err
	}
	return current.body, nil
}

func (c *Cache) fetchAndStore(ctx context.Context, path, encodingKey, expectedSHA256 string, fetch FetchFunc) ([]byte, error) {
	body, err := fetch(ctx, encodingKey)
	if err != nil {
		return nil, err
	}
	if sha256Hex(body) != strings.ToLower(expectedSHA256) {
		return nil, errors.New("raw cache: fetched blob sha256 mismatch")
	}
	if err := writeAtomic(c.root, path, body); err != nil {
		return nil, err
	}
	return body, nil
}

func Path(root, region, product, buildKey, encodingKey string) (string, error) {
	if root == "" {
		return "", errors.New("raw cache: empty root")
	}
	parts := []string{region, product, buildKey, encodingKey}
	for _, part := range parts {
		if part == "" {
			return "", errors.New("raw cache: empty path part")
		}
		if filepath.IsAbs(part) || part == "." || part == ".." || strings.ContainsAny(part, `/\`) {
			return "", fmt.Errorf("raw cache: unsafe path part %q", part)
		}
	}

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	path := filepath.Join(rootAbs, "casc", region, product, buildKey, "data", encodingKey)
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("raw cache: path escapes root")
	}
	return pathAbs, nil
}

func readValid(path, expectedSHA256 string) ([]byte, bool) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	if sha256Hex(body) != strings.ToLower(expectedSHA256) {
		return nil, false
	}
	return body, true
}

func writeAtomic(root, path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err := rejectSymlinkPath(root, path); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()

	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func sha256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func rejectSymlinkPath(root, path string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("raw cache: path escapes root")
	}

	current := rootAbs
	if info, err := os.Lstat(current); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("raw cache: symlinked cache path %q", current)
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("raw cache: symlinked cache path %q", current)
		}
	}
	return nil
}
