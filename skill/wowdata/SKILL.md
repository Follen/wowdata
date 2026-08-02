---
name: wowdata
description: Query and export World of Warcraft game data with the wowdata CLI, including CASC and DB2 schemas and rows, files, icons, textures, spells, encounters, items, creatures, decor, Builds, profiles, cache, and diagnostics. Use when a user asks to inspect or export WoW data from a local client or CDN, resolve a product/Build/locale target, or diagnose wowdata.
---

# wowdata

Use the `wowdata` executable from `PATH`. This Skill is CLI-only: do not search for a bundled executable and do not use MCP or another server transport.

## Resolve The Target

Resolve the target before running a command that reads CASC or DB2 data.

1. If the user gives a Profile, pass only `--profile <name>`. Do not combine a Profile with target flags.
2. Resolve the product from the user's words or established conversation context. Read [clients.yaml](references/clients.yaml) for English and Chinese aliases. Product has no default; if it is still missing or ambiguous, ask one short product question and do not run a data command yet. Treat “硬核服” as a server rule, not a product: resolve its classic-era or anniversary product from context, or ask which one.
3. Preserve every explicit source, path, region, product, Build, and locale value.
4. When no source or local path is stated, use remote CDN data.
5. For an otherwise unspecified remote target, pass `--source remote --region cn --build latest --locale zhCN` plus the resolved product.
6. For `--source local`, require the client `--path` and complete every remaining target field explicitly. Do not replace missing local-client facts with guesses.

Never rely on CLI target defaults; the CLI intentionally has none. `--build latest` resolves at execution time, so never store a current Build number in this Skill. Read [locales.yaml](references/locales.yaml) when mapping natural-language locale names.

Commands that do not read a game target do not need the full target: `video demux`, `doctor`, `cache`, `profile`, `update`, and `uninstall`. `casc products` uses only source discovery flags such as `--source remote --region cn` or `--source local --path <client>`.

## Run Atomic Commands

Call the business command directly. Do not run `warmup` before an ordinary query. The CLI checks Build identity and cache integrity, downloads missing data, then completes the original query in the same process.

- Use `casc products` when the user asks which products or Builds exist.
- Use `warmup` only when the user explicitly asks to download data ahead of time.
- Use `doctor` after an environment, target, cache, or network failure needs diagnosis.
- Read [commands.md](references/commands.md) before composing an unfamiliar command.
- Read [tables.yaml](references/tables.yaml) when translating a semantic request into DB2 tables.
- Never invent a DB2 field name from a natural-language label. If the user requests selected fields without giving exact schema names, run `db2 schema <table>` first, then use names returned by that schema.
- Use `db2 rows`, never the obsolete `query rows` spelling.
- Keep exports at the user-requested path, or in the current workspace when no path is given.
- Do not run `cache clear`, `profile remove`, `update`, or `uninstall` unless the user explicitly requests that action.

```powershell
wowdata db2 rows SpellName --id 133 --source remote --region cn --product wow --build latest --locale zhCN
wowdata item textures --item-id 19019 --profile retail-cn
wowdata icon export --file-data-id 134400 --format png --output output/icon.png --source remote --region cn --product wow --build latest --locale zhCN
```

## Read Results

Treat the streams and exit status as one contract:

- stderr contains preparation and download progress. If it reports `prepare` or `download`, let the command continue; do not start a separate warmup.
- stdout contains the final JSON. `db2 stream` defaults to JSONL; parse it line by line or pass `--format json` when one JSON document is preferable.
- Exit code `0` corresponds to `"ok": true`.
- Exit code `1` with JSON stdout corresponds to `"ok": false`; report `error.code` and `error.message`. A CLI parsing error may instead be plain stderr.
- An `"ok": true` response with zero rows is a valid empty result. Recheck the ID, product, Build, and locale before trying another query.

Return the requested result, not download narration. Preserve the product, resolved Build, region, locale, table or ID, and output path/hash when present so the answer remains traceable.
