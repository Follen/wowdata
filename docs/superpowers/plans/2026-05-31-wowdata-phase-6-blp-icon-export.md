# wowdata Phase 6 BLP and Icon Export Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement BLP decode and icon export to PNG/WebP with Node-equivalent mask, mipmap, and quality behavior.

**Architecture:** Port BLP parsing into a focused package, then route `icon export` through CASC file access and artifact output helpers. Use content hashes for golden verification.

**Tech Stack:** Go, image packages, PNG encoding, WebP dependency decision, binary parsing, artifact hash fixtures.

---

## File Structure

- Create: `internal/blp/blp.go`
- Create: `internal/blp/blp_test.go`
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

- [ ] Choose Go WebP dependency or document intentional difference if WebP cannot be supported without unacceptable dependency cost.
- [ ] Add WebP fixture if implemented.
- [ ] Keep `--format webp` help and error behavior explicit.

### Task 5: Wire CLI Command

- [ ] Replace `icon export` stub.
- [ ] Add tests for PNG, cached output, bad fileDataID, bad format.
- [ ] Update golden manifest.
- [ ] Commit: `git commit -m "feat: add blp icon export"`
