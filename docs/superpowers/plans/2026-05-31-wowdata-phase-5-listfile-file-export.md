# wowdata Phase 5 Listfile, File Access, and Export Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement listfile loading/search and CASC file lookup/get/exists/encoding/export commands.

**Architecture:** Build on Phase 2 and Phase 3 CASC foundations. Keep listfile behavior compatible with Node, including unknown model/texture enrichment and cache refresh semantics where business-visible.

**Tech Stack:** Go, CSV/text parsing, filesystem output, CASC payload retrieval, golden fixtures.

---

## File Structure

- Create: `internal/listfile/listfile.go`
- Create: `internal/listfile/listfile_test.go`
- Create: `internal/casc/file.go`
- Create: `internal/casc/file_test.go`
- Create: `internal/export/files.go`
- Create: `internal/export/files_test.go`
- Modify: `internal/app/commands.go`

### Task 1: Port Listfile Core

- [ ] Implement load from cached file.
- [ ] Implement load from text source.
- [ ] Implement lookup by fileDataID.
- [ ] Implement filtered search.
- [ ] Implement extension query.
- [ ] Implement unknown model/texture enrichment hooks.
- [ ] Run: `go test ./internal/listfile`

### Task 2: Implement File Metadata Access

- [ ] Implement `file exists` by ID and filename.
- [ ] Implement `file encoding` content-key and encoding-key metadata.
- [ ] Implement virtual file access parity for ID and name.
- [ ] Add Node golden comparisons.

### Task 3: Implement Raw File Get and Export

- [ ] Implement `file get` writing to stdout or output path.
- [ ] Implement `file export` with deterministic overwrite/cache behavior.
- [ ] Include content hash in JSON response.
- [ ] Add tests for missing output path and missing file.

### Task 4: Wire CLI Commands

- [ ] Replace `file lookup`, `file search`, `file extension`, `file get`, `file exists`, `file encoding`, and `file export` stubs.
- [ ] Add command tests for JSON envelope and error behavior.
- [ ] Compare with Node fixtures.

### Task 5: Verify Phase 5

- [ ] Run: `go test ./...`
- [ ] Run representative file commands.
- [ ] Update golden manifest.
- [ ] Commit: `git commit -m "feat: add listfile and file export commands"`
