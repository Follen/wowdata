e# wowdata

`wowdata` is a pure Go CLI and MCP server for querying World of Warcraft CASC, DB2, listfile, texture, spell, encounter, item, creature, decor, video, and diagnostic data from either a local client install or Blizzard CDN builds.

## Build

```powershell
go build ./cmd/wowdata
```

Run directly during development:

```powershell
go run ./cmd/wowdata --help
```

## Warmup

Most commands need an active CASC/DB2 context. Use `warmup` directly, or pass `--auto-warmup` to commands that should initialize first.

Remote CDN:

```powershell
go run ./cmd/wowdata warmup --source remote --region cn --product wow
```

Local client:

```powershell
go run ./cmd/wowdata warmup --source local --path "D:\Game\World of Warcraft" --region cn --product wow
```

List products from a local client without loading a build:

```powershell
go run ./cmd/wowdata casc products --source local --path "D:\Game\World of Warcraft" --region cn
```

Optional warmup controls:

- `--listfile=true|false`
- `--listfile-format binary|text`
- `--dbd-manifest=true|false`
- `--tables SpellName,Spell,SpellEffect`
- `--locale zhCN|enUS|...` (default `zhCN`)

## Command Groups

- `warmup`
- `db2 schema|rows|search|foreign-key|stream`
- `spell info|auras|summons`
- `encounter get`
- `file lookup|search|extension|get|exists|encoding|export`
- `icon export`
- `casc info|products|diagnose`
- `item get|models|geosets|textures`
- `creature display|model`
- `decor list|get`
- `video demux`
- `golden capture|compare`
- `mcp serve`

Examples:

```powershell
go run ./cmd/wowdata --auto-warmup --source remote --region us --product wow_classic_era --listfile=true --dbd-manifest=true --tables=SpellName db2 rows SpellName --id 1
go run ./cmd/wowdata --auto-warmup --source remote --region us --product wow_classic_era --listfile=true --dbd-manifest=false --tables= file lookup --file-data-id 134400
go run ./cmd/wowdata --auto-warmup --source remote --region us --product wow_classic_era --listfile=false --dbd-manifest=false --tables= icon export --file-data-id 134400 --format png --output output/icon.png
```

`db2 stream` defaults to JSONL and also supports aggregate JSON:

```powershell
go run ./cmd/wowdata --auto-warmup --source remote --region us --product wow_classic_era --tables=SpellEffect db2 stream SpellEffect --limit 3
go run ./cmd/wowdata --auto-warmup --source remote --region us --product wow_classic_era --tables=SpellEffect db2 stream SpellEffect --limit 3 --format json
```

WebP icon export is always lossless. There is no `--quality` flag.

## MCP Server

Run the same Go runtime as an MCP stdio server:

```powershell
go run ./cmd/wowdata mcp serve
```

The `--mcp` alias is also supported by the built binary:

```powershell
wowdata --mcp
```

MCP tools reuse the in-process CLI runtime and preserve command JSON envelopes. The exposed tools are `wow_warmup`, `wow_casc`, `wow_db2`, `wow_file`, `wow_icon`, `wow_spell`, `wow_encounter`, `wow_item`, `wow_creature`, `wow_decor`, and `wow_video`. The development-only `golden` command stays CLI-only.

## Cache And Output

Generated local data is intentionally not committed:

- `cache/`: Go CASC, DBD, listfile, and TACT key caches.
- `output/`: default exported artifacts.

Large remote downloads use concurrent HTTP range requests when the server supports byte ranges. This includes CASC data files, text `community-listfile.csv`, and each binary listfile component.

## Golden Fixtures

Golden fixtures compare captured Go command output against expected Go fixtures:

```powershell
go test ./... -count=1
go run ./cmd/wowdata golden compare --all --fixture fixtures/golden/manifest.json
```
