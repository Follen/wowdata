# wowdata Benchmark Evidence

Run from the repository root:

```powershell
powershell -File tools/benchmark/run.ps1
```

The runner records clean Go tests, race checks for the read/transport core,
benchmark output, and the full golden runner under `analyze/benchmark`. The
directory is ignored, content-addressed by the report hashes, and contains no
production decoded-table cache. A public benchmark submission must additionally
record the fixed Build/locale, CDN/cache state, output hashes, p50/p95 samples,
CPU/memory/disk measurements, and a baseline binary using the same protocol.

Run the bounded local calibration before formal samples:

```powershell
go run ./tools/benchmark/calibrate `
  -output analyze/benchmark/calibration/local.json `
  -temp-dir analyze/benchmark/calibration/temp
```

The default probe uses a disposable 64 MiB file and removes it before exit. It
records physical/logical cores, GOMAXPROCS, available memory, disk throughput
and random-read latency, SHA-256 and JSON throughput, plus synthetic
BLTE/WDC/DBC/DBD/BLP startup measurements. For final corpus evidence, pass
representative real files with `-blte`, `-wdc`, `-dbc`, `-dbd`, and `-blp`;
each result records its absolute source path and decode mode. This command does
not write parsed tables, indexes, or other reusable codec results.

Run the bounded interleaved network calibration with:

```powershell
go run ./tools/benchmark/networkcalibrate `
  -metadata-tournament analyze/benchmark/metadata-tournament-r35-final/report.json `
  -large-range-tournament analyze/benchmark/large-range-tournament-r36-demand-final/report.json `
  -chunk-tournament analyze/benchmark/chunk-tournament-r36-demand-final/report.json `
  -output analyze/benchmark/network-calibration-r36-demand/report.json
```

It rotates CN CDN metadata, CN CDN single/multi-connection Range, GitHub API,
and raw DBD probes across three rounds. Each sample uses a fresh Transport and
records DNS, TCP connect, TLS, TTFB, HTTP protocol, response bytes, body/wall
throughput, status and hash. Default CDN traffic is 8 MiB per round; payloads
remain in memory. The report imports metadata, on-demand large-range, and
single-object chunk/resume tournaments. A zero-failure candidate within 10% of
the fastest p50 wins by p95, then p50, peak working set, and candidate value.
The large-range workload must be a real on-demand data command; chunk/resume
uses the production Range-to-part-to-SHA-to-atomic-publication path. Full
`warmup` measurements are valid only for the `warmup/corpus` command class and
must not select point-query or composite/export defaults.

Assemble the current evidence into a command-sorted draft report with:

```powershell
go run ./tools/benchmark/report `
  -matrix-report analyze/benchmark/<matrix-run>/report.json `
  -floor-index analyze/benchmark/<matrix-run>/floors/index.json `
  -local-calibration analyze/benchmark/<calibration>/local.json `
  -network-calibration analyze/benchmark/<network-calibration>/report.json `
  -metadata-tournament analyze/benchmark/metadata-tournament-r35-final/report.json `
  -large-range-tournament analyze/benchmark/large-range-tournament-r36-demand-final/report.json `
  -chunk-tournament analyze/benchmark/chunk-tournament-r36-demand-final/report.json `
  -output analyze/benchmark/<matrix-run>/final-report
```

The report generator enumerates the live Cobra command tree and requires one
explicit formal matrix report plus its identity-matched case/protocol floor
index. It does not scan the evidence tree and guess a latest run. Additional
calibration, DB2 corpus, resume, regression, and network-tournament evidence is
selected by the explicit flags. Missing measurements remain explicitly
`missing`; they are never converted into a passing regression or floor result.

Run the fixed real-CDN encounter protocol with:

```powershell
powershell -File tools/benchmark/run-live.ps1 -Binary analyze/benchmark/wowdata-current.exe
```

It records every process exit, stdout/stderr hash, wall and CPU time, peak
working set, cache delta, p50/p95, manifest cardinality, PNG signatures and
SHA-256 verification under `analyze/benchmark/live-encounter`.

Generate the benchmark/regression matrix directly from the live Cobra command
tree and the golden manifest with:

```powershell
powershell -NoProfile -File tools/benchmark/generate-command-matrix.ps1
```

The output contains every current leaf command, its fixture or generated
isolated smoke case, independent/shared cold plus warm/repeat-warm protocols,
target applicability, and filesystem/install isolation policy. `update` and
`uninstall` are emitted as record-only plans so matrix generation never mutates
the active npm installation. Each also has a `generated-isolated-install` case
that runs the real CLI lifecycle only when the runner receives
`-ExecuteRecordOnly`. Target-data entries separately report required,
observed, and missing Retail CN / Classic Era US coverage; fixture gaps remain
visible until a real fixed-input case is added.

Generate the richer integration matrix used for primary performance and bug
coverage with:

```powershell
powershell -NoProfile -File tools/benchmark/generate-integration-benchmark.ps1
```

