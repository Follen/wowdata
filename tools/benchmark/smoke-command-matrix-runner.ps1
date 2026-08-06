param(
  [string]$Binary = "analyze/benchmark/wowdata-current.exe",
  [string]$Matrix = "analyze/benchmark/command-matrix/matrix.json",
  [string]$OutputRoot = "analyze/benchmark/command-matrix-runner-smoke",
  [int]$Repetitions = 2
)

$ErrorActionPreference = "Stop"
$repo = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
Set-Location $repo
$runner = Join-Path $PSScriptRoot "run-command-matrix.ps1"
& powershell -NoProfile -File $runner -Binary $Binary -BaselineBinary $Binary -Matrix $Matrix -OutputRoot $OutputRoot -CommandPattern '^(profile set|profile show)$' -MaxCases 2 -Repetitions $Repetitions *> $null
if ($LASTEXITCODE -ne 0) { throw "matrix runner exited $LASTEXITCODE" }

$reportPath = Join-Path ([IO.Path]::GetFullPath((Join-Path $repo $OutputRoot))) "report.json"
$report = Get-Content -Raw $reportPath | ConvertFrom-Json
$actualOrder = @($report.samples.protocol)
$oneRound = @("independent-cold", "independent-cold", "shared-build-cold", "shared-build-cold", "warm", "warm", "repeat-warm", "repeat-warm")
$expectedOrder = @(for ($i = 0; $i -lt $Repetitions; $i++) { $oneRound })
if (($actualOrder -join ',') -ne ($expectedOrder -join ',')) {
  throw "protocol order $($actualOrder -join ',') does not match $($expectedOrder -join ',')"
}
foreach ($sample in $report.samples) {
  if ($sample.status -ne 'pass') { throw "$($sample.protocol) sample failed" }
  if (-not $sample.baseline.outputMatch) { throw "$($sample.protocol) baseline mismatch" }
  if ($null -eq $sample.ttfbMilliseconds) { throw "$($sample.protocol) missing TTFB" }
  if (-not $sample.stableCaseKey -or -not $sample.runID -or -not $sample.revision -or -not $sample.binarySha256) { throw "$($sample.protocol) missing run identity" }
  if (-not $sample.outputComparisonArtifactMatch -or -not $sample.baseline.artifactMatch) { throw "$($sample.protocol) artifact manifest mismatch" }
}
$independent = @($report.samples | Where-Object protocol -eq 'independent-cold')
foreach ($sample in $independent) {
  if (@($sample.cleanup).Count -ne 2 -or @($sample.cleanup | Where-Object status -ne 'removed').Count -ne 0) { throw "independent cleanup is incomplete for $($sample.commandPath)" }
  foreach ($record in @($sample.cleanup)) { if (Test-Path -LiteralPath $record.path) { throw "cleanup path remains: $($record.path)" } }
}
if ($report.cleanup.status -ne 'complete' -or @($report.cleanup.records).Count -lt (2 * $independent.Count)) { throw 'cleanup report is incomplete' }
$leftoverHomes = @(Get-ChildItem -LiteralPath ([IO.Path]::GetFullPath((Join-Path $repo $OutputRoot))) -Recurse -Directory -Filter home | Where-Object { $_.FullName -match '[\\/](shared|bshare|cases|bline)[\\/]' })
if ($leftoverHomes.Count -ne 0) { throw "matrix retained $($leftoverHomes.Count) isolated HOME directories" }
$expectedCommands = @('profile set', 'profile show')
foreach ($protocol in @('independent-cold', 'shared-build-cold', 'warm', 'repeat-warm')) {
  $commands = @($report.samples | Where-Object protocol -eq $protocol | ForEach-Object commandPath)
  $expectedRepeated = @(for ($i = 0; $i -lt $Repetitions; $i++) {
    $offset = $i % $expectedCommands.Count
    for ($j = 0; $j -lt $expectedCommands.Count; $j++) { $expectedCommands[($offset + $j) % $expectedCommands.Count] }
  })
  if (($commands -join ',') -ne ($expectedRepeated -join ',')) { throw "$protocol command order is $($commands -join ',')" }
}
foreach ($sample in $report.samples) {
  $expectedCandidateOrder = if ($sample.repetition % 2 -eq 0) { 'baseline,current' } else { 'current,baseline' }
  if ($sample.candidateOrder -ne $expectedCandidateOrder) { throw "repetition $($sample.repetition) candidate order is $($sample.candidateOrder)" }
}
foreach ($repetition in 1..$Repetitions) {
  foreach ($protocol in @('independent-cold', 'shared-build-cold', 'warm', 'repeat-warm')) {
    $ordinals = @($report.samples | Where-Object { $_.repetition -eq $repetition -and $_.protocol -eq $protocol } | ForEach-Object roundCaseOrdinal)
    if (($ordinals -join ',') -ne '0,1') { throw "repetition $repetition protocol $protocol case ordinals are $($ordinals -join ',')" }
  }
}
foreach ($repeat in @($report.samples | Where-Object protocol -eq 'repeat-warm')) {
  if ($repeat.cacheDeltaBytes -ne 0 -or -not $repeat.repeatWarmCacheStable -or $repeat.cacheManifestBeforeSha256 -ne $repeat.cacheManifestAfterSha256) {
    throw "repeat-warm cache changed by $($repeat.cacheDeltaBytes) bytes for $($repeat.commandPath)"
  }
  if ($repeat.cacheManifestDiff.added.Count -ne 0 -or $repeat.cacheManifestDiff.removed.Count -ne 0 -or $repeat.cacheManifestDiff.changed.Count -ne 0) { throw "repeat-warm cache manifest diff is non-empty for $($repeat.commandPath)" }
}
$setupDirectories = @(Get-ChildItem -LiteralPath ([IO.Path]::GetFullPath((Join-Path $repo $OutputRoot))) -Recurse -Directory -Filter setup)
$definition = Get-Content -Raw $Matrix | ConvertFrom-Json
$setupCaseCount = @(foreach ($command in $definition.commands | Where-Object { $_.path -match '^(profile set|profile show)$' }) { foreach ($case in @($command.cases)) { if (@($case.setup | Where-Object { $null -ne $_ }).Count -gt 0) { $case } } }).Count
$expectedSetupDirectories = 4 * $setupCaseCount * $Repetitions
if ($setupDirectories.Count -ne $expectedSetupDirectories) {
  throw "expected current+baseline setup only in independent/shared cold ($expectedSetupDirectories directories), found $($setupDirectories.Count)"
}
[ordered]@{ schema = 'wowdata.command-matrix-runner-smoke.v1'; status = 'pass'; report = $reportPath.Replace('\','/'); protocols = $actualOrder } | ConvertTo-Json
