# wowdata Phase 3 BLTE, TACT, and Data Cache Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the binary transport layer needed to retrieve, decrypt, decompress, and cache CASC payloads.

**Architecture:** Port low-level behavior from Node modules `blte-reader.js`, `blte-stream-reader.js`, `salsa20.js`, `tact-keys.js`, and `build-cache.js` into focused Go packages with fixture tests before higher-level DB2/file commands depend on them.

**Tech Stack:** Go, `compress/zlib`, binary readers, SHA/MD5-style hash comparisons as needed, JSON diagnostics.

---

## File Structure

- Create: `internal/crypto/salsa20.go`
- Create: `internal/crypto/salsa20_test.go`
- Create: `internal/tact/keys.go`
- Create: `internal/tact/keys_test.go`
- Create: `internal/blte/reader.go`
- Create: `internal/blte/reader_test.go`
- Create: `internal/casc/datacache.go`
- Create: `internal/casc/datacache_test.go`

### Task 1: Port Salsa20

**Files:**
- Create: `internal/crypto/salsa20.go`
- Create: `internal/crypto/salsa20_test.go`

- [ ] **Step 1: Extract Node vectors**

Create fixture vectors from `core/casc/salsa20.js` behavior using a tiny script and store them under `fixtures/golden/node/crypto/salsa20.json`.

- [ ] **Step 2: Write Go vector test**

Test must load the fixture and verify Go output bytes match Node output.

- [ ] **Step 3: Implement Salsa20**

Port key setup, nonce setup, block generation, counter increment, and process behavior.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/crypto`

Expected: PASS.

### Task 2: Port TACT Key Loading

**Files:**
- Create: `internal/tact/keys.go`
- Create: `internal/tact/keys_test.go`

- [ ] Parse key files compatible with Node `tact-keys.js`.
- [ ] Support lookup by key name.
- [ ] Return a typed missing-key error.
- [ ] Golden-test missing-key diagnostics against Node.

### Task 3: Port BLTE Reader

**Files:**
- Create: `internal/blte/reader.go`
- Create: `internal/blte/reader_test.go`

- [ ] Decode single-block uncompressed BLTE.
- [ ] Decode zlib-compressed BLTE.
- [ ] Decode encrypted BLTE after TACT keys are loaded.
- [ ] Validate block hashes and return typed integrity errors.
- [ ] Add tests for missing keys and bad hashes.

### Task 4: Add Data Cache

**Files:**
- Create: `internal/casc/datacache.go`
- Create: `internal/casc/datacache_test.go`

- [ ] Implement cache root, manifest path, file path, get, store, and integrity metadata.
- [ ] Preserve Node cache semantics where business-visible.
- [ ] Normalize Windows paths in JSON diagnostics.

### Task 5: Verify Phase 3

- [ ] Run: `go test ./internal/crypto ./internal/tact ./internal/blte ./internal/casc`
- [ ] Capture fixtures for encrypted and unencrypted BLTE samples.
- [ ] Commit: `git commit -m "feat: add blte tact and data cache foundation"`