This matrix is independent of the golden fixture list. It enumerates every
Cobra leaf, every catalogued local/remote Build, all four cache protocols, and
success/missing-args/empty-result/representative-error scenarios. The catalog
contains the two pinned public targets plus every Build discovered in the
configured local client snapshot. A missing real corpus case is emitted as an
explicit `coverageGap` with no executable result; it is never counted as a
passing smoke test. `goldenLayer` continues to point at
`fixtures/golden/manifest.json` for the small frozen semantic checks.

Run selected executable integration cells with isolated protocol state:

```powershell
powershell -NoProfile -File tools/benchmark/run-integration-benchmark.ps1 `
  -Binary analyze/benchmark/wowdata-current.exe `
  -Matrix analyze/benchmark/integration/matrix.json `
  -CommandPattern '^cache status$' -MaxCases 1
```

The integration runner validates the matrix schema and summary counts before
execution, expands local path placeholders, executes setup commands, and
writes a `wowdata.integration-benchmark-run.v1` report. Coverage gaps remain
non-executable and are reported rather than converted to passes.

Execute any slice of the generated matrix with per-process resource, hash,
golden, output, and cache-delta evidence:

```powershell
powershell -NoProfile -File tools/benchmark/run-command-matrix.ps1 `
  -Binary analyze/benchmark/wowdata-current.exe `
  -Matrix analyze/benchmark/command-matrix/matrix.json `
  -CommandPattern '^cache status$' -MaxCases 1
```

The default is one lightweight repetition. Formal fixed-input evidence must use
`-Formal -ExecuteRecordOnly -BaselineBinary <path> -Repetitions 10` (or more)
for reportable runs. Formal mode rejects missing baselines, protocol subsets,
case slicing, skipped record-only commands, and fewer than ten repetitions
before starting any sample. Each repetition starts with fresh independent and
shared-build state, runs the protocol DAG in order, and alternates whether the
current or baseline binary runs first. Samples carry the run ID, repository
revision, matrix SHA-256, binary SHA-256, stable input-derived case key, and
repetition number.

Pass `-BaselineBinary <path>` to execute that binary through the same protocol
and isolated state progression. Baseline output comparison is distinct from
the current binary's cross-protocol semantic hash. Stdout and the complete
relative-path/size/SHA-256 output-file manifest are both hard gates for golden,
baseline, and cross-protocol equality. A missing or additional output file is a
failure even when stdout is unchanged.

The generated matrix declares an output comparison policy per command. Data
queries use `strict-cross-protocol`. Commands whose protocol DAG intentionally
changes observable cache, profile, or install state use
`stable-within-protocol`: normalized stdout and the artifact manifest must
match across independent repetitions of the same protocol. The first
repetition records `outputComparisonReferenceAvailable=false`; formal evidence
requires later repetitions. Baseline comparison and repeat-warm cache identity
remain mandatory under both policies.

Repeat-warm cache stability is content-addressed. Each repeat-warm candidate
records the cache tree before and after as a sorted
relative-path/length/SHA-256 manifest, both manifest hashes, and explicit
added/removed/changed path lists. Other protocols avoid this full-tree hashing
cost and retain their byte-delta telemetry.
`repeatWarmCacheStable` requires identical manifests; a same-size payload
rewrite fails even when `cacheDeltaBytes` is zero.

Within each repetition the runner executes all selected independent-cold cases,
then shared-build-cold, warm, and repeat-warm. The next repetition gets new
shared homes, so formal samples are independent workflow rounds instead of one
progressively warmer cache.

Real cross-Build inputs are configured in `target-inputs.json`; the matrix
generator merges them with golden fixtures and continues to report unresolved
command/target pairs. A Build-specific command with no real input may be listed
under `nonTargets` only with both a reason and an evidence path. These audited
exceptions remain separate from observed target coverage.

The runner reads manifests explicitly as UTF-8, preserves the basename for
file-valued `--output` flags, and redirects directory-valued outputs beneath the
case evidence root. Timing evidence includes parsed `networkMetrics` and
`resourceMetrics` objects when emitted by the CLI.

Run the local executable protocol/delta/baseline smoke assertions with:

```powershell
powershell -NoProfile -File tools/benchmark/smoke-command-matrix-runner.ps1 `
  -Binary analyze/benchmark/wowdata-current.exe `
  -Matrix analyze/benchmark/command-matrix/matrix.json
```

Run the network-free output manifest positive/negative smoke with:

```powershell
powershell -NoProfile -File tools/benchmark/smoke-command-matrix-artifacts.ps1
```

Run the same-size cache mutation positive/negative smoke with:

```powershell
powershell -NoProfile -File tools/benchmark/smoke-command-matrix-cache-identity.ps1
```

Run real `update`/`uninstall` handler isolation with a sandboxed npm shim:

```powershell
powershell -NoProfile -File tools/benchmark/smoke-command-matrix-install-isolation.ps1 `
  -Binary analyze/benchmark/wowdata-current.exe
