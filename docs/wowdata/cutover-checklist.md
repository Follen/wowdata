# wowdata Go CLI Cutover Checklist

## Verified

- [x] `go test ./...` — all 14 packages pass (42+ tests)
- [x] `wowdata --help` — shows all 12 command groups
- [x] All planned CLI commands have real handlers (not not_implemented)
- [x] Go binary builds: `go build ./cmd/wowdata`
- [x] CASC foundation: types, cache paths, source selection
- [x] Salsa20 stream cipher (tested against Node vectors)
- [x] TACT key ring: loading, validation, JSON import
- [x] BLTE reader: uncompressed, zlib, encrypted blocks with integrity
- [x] Data cache: store/get, integrity verification, path normalization
- [x] DBD parser: column types, build ranges, layout hashes
- [x] WDC reader: header, sections, compressed fields, copy table
- [x] DBC reader: legacy table format with string block
- [x] Listfile: ID lookup, filename search, extension filter
- [x] CASC file service: exists, encoding metadata
- [x] File export: path creation, hash output
- [x] BLP decoder: paletted, BGRA, mipmap, PNG export
- [x] VP9 AVI demuxer: RIFF parsing, frame extraction
- [x] Spell: trigger traversal, cycle detection, aura/summon detection
- [x] Encounter: section tree build with ordering
- [x] Items: summary, models, geosets, textures, equipment slots
- [x] Creatures: modern displays, legacy path lookup
- [x] Decor: list, get by ID, get by model
- [x] Diagnostics: CASC info, products, health check
- [x] Golden: capture and compare stubs

## Next Steps for Full Parity

These require CASC HTTP/data fetch context:

- [ ] `casc info` — requires live CASC context (build config, encoding)
- [ ] `casc products` — requires CDN version config fetch
- [ ] `casc diagnose` — requires CDN host resolution
- [ ] `file get/export` — requires CASC blob retrieval
- [ ] `icon export` — requires CASC BLP file fetch
- [ ] `db2 schema/rows/search/foreign-key/stream` — requires DB2 file fetch
- [ ] `warmup` — requires CDN config download, listfile fetch, root/encoding parse
- [ ] Golden capture/compare — requires populated CASC context

## Runtime Cutover

- [ ] `package.json` retained for historical Node scripts
- [ ] Node source maintained as reference baseline on `main` branch
- [ ] `wowdata` Go binary is the primary entry point on `go-fully-rewrite` branch
