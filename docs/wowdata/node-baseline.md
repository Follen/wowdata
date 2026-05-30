# Node Baseline Capture

The Node implementation is the source of truth for capability parity until Go `wowdata` replaces it.

## Rules

- Capture business output, not incidental console noise.
- Include command args, region, product, build, cache state, and fixture name.
- Store generated artifact hashes instead of large binaries.
- Record only non-business intentional differences before accepting a Go mismatch.
- Reject any missing business capability except MCP transport itself.

## Required First Fixtures

- warmup prompt without args
- warmup remote product selection
- db2 schema for `SpellName`
- db2 rows for a known ID
- file lookup for a known fileDataID
- missing warmup error