```

Formal and baseline comparison policies:

```powershell
powershell -NoProfile -File tools/benchmark/smoke-command-matrix-policies.ps1
```

Network payload gates (HTTP body bytes only; headers are excluded):

```powershell
powershell -NoProfile -File tools/benchmark/smoke-command-matrix-network-gates.ps1
```

For install-destructive cases the runner creates candidate- and round-specific
`USERPROFILE`, `HOME`, `WOWDATA_HOME`, `AGENTS_HOME`, npm prefix/cache/config,
AppData, temp, install, and PATH roots below the evidence directory. PATH is
rebuilt from those roots, npm/node tool directories, and Windows system
directories; the user's full PATH is not inherited. `-NPMShim` is a smoke-test
hook: the supplied file is copied into each sandbox as `npm.cmd` before the
real wowdata command executes it.

The report generator never combines raw timings from different stable case
keys, run IDs, revisions, or binary hashes. Every command retains a sorted
`cases[]` entry for each fixed input/target/size; each entry has independent
protocol percentiles and regression state. Command-level status summarizes all
of those cases, while heterogeneous raw timings are never folded into one
percentile series.

Compute a command's dynamic theoretical lower bound from its measured execution
DAG with:

```powershell
go run ./tools/benchmark/floor -input analyze/benchmark/<run>/floor-input.json -output analyze/benchmark/<run>/floor.json
```

Each DAG node records its dependencies, unavoidable latency/output, and unique
network, disk, and CPU-equivalent work. Inputs using only the original v1 rate
fields remain valid: their work is assigned to the default `network`, `disk`,
and `cpu` resource classes. Commands with independently calibrated resources
can define `calibration.resourceClasses` entries containing `resource` and
`bytesPerSecond`, then select them with the node-level
`networkResourceClass`, `diskResourceClass`, or `cpuResourceClass` fields. A
node can instead supply the corresponding `networkBytesPerSecond`,
`diskBytesPerSecond`, or `cpuWorkBytesPerSecond` override; every node sharing a
class must resolve to the same throughput.

Work is summed within each resource class to enforce its finite capacity, while
different classes (for example `cn-cdn` and `github-dbd`) are allowed to overlap.
The calculator takes the maximum of the dependency critical path and every
class capacity floor; it never adds independently overlapping stages. The JSON
report lists the sorted `classCapacityFloors`, the
`dominantResourceClass`, the exact DAG critical path, and the p50/p95
`1.25x`/`1.50x` gate results. `aggregateResourceFloor` remains present for v1
consumers and reports the maximum class floor for each legacy resource kind.

Measure process startup for the exact benchmark binary without including Cobra
rendering or a data command:

```powershell
go run ./tools/benchmark/processprobe -binary analyze/benchmark/wowdata-current.exe -samples 10 -output analyze/benchmark/calibration/process-start.json
```

The child exits at `main` entry only when `WOWDATA_PROCESS_START_PROBE=1`; normal
commands never set this probe variable. The report retains every launch sample
and the binary SHA-256 used by the corresponding floor calculation.

Generate auditable floors for every stable case/protocol in one formal matrix
report with:

```powershell
go run ./tools/benchmark/floor `
  -matrix-report analyze/benchmark/<matrix-run>/report.json `
  -local-calibration analyze/benchmark/<calibration>/local.json `
  -network-calibration analyze/benchmark/<network-calibration>/report.json `
  -process-calibration analyze/benchmark/<calibration>/process-start.json `
  -output-dir analyze/benchmark/<matrix-run>/floors
```

All calibration paths are explicit; the generator has no release- or
machine-specific hardcoded evidence. It groups only by `stableCaseKey` and
protocol and writes an `index.json` entry for every such group. Available
entries point to reproducible floor input/output pairs. Failed samples,
incomplete resource-metrics-v2 work coverage, a missing exact
`resourceClass`/`workUnit` calibration, absent network stage dependencies or
pool capacities, and network stage bytes that do not exactly close to the
sample's total unique bytes produce an explicit `missing` entry. In particular,
unattributed DBD/GitHub traffic is not guessed into a CDN class.
Generated outputs use `wowdata.command-case-floor.v1`; the index carries their
stable-case/protocol identity. This keeps the command-level report scanner from
silently collapsing heterogeneous case floors into one command-wide number.

Network nodes are formed from detailed non-overlapping `casc-*` stage metrics.
Request waves use the measured pool capacity and connection hard limit; body
throughput and TTFB come from separate calibration fields so RTT is not counted
twice. Network dependencies must be emitted explicitly by telemetry and are
never inferred from stderr line order. Metadata and large-range classes retain
their own calibration while also sharing the calibrated CDN capacity.

CPU, codec, and storage floors use resource-metrics-v2 counters with mixed
units such as bytes, decoded bytes, fields, rows, probes, and pixels. Every
counter must match a calibration entry with the same `resourceClass` and
`workUnit`. Observed `cpuMilliseconds` and wall time are ratio/audit evidence
only and never become fixed cost or a proxy work counter.
