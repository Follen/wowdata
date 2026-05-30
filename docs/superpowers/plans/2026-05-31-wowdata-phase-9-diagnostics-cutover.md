# wowdata Phase 9 Diagnostics and Cutover Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish diagnostics, run full parity verification, update documentation, and retire Node as the normal runtime.

**Architecture:** Add user-facing diagnostics after all core capabilities exist, then perform a full golden sweep. Cutover only after the CLI replaces normal Node workflows and documented intentional differences are accepted.

**Tech Stack:** Go, golden compare runner, README docs, release checklist.

---

## File Structure

- Create: `internal/diagnostics/diagnostics.go`
- Create: `internal/diagnostics/diagnostics_test.go`
- Modify: `internal/app/commands.go`
- Modify: `README.md`
- Modify: `package.json`
- Create: `docs/wowdata/cutover-checklist.md`
- Create: `docs/wowdata/intentional-differences.md`

### Task 1: Implement Diagnostics

- [ ] Implement `casc info`.
- [ ] Implement `casc products`.
- [ ] Implement `casc diagnose`.
- [ ] Include source, region, product/build, cache paths, CDN host, archive state, root/encoding state, TACT key state.
- [ ] Compare with Node diagnostics where available.

### Task 2: Implement Golden Compare Commands

- [ ] Replace `golden capture` stub with fixture capture.
- [ ] Replace `golden compare` stub with semantic JSON comparison.
- [ ] Fail when a Node capability has no Go CLI equivalent.
- [ ] Store diff JSON for mismatches.

### Task 3: Full Parity Sweep

- [ ] Run all Go tests.
- [ ] Run all CLI help commands.
- [ ] Run golden compare for every fixture in the manifest.
- [ ] Update `docs/wowdata/intentional-differences.md` for accepted differences only.

### Task 4: Documentation Cutover

- [ ] Rewrite README usage around `wowdata`.
- [ ] Document install/build commands.
- [ ] Document cache and output directories.
- [ ] Remove MCP usage as the primary path.
- [ ] Keep Node reference only as historical baseline if still present.

### Task 5: Runtime Cutover

- [ ] Decide whether `package.json` is removed, retained for historical scripts, or changed to call `wowdata`.
- [ ] Retire Node source files only after full golden pass.
- [ ] Keep fixtures, docs, and intentional-difference records.

### Task 6: Verify Phase 9

- [ ] Run: `go test ./...`
- [ ] Run: `go run ./cmd/wowdata --help`
- [ ] Run: `go run ./cmd/wowdata golden compare --all`
- [ ] Run: `git status --short`
- [ ] Commit: `git commit -m "chore: cut over to wowdata cli"`
