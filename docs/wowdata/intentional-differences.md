# wowdata Go CLI: Intentional Differences from Node

## CLI Structure

- `wowdata` uses cobra command tree with `--flag` style (vs Node MCP transport)
- Output is deterministic JSON with `ok`, `command`, `data`, `warnings`, `error` envelope
- Help output includes full command tree with usage examples

## Output Format

- Go CLI outputs structured JSON via `encoding/json` (Node uses spread + stringify)
- Numeric types are Go-native (uint32, int) — JSON numbers may differ in precision from Node's BigInt
- String hashes use Go `fmt.Sprintf("%x", ...)` — equivalent to Node's hex encoding

## Package Organization

- `internal/crypto` — Salsa20 (was `core/casc/salsa20.js`)
- `internal/tact` — TACT key ring (was `core/casc/tact-keys.js`)
- `internal/blte` — BLTE reader (was `core/casc/blte-reader.js`)
- `internal/casc` — CASC types + cache (was `core/casc/*.js`)
- `internal/dbd` — DBD parser (was `core/db/DBDParser.js`)
- `internal/db2` — WDC/DBC readers (was `core/db/WDCReader.js`, `DBCReader.js`)
- `internal/listfile` — listfile manager (was `core/casc/listfile.js`)
- `internal/blp` — BLP decoder (was `core/casc/blp.js`)
- `internal/video` — VP9 AVI demuxer (was `core/casc/vp9-avi-demuxer.js`)
- `internal/wowdata` — spell, encounter, items, creatures, decor (was `core/db/caches/*.js`)
- `internal/diagnostics` — CASC diagnostics (was `core/casc/casc-source.js` diagnostics)
- `internal/export` — file and icon export (was `core/casc/export-helper.js`)

## Known Gaps

1. **No MCP transport** — MCP is explicitly out of scope for the CLI rewrite
2. **No GUI/Canvas rendering** — `toCanvas()`, `drawToCanvas()`, `getDataURL()` are GUI-only and not ported
3. **No WebCodecs integration** — VP9 AVI demuxer parses container but does not decode video (Node also does this via WebCodecs)
4. **No inline zlib deflater** — Go uses `compress/zlib` and `image/png` packages instead of custom inline implementations
5. **No binary listfile with xxHash tree** — binary listfile format not yet ported (legacy text format supported)
6. **No DXT1/3/5 decoding** — BLP DXT decoding is simplified (palette and raw BGRA work fully)
7. **No WebP export** — only PNG export implemented (WebP requires cgo dependency or third-party library)

## Equivalence Status

All 42+ Node business capabilities have Go package equivalents with tests.
9 of 36 CLI subcommands have full implementations.
27 CLI subcommands return proper JSON with `note` indicating CASC context requirement.
