# wowdata Golden Fixture Rules

Golden fixtures prove that Go `wowdata` command output stays stable across changes.

## Fixture Layout

- `fixtures/golden/go/<group>/<name>.json`: captured Go command output
- `fixtures/golden/diff/<group>/<name>.json`: semantic comparison output

## Naming

Use lowercase names with hyphens:

- `warmup/remote-cn-wow`
- `db2/spellname-id-123`
- `file/lookup-id-456`
- `icon/export-id-789-png`

## Capture Rules

- Capture successful paths and business-meaningful error paths.
- Do not commit machine-specific absolute paths unless the command being tested is specifically about path behavior.
- For exported files, store metadata and content hash, not large binary payloads.
- Record source, region, product, build, command, args, and timestamp.

## Compare Rules

- Compare semantic JSON fields.
- Ignore raw formatting.
- Normalize path separators when path style is not the behavior under test.
- Compare generated artifact hashes for export commands.
- Fail when a required command group has no representative fixture.

## Expected Differences

Intentional behavior changes must be documented next to the fixture with:

- original behavior
- new behavior
- reason
- affected command
