# wowdata Node Capability Inventory

This inventory is the required parity checklist for the Go `wowdata` CLI.

## Old Node Entry Capabilities

- warmup source selection: local, remote
- region selection: cn, us, eu, kr, tw
- product/build selection: wow, wow_classic, wow_classic_titan, wow_classic_era, wowt, wowxptr, buildIndex
- listfile warmup and cache validation
- DBD manifest warmup
- DB2 table preload
- DB2 schema, rows, search, field selection, filters, limits
- spell info chains, aura detection, summon detection
- encounter section tree and related spell IDs
- file lookup, search, extension query, icon extraction

## Core Capabilities That Must Become CLI Commands

- CASC file access by fileDataID
- CASC file access by filename
- CASC virtual file access by fileDataID and filename
- file existence checks
- file encoding metadata: content key and encoding key
- install manifest reading
- local source loading, indexes, configs, root, encoding
- remote source loading, CDN config, archives, root, encoding
- CDN host resolution and diagnostics
- build cache integrity and manifest behavior
- TACT key loading, lookup, and missing-key diagnostics
- BLTE full decode and stream decode
- Salsa20 decryption behavior used by BLTE
- DBD manifest lookup by table name and file ID
- DB2 get row, get all rows, relation rows, foreign-key helper, stream rows
- DBC legacy reader support used by legacy creature data
- listfile lookup by ID, search, extension, unknown model/texture enrichment
- BLP PNG export, WebP export, mipmap, mask, quality, raw buffer behavior
- item summary and slot lookup
- item model lookup by race and gender
- item geoset and helmet-hide lookup
- item character texture lookup
- creature display lookup by fileDataID and display ID
- legacy creature display lookup by model path
- decor list and lookup by ID or model fileDataID
- equipment slot and item slot naming helpers
- VP9 AVI demuxer

## Difference Policy

Any non-business Node behavior not replicated must be listed in an implementation note as an intentional difference with:

- original Node behavior
- Go replacement behavior
- reason for the difference
- affected commands
- test coverage proving the new behavior

Business capabilities must not be skipped. MCP transport is the only excluded capability surface.
