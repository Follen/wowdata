# Integration benchmark matrix

This generator is the exhaustive integration and performance layer above the
small versioned golden suite. It discovers the Cobra leaf command tree at run
time and expands every leaf across each catalogued Build, `local` and `remote`
sources, the four cache protocols, and four behavioral scenarios.

Generate a matrix:

```powershell
powershell -NoProfile -File tools/benchmark/generate-integration-benchmark.ps1
```

An input without a `scenario` is a `success` case. A case `target` may be an
exact Build catalog name, `product:<name>`, or `*`; an optional `source` field
(`local` or `remote`) binds evidence to the measured transport. Selection
prefers source-specific evidence, then source-neutral evidence, and always
prefers exact Build over product-family over portable evidence. Missing-args and
representative-error probes are generated from the live command path and run
without a data corpus. Commands with a real empty-set contract receive typed
maximum-ID or sentinel-query probes. Add richer Build-specific `success` cases
to the input catalog when portable IDs do not apply. Missing inputs remain
visible, non-executable matrix
cells; they are not replaced with help-only smoke tests. Build-specific
impossibilities belong in `nonTargets` and require both a reason and evidence.
A non-target may also set `source` to exclude only one transport; negative
contract probes remain executable even when success data is absent.

Every executable cell records output identity, exit behavior, timing, memory,
GC, network, disk, stage, cache-delta, and fallback metrics. Destructive
commands receive isolated HOME, cache, output, and install roots.

`update` and `uninstall` success cells use an `isolated-npm-success` fixture.
For each cell the runner creates a synthetic `npm.cmd` below `installRoot` and
redirects `PATH`, user directories, npm prefix, and npm cache into that run
root. This exercises wowdata's npm command construction and response contract
without contacting a registry or changing a user installation.

Run selected executable cells:

```powershell
powershell -NoProfile -File tools/benchmark/run-integration-benchmark.ps1 `
  -Binary analyze/benchmark/wowdata-current.exe `
  -Matrix analyze/benchmark/integration/matrix.json `
  -CommandPattern '^cache status$' `
  -CasePattern 'retail-cn-local-independent-cold-success$' `
  -Protocols independent-cold `
  -MaxCases 1 `
  -Repetitions 1
```

The runner executes only cells marked `executable`, expands
`${WOWDATA_GAME_DIR}` and `${RUN_ROOT}`, runs setup commands in the same isolated
environment, and writes `wowdata.integration-benchmark-run.v1`. A matrix schema
or required-field mismatch terminates with exit code 1.
