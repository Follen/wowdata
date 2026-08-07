# Failure handling

- Structured `{"ok":false,"error":{"code":...,"message":...}}` with exit `1`: report the code/message and retain the target and filters.
- Plain stderr with a usage/flag error: read the relevant `--help`, correct only the malformed flag, and retry once.
- Missing/ambiguous product, Build, locale, or local path: resolve from context or ask one concise question; do not guess.
- Cache/DBD/schema mismatch: run `doctor` or `db2 schema` for the same target; do not silently switch product or Build.
- Remote prepare/download failure: report provider and target, then retry with the same explicit target or use an explicitly requested local cache/profile. Do not launch a separate warmup unless requested.
- Empty success: treat as valid; verify identifiers and locale before trying another source.
