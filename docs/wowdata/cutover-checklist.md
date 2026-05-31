# wowdata Go CLI Cutover Checklist

## Verified

- [x] `go test ./...` — passes as of current workspace verification
- [x] `wowdata --help` — shows all 12 command groups
- [x] `wowdata mcp serve` — exposes all command groups as in-process MCP stdio tools, with `wowdata --mcp` as a built-binary alias
- [x] All planned CLI commands have real handlers with Go tests and fixtures
- [x] Go binary builds: `go build ./cmd/wowdata`
- [x] CASC foundation: types, cache paths, source selection
- [x] Salsa20 stream cipher
- [x] TACT key ring: loading, validation, JSON import
- [x] BLTE reader: uncompressed, zlib, encrypted blocks with integrity
- [x] Data cache: store/get, integrity verification, path normalization
- [x] DBD parser: column types, build ranges, layout hashes
- [x] WDC reader: header, sections, compressed fields, copy table
- [x] DBC reader: legacy table format with string block
- [x] Listfile: legacy text and binary component format, ID lookup, filename search, extension filter
- [x] Remote download behavior: large CASC data files, text `community-listfile.csv`, and individual binary listfile components use concurrent HTTP byte ranges, with normal `GET` fallback for small or non-range servers; binary listfile loading has both component-level concurrency and per-large-component range chunking
- [x] CASC file service: exists, encoding metadata
- [x] File export: path creation, hash output
- [x] BLP decoder: paletted, BGRA, DXT1/DXT3/DXT5, mipmap, PNG and lossless WebP export
- [x] VP9 AVI demuxer: RIFF parsing, frame extraction
- [x] Spell: trigger traversal, cycle detection, aura/summon detection
- [x] Encounter: section tree build with ordering
- [x] Items: summary, models, geosets, textures, equipment slots
- [x] Creatures: modern displays, legacy path lookup
- [x] Decor: list, get by ID, get by model
- [x] Diagnostics: CASC info, products, health check
- [x] Golden: Go fixture capture and compare utilities
  - Current gate is truthful: empty/incomplete manifests fail with `missing_required_groups`.
  - Captured command fixtures compare the JSON inside `stdout` semantically, so command wrapper metadata does not mask or invent business parity.
  - Additional real fixtures pass: `warmup/prompt-no-args`, `video/demux-minimal-vp9`.
  - Go fixtures cover one fixture in every required group plus DB2 schema/rows/search/foreign-key/stream, file lookup/search/extension/get/exists/encoding/export, detailed spell info/summons, a richer Retail encounter section tree, item models/geosets/textures, creature display/model success coverage, and real-client local warmup.
  - Local warmup parity is covered by `warmup/local-wow-cn` against `D:\Game\World of Warcraft`; the fixture proves `.build.info`, local `.idx`, config, encoding, root, and archive data loading against a real client path with spaces.

## Full Parity Evidence

- [x] `warmup --source local` — Go local CASC source reads `.build.info`, local `.idx` files, config files, encoding, root, and local data archives; real-client fixture `warmup/local-wow-cn` covers `D:\Game\World of Warcraft`
- [x] `icon export` WebP mode — Go WebP output is intentionally always lossless and has no quality toggle
- [x] `db2 schema/rows/search/foreign-key/stream` — relationship-map semantics and both JSONL/default stream support plus `--format json` aggregate output are covered
- [x] `spell`, `encounter`, `item`, `creature`, `decor` — spell info/auras/summons, item get/models/geosets/textures, creature display/model, Retail encounter section tree, and Retail decor get by ID/model are covered
- [x] `casc info/products/diagnose` — handler paths exist; remote products, remote diagnose, local warmup state, local `casc products --source local --path ...`, and `casc info` locale output are covered by tests or fixtures
- [x] Golden capture/compare — implementation exists, quoted command arguments are supported for paths with spaces, and `golden compare --all` passes for seeded required groups
- [x] Documentation and intentional differences — binary listfile, DXT, WebP lossless-only, partial DB2 decrypt, and concurrent range download decisions are documented as approved behavior or implementation improvements
- [x] MCP service mode — `wow_warmup`, `wow_casc`, `wow_db2`, `wow_file`, `wow_icon`, `wow_spell`, `wow_encounter`, `wow_item`, `wow_creature`, `wow_decor`, and `wow_video` reuse the Go CLI runtime in process; development-only `golden` stays CLI-only

## Runtime Cutover

- [x] Legacy JavaScript runtime, dependencies, baseline scripts, and fixtures removed for the pure Go cutover
- [x] `wowdata` Go CLI and MCP server are documented as the primary entry points
