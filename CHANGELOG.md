# Changelog

## v0.0.1 - 2026-06-01

- Fixed local CASC file reads to decode BLTE payloads consistently with remote CASC reads.
- Fixed DB2 schema output to preserve array field lengths, such as `uint32[3]`.
- Fixed item inventory slot display names so DB2 `InventoryType` values map to their real item slots, including Trinket.
- Improved item model selection for race, gender, neutral variants, and paired shoulder model resources.
- Kept listfile warmup unfiltered so generated asset lookups remain available after initialization.
- Added same-configuration warmup reuse so repeated explicit `wow_warmup` calls avoid resetting hot in-memory state when the requested build and warmed resources are already covered.
- Added regression coverage for local CASC reads, DB2 schema arrays, item slot names, and item model selection.
- Verified Retail, PTR, Beta, Classic, Classic Era, and Classic Titan DB2 coverage across CN, US, EU, KR, and TW remote products.
