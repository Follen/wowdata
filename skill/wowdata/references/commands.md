# Command Reference

Run `wowdata <command> --help` when a flag is uncertain.

For commands that read CASC or DB2, append either:

```text
--profile <name>
```

or a complete target:

```text
--source remote --region <region> --product <product> --build <latest|version|build-id|config-key> --locale <locale>
```

Use `--source local --path <client>` plus explicit region, product, Build, and locale for a local client.

## Discovery And Preparation

| Intent | Command |
| --- | --- |
| List remote product/Build/locale combinations | `casc products --source remote --region <region>` |
| List local client combinations | `casc products --source local --path <client>` |
| Inspect resolved CASC and Build state | `casc info <target>` |
| Diagnose CDN, root, encoding, archive, cache, and TACT state | `casc diagnose <target>` |
| Download ahead of time | `warmup <target>` |

Ordinary queries prepare their own dependencies. Do not call `warmup` as a prerequisite.

## DB2 And Domain Queries

| Intent | Command |
| --- | --- |
| Inspect table fields and row count | `db2 schema <table> <target>` |
| Read rows by ID | `db2 rows <table> --id <id[,id...]> [--fields <field,...>] <target>` |
| Read multiple IDs | `db2 rows <table> --ids <id,...> <target>` |
| Filter rows | `db2 rows <table> --filter <field=value> [--limit N] <target>` |
| Search localized or text fields | `db2 search <table> --field <field> --query <text> [--limit N] <target>` |
| Follow a numeric relation | `db2 foreign-key <table> --field <field> --value <id> <target>` |
| Stream a large table | `db2 stream <table> [--fields <field,...>] [--filter <field=value>] [--limit N] [--format jsonl|json] <target>` |
| Inspect spell relationships | `spell info --spell-id <id> [--max-depth N] <target>` |
| Detect spell aura behavior | `spell auras --spell-id <id> <target>` |
| Detect summoned NPCs | `spell summons --spell-id <id> [--npc-id <id>] <target>` |
| Read an encounter section tree | `encounter get --journal-encounter-id <id> <target>` |
| Read item metadata | `item get --item-id <id> <target>` |
| Read item model and texture IDs | `item models --item-id <id> [--race-id N] [--gender 0|1] <target>` |
| Read item geosets | `item geosets --item-id <id> <target>` |
| Read item textures | `item textures --item-id <id> <target>` |
| Read a creature display | `creature display (--display-id <id>|--file-data-id <id>) <target>` |
| Find displays for a creature model | `creature model --file-data-id <id> <target>` |
| List decor | `decor list [--limit N] <target>` |
| Read decor | `decor get (--id <id>|--model-file-data-id <id>) <target>` |

Never translate a semantic label such as "name", "description", or "model" directly into a guessed DB2 field. When exact field names are not supplied, run `db2 schema` first and use the returned names. Then use `--fields` to keep large row responses focused.

## Files And Media

| Intent | Command |
| --- | --- |
| Resolve a fileDataID | `file lookup --file-data-id <id> <target>` |
| Search names in the listfile | `file search --query <text> [--limit N] <target>` |
| List by extension | `file extension --extension <ext> [--limit N] <target>` |
| Read file size and hash | `file get (--file-data-id <id>|--filename <name>) <target>` |
| Read or write a raw file | `file get (--file-data-id <id>|--filename <name>) --output <path> <target>` |
| Test existence | `file exists (--file-data-id <id>|--filename <name>) <target>` |
| Inspect content/encoding keys | `file encoding --file-data-id <id> <target>` |
| Export a raw file | `file export (--file-data-id <id>|--filename <name>) --output <path> <target>` |
| Decode a BLP texture | `icon export --file-data-id <id> --format png|webp --output <path> [--mipmap N] [--mask N] <target>` |
| Inspect a local VP9 AVI container | `video demux --input <file> [--output <directory>]` |

`file get` without `--output` returns metadata, not raw bytes in JSON. Use `file export` or `file get --output` when the user needs an artifact.

## Profiles, Cache, And Maintenance

| Intent | Command |
| --- | --- |
| Manage complete targets | `profile list`, `profile show <name>`, `profile set <name> <target>`, `profile remove <name>` |
| Inspect or verify cache | `cache status`, `cache verify` |
| Manage cache | `cache prune`, `cache clear`, `cache config [--max-gb N] [--workers N]` |
| Diagnose installation | `doctor` |
| Update package | `update [--version latest|x.y.z]` |
| Uninstall | `uninstall [--keep-data]` |

Profile flags and explicit target flags are mutually exclusive. Treat cache clearing, Profile removal, update, and uninstall as explicit user actions.
