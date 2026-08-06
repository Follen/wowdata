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
- Every remote target capture must pass an explicit immutable `--build` version,
  build ID, or config key. `latest` is rejected because it changes the fixture
  input when the CDN publishes a Build.
- Remote discovery commands that have no Build input, such as `casc products`,
  use an explicit comparison mode. `remote-products-v1` freezes product order,
  region, and locales while validating version/build/key consistency without
  pretending that two different published Builds are the same fixed input.

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

## Full Regression Runner

Run the repository-backed runner from the project root:

```powershell
powershell -File tools/golden/run-all.ps1
```

It replays every capture under `fixtures/golden/go`, writes actual outputs and a
machine-readable report under `analyze/golden`, generates the root manifest, and
invokes `wowdata golden compare --all`. Missing command groups are reported as a
coverage gap and return exit code `2`; comparison failures return exit code `1`.
The `analyze/` outputs are reproducible and are not production DB2/CASC cache.

## Build Migrations

- 2026-08-05: Classic Era `us/enUS` moved from unavailable Build
  `1.15.9.68940` (`f622d60e2229df3f290a83452599308e`) to pinned Build
  `1.15.9.69109` (`9f9686341092239cfa4812a0ba153dc6`). A direct r31 probe of
  the old version returned `status=no_build`; fixtures were recaptured as a new
  fixed input rather than compared across Builds. Retail remote target captures
  are pinned to `12.0.7.68974`.
