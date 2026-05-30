# wowdata Full Rewrite Plan Index

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement these plans task-by-task. Steps in phase plans use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Coordinate the full Go CLI rewrite of `wowdata` across all capability domains while preserving full Node capability parity.

**Architecture:** Execute the rewrite as a sequence of independently reviewable phase plans. Each phase creates tests, implements the minimum needed package surface, and expands golden coverage before moving to the next domain.

**Tech Stack:** Go, Cobra CLI, Go `testing`, JSON golden fixtures, filesystem cache, HTTP client, binary parsing, Node baseline runner.

---

## Hard Constraints

- Do not leave `TODO`, `TBD`, placeholder text, fake implementations, empty functions, unconditional success paths, or "fill this in later" work in any plan output or implementation.
- Do not mark a capability complete until it has real behavior, tests, and golden coverage where Node parity applies.
- Do not skip hard parser/export behavior by returning mock JSON, static fixtures, or hand-written sample data.
- Do not weaken Node parity. Every non-MCP Node business capability must have an equivalent `wowdata` CLI path.
- Phase 0 may introduce explicit `not_implemented` command responses only as a temporary command-surface scaffold. Later phases must remove those responses for their owned commands before claiming completion.
- If an agent cannot implement a capability, it must stop and report the blocker instead of silently substituting a placeholder.

## Plan Order

1. `2026-05-31-wowdata-phase-0-contract-cli.md`
   - Go module, CLI skeleton, command tree, output envelope, capability inventory, golden rules.
2. `2026-05-31-wowdata-phase-1-node-baseline-golden.md`
   - Node capability scanner, baseline runner, fixture manifest, first golden captures.
3. `2026-05-31-wowdata-phase-2-casc-foundation.md`
   - Config parsing, products/builds, local/remote source shell, cache paths, CDN resolver.
4. `2026-05-31-wowdata-phase-3-blte-tact-cache.md`
   - BLTE blocks, zlib, Salsa20, TACT key loading, data/cache retrieval.
5. `2026-05-31-wowdata-phase-4-dbd-db2.md`
   - DBD parser, WDC/DBC readers, DB2 query helpers, schema/rows/search/foreign-key/stream.
6. `2026-05-31-wowdata-phase-5-listfile-file-export.md`
   - listfile load/search/extension, CASC file lookup/get/exists/encoding/export.
7. `2026-05-31-wowdata-phase-6-blp-icon-video-export.md`
   - BLP decode, PNG/WebP export, icon command parity, VP9 AVI demuxer parity, artifact hash verification.
8. `2026-05-31-wowdata-phase-7-spell-encounter.md`
   - spell chains, aura/summon detection, encounter section trees.
9. `2026-05-31-wowdata-phase-8-items-creatures-decor.md`
   - item summaries, item models, geosets, textures, creature display/model, decor.
10. `2026-05-31-wowdata-phase-9-diagnostics-cutover.md`
    - diagnostics, full parity sweep, README update, Node retirement.

## Execution Rule

Do not start a later phase until the previous phase passes its verification commands and the golden fixture manifest has been updated for that phase.

## Completion Rule

The rewrite is complete only when:

- every planned CLI command is implemented
- every Node capability in `docs/wowdata/capability-inventory.md` has an equivalent CLI command, except MCP transport itself
- golden comparison passes for every published command and representative error path
- normal usage no longer depends on Node
