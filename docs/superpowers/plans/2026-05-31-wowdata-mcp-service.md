# wowdata MCP Service Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a stateful `wowdata mcp serve` mode that exposes all Go CLI capability groups as MCP tools.

**Architecture:** Implement a small stdio JSON-RPC MCP server in `internal/mcpserver`, then map MCP tool calls to the existing Cobra command handlers in `cmd/wowdata` using one shared `Runtime`. Tool results preserve the existing JSON response envelope, while download progress remains on stderr/log output.

**Tech Stack:** Go stdlib JSON-RPC framing, Cobra, existing `Runtime` and `internal/app` handlers.

---

### Task 1: MCP Protocol Core

**Files:**
- Create: `internal/mcpserver/server.go`
- Test: `internal/mcpserver/server_test.go`

- [ ] Add JSON-RPC initialize, tools/list, tools/call handling over MCP stdio framing.
- [ ] Return tool call results as MCP text content.
- [ ] Ignore notifications without emitting responses.
- [ ] Verify with `go test ./internal/mcpserver -count=1`.

### Task 2: CLI Command Bridge

**Files:**
- Modify: `cmd/wowdata/main.go`
- Test: `cmd/wowdata/mcp_test.go`

- [ ] Extract root command construction into a reusable function bound to a shared `Runtime`.
- [ ] Add `wowdata mcp serve` and `--mcp` alias for stdio serving.
- [ ] Implement tool definitions for warmup, casc, db2, file, icon, spell, encounter, item, creature, decor, video, and golden.
- [ ] Map each tool's structured JSON arguments to the existing CLI arguments and execute in process.
- [ ] Verify with `go test ./cmd/wowdata -count=1`.

### Task 3: Documentation And Smoke Test

**Files:**
- Modify: `README.md`
- Modify: `docs/wowdata/cutover-checklist.md`

- [ ] Document `wowdata mcp serve`, `wowdata --mcp`, default `zhCN` locale, and stdio usage.
- [ ] Run `go test ./... -count=1`.
- [ ] Smoke test `tools/list` and one `wow_casc` or `wow_warmup` call through framed stdio.
