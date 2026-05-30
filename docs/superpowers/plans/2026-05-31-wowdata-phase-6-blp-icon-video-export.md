# wowdata Phase 6 BLP, Icon Export, and Video Demux Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement BLP decode, icon export to PNG/WebP, and VP9 AVI demuxing with Node-equivalent mask, mipmap, quality, and demux behavior.

**Architecture:** Port BLP parsing and VP9 AVI demuxing into focused packages, then route `icon export` and `video demux` through file access and artifact output helpers. Use content hashes and frame metadata for golden verification.

**Tech Stack:** Go, image packages, PNG encoding, WebP dependency decision, binary parsing, artifact hash fixtures.

---

## Hard Constraints

- Do not leave `TODO`, `TBD`, placeholder text, fake implementations, empty functions, unconditional success paths, or "fill this in later" work in any plan output or implementation.
- Do not mark a capability complete until it has real behavior, tests, and golden coverage where Node parity applies.
- Do not skip hard parser/export behavior by returning mock JSON, static fixtures, or hand-written sample data.
- Do not weaken Node parity. Every non-MCP Node business capability must have an equivalent `wowdata` CLI path.
- Phase 0 may introduce explicit `not_implemented` command responses only as a temporary command-surface scaffold. Later phases must remove those responses for their owned commands before claiming completion.
- If an agent cannot implement a capability, it must stop and report the blocker instead of silently substituting a placeholder.

## File Structure

- Create: `internal/blp/blp.go`
- Create: `internal/blp/blp_test.go`
- Create: `internal/video/vp9_avi_demuxer.go`
- Create: `internal/video/vp9_avi_demuxer_test.go`
- Create: `internal/export/icon.go`
- Create: `internal/export/icon_test.go`
- Modify: `internal/app/commands.go`

### Task 1: Port BLP Header and Raw Mipmap Parsing

- [ ] Capture Node BLP metadata fixture.
- [ ] Implement header parsing.
- [ ] Implement mipmap offset and size extraction.
- [ ] Add tests for invalid magic, unsupported encoding, and valid metadata.

### Task 2: Port Pixel Decoding

- [ ] Implement uncompressed paths.
- [ ] Implement compressed paths used by current icon fixtures.
- [ ] Implement alpha mask behavior.
- [ ] Compare decoded RGBA hashes with Node output.

### Task 3: Implement PNG Export

- [ ] Implement `ToPNG`.
- [ ] Implement output path creation.
- [ ] Return JSON with path, status, format, mipmap, mask, and content hash.
- [ ] Compare output hash with Node `extractIcon` fixture.

### Task 4: Implement WebP Export

- [ ] Choose and integrate a Go WebP encoder dependency.
- [ ] Add WebP fixture coverage.
- [ ] Keep `--format webp` help and error behavior explicit.

### Task 5: Wire CLI Command

- [ ] Replace `icon export` stub.
- [ ] Add tests for PNG, cached output, bad fileDataID, bad format.
- [ ] Update golden manifest.

### Task 6: Port VP9 AVI Demuxer

- [ ] Port `core/casc/vp9-avi-demuxer.js`.
- [ ] Parse AVI headers needed by the Node implementation.
- [ ] Find chunks by FourCC.
- [ ] Parse `movi` frames with remainder behavior matching Node.
- [ ] Add fixtures for valid header, missing chunk, and partial frame remainder.
- [ ] Compare frame counts and frame metadata with Node fixture output.

### Task 7: Wire Video CLI Command

- [ ] Replace `video demux` stub.
- [ ] Add command tests for input path, output path, missing file, and JSON metadata response.
- [ ] Update golden manifest with `video/demux-*` fixtures.
- [ ] Commit: `git commit -m "feat: add blp icon export and video demux"`
