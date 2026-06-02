package rawcache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestValidBlobHitAvoidsRemoteFetch(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	key := "encoding-a"
	body := []byte("cached body")
	sum := testSHA256Hex(body)

	cachePath, err := Path(root, "us", "wow", "build-a", key)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(cachePath, body, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var calls atomic.Int64
	got, err := New(root).Get(ctx, "us", "wow", "build-a", key, sum, func(context.Context, string) ([]byte, error) {
		calls.Add(1)
		return []byte("remote body"), nil
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("Get() = %q, want %q", got, body)
	}
	if calls.Load() != 0 {
		t.Fatalf("remote calls = %d, want 0", calls.Load())
	}
}

func TestValidBlobHitAllowsNilRemoteFetch(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	key := "encoding-a"
	body := []byte("cached body")
	sum := testSHA256Hex(body)

	cachePath, err := Path(root, "us", "wow", "build-a", key)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(cachePath, body, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := New(root).Get(ctx, "us", "wow", "build-a", key, sum, nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("Get() = %q, want %q", got, body)
	}
}

func TestCorruptBlobRefetches(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	key := "encoding-a"
	remote := []byte("fresh body")
	sum := testSHA256Hex(remote)

	cachePath, err := Path(root, "us", "wow", "build-a", key)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte("corrupt body"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var calls atomic.Int64
	got, err := New(root).Get(ctx, "us", "wow", "build-a", key, sum, func(context.Context, string) ([]byte, error) {
		calls.Add(1)
		return remote, nil
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, remote) {
		t.Fatalf("Get() = %q, want %q", got, remote)
	}
	if calls.Load() != 1 {
		t.Fatalf("remote calls = %d, want 1", calls.Load())
	}

	onDisk, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("ReadFile cache: %v", err)
	}
	if !bytes.Equal(onDisk, remote) {
		t.Fatalf("cache file = %q, want %q", onDisk, remote)
	}
}

func TestSameEncodingKeySingleflights(t *testing.T) {
	ctx := context.Background()
	cache := New(t.TempDir())
	body := []byte("shared body")
	sum := testSHA256Hex(body)

	var calls atomic.Int64
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	results := make(chan []byte, 16)

	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got, err := cache.Get(ctx, "us", "wow", "build-a", "encoding-a", sum, func(context.Context, string) ([]byte, error) {
				calls.Add(1)
				return body, nil
			})
			if err != nil {
				errs <- err
				return
			}
			results <- got
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	close(results)

	for err := range errs {
		t.Fatalf("Get: %v", err)
	}
	for got := range results {
		if !bytes.Equal(got, body) {
			t.Fatalf("Get() = %q, want %q", got, body)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("remote calls = %d, want 1", calls.Load())
	}
}

func TestDifferentEncodingKeysRunConcurrently(t *testing.T) {
	ctx := context.Background()
	cache := New(t.TempDir())
	release := make(chan struct{})
	started := make(chan string, 2)
	bodyA := []byte("body a")
	bodyB := []byte("body b")
	sums := map[string]string{
		"encoding-a": testSHA256Hex(bodyA),
		"encoding-b": testSHA256Hex(bodyB),
	}
	bodies := map[string][]byte{
		"encoding-a": bodyA,
		"encoding-b": bodyB,
	}

	fetch := func(ctx context.Context, key string) ([]byte, error) {
		started <- key
		select {
		case <-release:
			return bodies[key], nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, key := range []string{"encoding-a", "encoding-b"} {
		key := key
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := cache.Get(ctx, "us", "wow", "build-a", key, sums[key], fetch)
			if err != nil {
				errs <- err
				return
			}
			if !bytes.Equal(got, bodies[key]) {
				errs <- errors.New("unexpected body")
			}
		}()
	}

	seen := map[string]bool{}
	for len(seen) < 2 {
		key := <-started
		seen[key] = true
	}
	close(release)
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("Get: %v", err)
	}
}

func TestCachePrunesOldCASCFilesWhenDiskLimitExceeded(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	oldPath, err := Path(root, "cn", "wow", "old-build", "old-encoding")
	if err != nil {
		t.Fatalf("old path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(oldPath), 0755); err != nil {
		t.Fatalf("mkdir old path: %v", err)
	}
	if err := os.WriteFile(oldPath, bytes.Repeat([]byte("o"), 70), 0644); err != nil {
		t.Fatalf("write old cache file: %v", err)
	}
	oldTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes old cache file: %v", err)
	}

	body := bytes.Repeat([]byte("n"), 50)
	cache := NewWithLimits(root, 100, 50)
	got, err := cache.Get(ctx, "cn", "wow", "new-build", "new-encoding", testSHA256Hex(body), func(context.Context, string) ([]byte, error) {
		return body, nil
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("Get() = %q, want new body", got)
	}
	if _, err := os.Stat(oldPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old cache file stat error = %v, want pruned", err)
	}
	newPath, err := Path(root, "cn", "wow", "new-build", "new-encoding")
	if err != nil {
		t.Fatalf("new path: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("new cache file should remain after prune: %v", err)
	}
}

func TestSymlinkedCacheAncestorCannotEscapeRoot(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	outside := t.TempDir()
	cascLink := filepath.Join(root, "casc")
	if err := os.Symlink(outside, cascLink); err != nil {
		t.Skipf("os.Symlink unavailable on this platform or filesystem: %v", err)
	}

	body := []byte("remote body")
	var calls atomic.Int64
	_, err := New(root).Get(ctx, "us", "wow", "build-a", "encoding-a", testSHA256Hex(body), func(context.Context, string) ([]byte, error) {
		calls.Add(1)
		return body, nil
	})
	if err == nil {
		t.Fatal("Get with symlinked cache ancestor succeeded, want error")
	}
	if calls.Load() != 0 {
		t.Fatalf("remote calls = %d, want 0", calls.Load())
	}

	outsidePath := filepath.Join(outside, "us", "wow", "build-a", "data", "encoding-a")
	if _, err := os.Stat(outsidePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside cache file stat error = %v, want not exist", err)
	}
}

func TestEmptyExpectedSHA256RejectedBeforeFetch(t *testing.T) {
	ctx := context.Background()

	var calls atomic.Int64
	_, err := New(t.TempDir()).Get(ctx, "us", "wow", "build-a", "encoding-a", "", func(context.Context, string) ([]byte, error) {
		calls.Add(1)
		return []byte("remote body"), nil
	})
	if err == nil {
		t.Fatal("Get with empty expectedSHA256 succeeded, want error")
	}
	if calls.Load() != 0 {
		t.Fatalf("remote calls = %d, want 0", calls.Load())
	}
}

func testSHA256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
