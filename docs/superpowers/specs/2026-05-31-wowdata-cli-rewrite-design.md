# wowdata CLI Rewrite Design

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rewrite the current WoW data tooling as a single Go CLI called `wowdata` that exposes all existing business capabilities with stable, script-friendly output.

**Architecture:** Build one shared Go core for CASC, DB2/WDC/DBC, BLTE, BLP, listfile, item, creature, decor, and diagnostics logic. Put a thin CLI layer on top that routes subcommands to the shared services and prints deterministic JSON by default. Use Node as the behavior baseline until Go passes golden-output comparison for every exposed command.

**Tech Stack:** Go, standard library, Cobra or a similar CLI library, JSON output, filesystem cache, HTTP client, binary parsing, fixture-based golden tests.

---

## Scope

`wowdata` will replace the current Node implementation as the primary executable.
It must expose every meaningful capability already present in the repo, not only the current MCP tools.

Included capability groups:

- warmup and source selection for local client or remote CDN
- DB2 schema, row, filter, foreign-key, and search access
- spell chains, aura detection, and summon detection
- encounter section trees and related spell IDs
- file lookup, search, extension queries, raw fetch, and export
- icon extraction from BLP to PNG and WebP
- CASC diagnostics, encoding/root/build inspection, and file existence checks
- item, model, geoset, texture, creature, and decor queries
- golden capture and comparison utilities for regression testing

## Non-Goals

- No MCP server in the first release.
- No partial rewrite that leaves the Node runtime as the main product.
- No behavioral redesign that changes business meaning just to simplify implementation.
- No GUI.

## CLI Shape

The binary name is `wowdata`.

Required top-level behavior:

- `wowdata --help`
- `wowdata <command> --help`
- stable exit codes
- machine-readable JSON output for normal command results
- human-readable error text on stderr

Planned command groups:

- `warmup`
- `db2`
- `spell`
- `encounter`
- `file`
- `icon`
- `casc`
- `item`
- `creature`
- `decor`
- `golden`

Planned subcommands:

- `wowdata warmup`: initialize source, region, product/build, listfile, DBD manifest, and optional DB2 table preload
- `wowdata db2 schema <table>`: print parsed DBD/WDC schema metadata
- `wowdata db2 rows <table>`: fetch rows by IDs, field selection, filter, and limit
- `wowdata db2 search <table>`: case-insensitive field search
- `wowdata db2 foreign-key <table>`: query relationship rows by foreign key
- `wowdata db2 stream <table>`: stream rows as JSON lines for large tables
- `wowdata spell info`: recursively inspect trigger chains and description references
- `wowdata spell auras`: detect spell aura presence
- `wowdata spell summons`: detect NPC summons from spell effects
- `wowdata encounter get`: return a JournalEncounter section tree and related SpellIDs
- `wowdata file lookup`: resolve fileDataID to filename
- `wowdata file search`: search listfile entries
- `wowdata file extension`: list files by extension
- `wowdata file get`: fetch a raw CASC file by fileDataID or filename
- `wowdata file exists`: test whether a fileDataID or filename exists
- `wowdata file encoding`: inspect content key and encoding key metadata
- `wowdata file export`: write raw CASC files to disk
- `wowdata icon export`: export BLP textures to PNG or WebP with mask, mipmap, and quality flags
- `wowdata casc info`: show selected build, build key, region, source, locale, and cache paths
- `wowdata casc products`: list available products/builds for a source
- `wowdata casc diagnose`: inspect CDN host, archive, root, encoding, cache, and TACT key status
- `wowdata item get`: return item summary and slot information
- `wowdata item models`: return item model fileDataIDs, race/gender selection, and textures
- `wowdata item geosets`: return item geoset and helmet-hide data
- `wowdata item textures`: return character texture fileDataIDs
- `wowdata creature display`: query creature display metadata by display ID or fileDataID
- `wowdata creature model`: query creature model fileDataID and display variants
- `wowdata decor list`: list decor items
- `wowdata decor get`: query decor item by ID or model fileDataID
- `wowdata golden capture`: capture Node baseline or Go command output into fixtures
- `wowdata golden compare`: compare Go command output against captured baselines

Example intent:

- `wowdata warmup --source remote --region cn --product wow`
- `wowdata db2 rows SpellName --id 123`
- `wowdata file lookup --file-data-id 456`
- `wowdata icon export --file-data-id 789 --format png`

## Data Model

The Go core should keep explicit runtime state for:

- selected source: local or remote
- region and product/build context
- build cache and downloaded manifest data
- loaded listfile and DBD manifest state
- cache paths and output paths
- current warmup status and diagnostics

The CLI must not duplicate business logic; it only parses flags, calls services, and formats output.

## Implementation Boundaries

Suggested package split:

- `internal/app`: command wiring and lifecycle
- `internal/casc`: local/remote source loading, build config, archives, encoding, root, caches
- `internal/db2`: DB2/WDC/DBC readers and query helpers
- `internal/blp`: texture decode/export
- `internal/listfile`: filename and extension lookup
- `internal/wowdata`: item, creature, decor, spell, and encounter business services
- `internal/golden`: fixture capture and comparison
- `cmd/wowdata`: binary entrypoint

The command layer should depend on interfaces, not concrete parser internals.

## Output Contract

Default command output should be JSON with a stable envelope:

- `ok`
- `command`
- `data`
- `warnings`
- `error` on failures

This keeps scripting simple and makes Node-vs-Go comparisons precise.

## Help Design

`--help` must be useful without prior context.

Required help content:

- what `wowdata` does
- how to warm up local or remote sources
- list of command groups
- one-line examples for common workflows
- note that some commands may take time on first use

Each command help page must include:

- purpose
- required arguments
- important optional flags
- JSON output shape summary
- at least one example
- whether warmup or an active build context is required

Help output is part of the product surface. Tests should verify that `wowdata --help` and every planned `wowdata <command> --help` command returns exit code 0.

## Regression Strategy

Before replacing Node, capture golden outputs from the current implementation.

Golden coverage should include:

- representative warmup flows
- every exposed command
- error paths for missing args and uninitialized state
- sample local and remote source behavior
- file export outputs and cache reuse

Comparison rules:

- compare semantic JSON fields, not raw formatting
- compare file existence and exported content hashes for generated artifacts
- keep expected divergences documented if the Go CLI intentionally improves formatting

## Migration Plan

1. Freeze current Node behavior with fixtures.
2. Implement Go core packages.
3. Implement `wowdata` CLI help and command tree.
4. Port the currently exposed tools first.
5. Port the unexposed business capabilities.
6. Run golden comparisons until parity is reached.
7. Replace the Node entrypoint with the Go binary.
8. Retire Node source files only after the Go release is stable.

## Risks

- CASC and DB2 parsing are the highest-risk areas because they encode implicit WoW-specific behavior.
- Output drift is likely unless golden fixtures are captured early.
- Some legacy Node behavior may be accidental; those cases must be explicitly classified instead of copied blindly.

## Acceptance Criteria

- `wowdata --help` works.
- Every intended capability is reachable from a CLI command.
- Warmup works for both local and remote sources.
- Golden comparisons pass for the published command set.
- The Node version is no longer required for normal use.
