# wowdata Phase 2 CASC Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the Go foundation for source selection, products/builds, cache paths, CDN resolution, and CASC config parsing.

**Architecture:** Keep this phase below file payload decoding. It prepares local and remote source metadata, cache directories, version config parsing, CDN config parsing, and command plumbing for `warmup`, `casc products`, and `casc info`.

**Tech Stack:** Go, HTTP client, filesystem cache, JSON output, golden fixtures.

---

## File Structure

- Create: `internal/casc/types.go`
- Create: `internal/casc/cache.go`
- Create: `internal/casc/config.go`
- Create: `internal/casc/remote.go`
- Create: `internal/casc/local.go`
- Create: `internal/casc/resolver.go`
- Create: `internal/casc/source_test.go`
- Modify: `internal/app/commands.go`
- Modify: `internal/app/root.go`

### Task 1: Define Source and Build Types

**Files:**
- Create: `internal/casc/types.go`
- Test: `internal/casc/source_test.go`

- [ ] **Step 1: Write tests**

```go
package casc

import "testing"

func TestSourceKindValidation(t *testing.T) {
	for _, kind := range []SourceKind{SourceLocal, SourceRemote} {
		if !kind.Valid() {
			t.Fatalf("expected %s to be valid", kind)
		}
	}
	if SourceKind("bad").Valid() {
		t.Fatalf("bad source kind should be invalid")
	}
}
```

- [ ] **Step 2: Implement types**

```go
package casc

type SourceKind string

const (
	SourceLocal  SourceKind = "local"
	SourceRemote SourceKind = "remote"
)

func (k SourceKind) Valid() bool {
	return k == SourceLocal || k == SourceRemote
}

type Product struct {
	Label      string `json:"label"`
	BuildIndex int   `json:"buildIndex"`
}

type Context struct {
	Source   SourceKind `json:"source"`
	Path     string     `json:"path,omitempty"`
	Region   string     `json:"region"`
	Product  string     `json:"product,omitempty"`
	BuildKey string     `json:"buildKey,omitempty"`
	BuildName string    `json:"buildName,omitempty"`
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/casc`

Expected: PASS.

### Task 2: Implement Cache Paths

**Files:**
- Create: `internal/casc/cache.go`
- Modify: `internal/casc/source_test.go`

- [ ] **Step 1: Add test**

```go
func TestCachePaths(t *testing.T) {
	paths := NewCachePaths("user_data", "wow-123")
	if paths.Root != "user_data/casc/wow-123" {
		t.Fatalf("root = %q", paths.Root)
	}
}
```

- [ ] **Step 2: Implement cache paths**

```go
package casc

import "path/filepath"

type CachePaths struct {
	Root string `json:"root"`
}

func NewCachePaths(base string, buildKey string) CachePaths {
	return CachePaths{Root: filepath.ToSlash(filepath.Join(base, "casc", buildKey))}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/casc`

Expected: PASS.

### Task 3: Wire Warmup and CASC Info Stubs to Source Context

**Files:**
- Modify: `internal/app/commands.go`
- Modify: `internal/app/root_test.go`

- [ ] **Step 1: Add test for `casc info` stub envelope**

Append a test that runs `casc info` and expects `"not_implemented"` until real source context is connected.

- [ ] **Step 2: Implement handler injection seam**

Add handler registration in `internal/app` so later phases can replace `notImplementedHandler` with service-backed handlers.

- [ ] **Step 3: Run tests**

Run: `go test ./...`

Expected: PASS.

### Task 4: Verify Phase 2

- [ ] Run: `go test ./...`
- [ ] Run: `go run ./cmd/wowdata casc products --help`
- [ ] Update golden manifest with planned `casc/products` and `casc/info` fixtures.
- [ ] Commit: `git commit -m "feat: add casc foundation types"`
