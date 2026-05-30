# wowdata Phase 4 DBD and DB2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement DBD parsing, WDC/DBC readers, and DB2 CLI commands with Node-equivalent query behavior.

**Architecture:** Port schema parsing first, then binary table readers, then query helpers, then CLI commands. Every query mode must compare against Node golden fixtures before being considered complete.

**Tech Stack:** Go, binary readers, DBD fixtures, DB2/WDC fixtures, golden JSON comparison.

---

## File Structure

- Create: `internal/dbd/parser.go`
- Create: `internal/dbd/parser_test.go`
- Create: `internal/db2/types.go`
- Create: `internal/db2/wdc.go`
- Create: `internal/db2/dbc.go`
- Create: `internal/db2/query.go`
- Create: `internal/db2/query_test.go`
- Modify: `internal/app/commands.go`

### Task 1: Port DBD Parser

- [ ] Capture Node DBD parse output for a small table definition.
- [ ] Write Go tests for build/layout selection.
- [ ] Implement fields, build ranges, layout hashes, and structure selection.
- [ ] Run: `go test ./internal/dbd`

### Task 2: Port WDC Reader

- [ ] Implement header parsing.
- [ ] Implement section parsing.
- [ ] Implement ID maps, copy table, relationship data, string table lookup.
- [ ] Add fixtures for `getRow`, `getAllRows`, and encrypted-section skip behavior.
- [ ] Run: `go test ./internal/db2`

### Task 3: Port DBC Reader

- [ ] Implement legacy DBC header and row parsing.
- [ ] Implement string table lookup.
- [ ] Add legacy creature fixture coverage.
- [ ] Run: `go test ./internal/db2`

### Task 4: Implement Query Helpers

- [ ] Implement schema output.
- [ ] Implement rows by ID, field projection, filter, and limit.
- [ ] Implement case-insensitive search.
- [ ] Implement foreign-key helper with relationship fast path and streaming fallback.
- [ ] Implement JSON-lines streaming.

### Task 5: Wire CLI Commands

- [ ] Replace `db2` command stubs with real handlers.
- [ ] Add command tests for `db2 schema`, `db2 rows`, `db2 search`, `db2 foreign-key`, and `db2 stream`.
- [ ] Compare against Node fixtures.

### Task 6: Verify Phase 4

- [ ] Run: `go test ./...`
- [ ] Run representative `wowdata db2` commands.
- [ ] Update golden manifest.
- [ ] Commit: `git commit -m "feat: add dbd and db2 queries"`
