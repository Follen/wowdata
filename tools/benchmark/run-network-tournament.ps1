param(
  [ValidateSet("metadata", "large-range", "chunk", "tail", "root-batch", "coalesce")]
  [string]$Mode = "metadata",
  [int[]]$Candidates = @(24, 36, 44),
  [int]$SuccessesPerCandidate = 10,
  [int]$MaxFailuresPerCandidate = 3,
  [string]$Binary = "analyze/benchmark/wowdata-current.exe",
  [string]$EvidenceRoot = "analyze/benchmark/network-tournament",
  [int]$MetadataWorkers = 36,
  [int]$LargeRangeWorkers = 4,
  [int]$ChunkMiB = 1,
  [string]$Build = "12.0.7.68974",
  [string]$LargeRangeTable = "ItemSparse",
  [string]$LargeRangeRecordID = "25",
  [string]$ChunkURL = "https://blzdist-wow.necdn.leihuo.netease.com/tpr/wow/data/25/18/2518c738f444acfdafde485c7f19e9f2",
  [string]$ChunkSHA256 = "9e0a17ada7e5530ac8ea421901f361aeb5f1ca2e5c4faa6c46da6bc6f002fda5"
)

$ErrorActionPreference = "Stop"
$repo = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
Set-Location $repo
$binaryPath = (Resolve-Path $Binary).Path
$evidencePath = [IO.Path]::GetFullPath((Join-Path $repo $EvidenceRoot))
New-Item -ItemType Directory -Force -Path $evidencePath | Out-Null
$rangeDownloadPath = Join-Path ([IO.Path]::GetTempPath()) ("wowdata-range-download-{0}.exe" -f $PID)
if ($Mode -eq "chunk") {
  & go build -o $rangeDownloadPath ./tools/benchmark/rangedownload
  if ($LASTEXITCODE -ne 0) { throw "failed to build range download calibration helper" }
}

function ConvertTo-ProcessArgument([string]$Value) {
  if ($Value -notmatch '[\s"]') { return $Value }
  return '"' + $Value.Replace('"', '\"') + '"'
}

function Get-Percentile([double[]]$Values, [double]$Percentile) {
  $sorted = @($Values | Sort-Object)
  if ($sorted.Count -eq 0) { return $null }
  return [double]$sorted[[Math]::Max(0, [Math]::Ceiling($Percentile * $sorted.Count) - 1)]
}

function Get-RepoRelativePath([string]$Path) {
  $rootUri = [Uri]($repo.TrimEnd('\') + '\')
  return [Uri]::UnescapeDataString($rootUri.MakeRelativeUri([Uri][IO.Path]::GetFullPath($Path)).ToString())
}

function Get-TextSHA256([string]$Text) {
  $sha = [Security.Cryptography.SHA256]::Create()
  try {
    return [Convert]::ToHexString($sha.ComputeHash([Text.Encoding]::UTF8.GetBytes($Text))).ToLowerInvariant()
  } finally {
    $sha.Dispose()
  }
}

function Normalize-SampleStdout([string]$Text, [string]$HomePath) {
  return $Text.Replace($HomePath.Replace('\', '/'), '<WOWDATA_HOME>').Replace($HomePath, '<WOWDATA_HOME>')
}

function Get-StderrMetrics([string]$Text) {
  $network = $null
  $archiveMs = $null
  $rootMs = $null
  foreach ($line in ($Text -split "`r?`n")) {
    if ($line -match '^network metrics=(\{.*\})$') { $network = $Matches[1] | ConvertFrom-Json }
    if ($line -match '^timing stage=casc-archives-selected duration=([0-9.]+)(ms|s)') {
      $archiveMs = [double]$Matches[1]
      if ($Matches[2] -eq 's') { $archiveMs *= 1000.0 }
    }
    if ($line -match '^timing stage=casc-root-selected duration=([0-9.]+)(ms|s)') {
      $rootMs = [double]$Matches[1]
      if ($Matches[2] -eq 's') { $rootMs *= 1000.0 }
    }
  }
  return [ordered]@{ archiveMs = $archiveMs; rootMs = $rootMs; network = $network }
}

function Write-Report {
  $samples = @(Get-ChildItem -LiteralPath $evidencePath -Directory -ErrorAction SilentlyContinue |
    ForEach-Object {
      $samplePath = Join-Path $_.FullName "sample.json"
      if (Test-Path -LiteralPath $samplePath) { Get-Content -LiteralPath $samplePath -Raw | ConvertFrom-Json }
    } | Sort-Object attempt)
  $successes = @($samples | Where-Object { $_.exitCode -eq 0 })
  $failures = @($samples | Where-Object { $_.exitCode -ne 0 })
  $summary = @()
  foreach ($candidate in $Candidates) {
    $runs = @($successes | Where-Object { $_.candidate -eq $candidate })
    $walls = [double[]]@($runs | ForEach-Object { $_.wallMs })
    $archives = [double[]]@($runs | Where-Object { $null -ne $_.archiveMs } | ForEach-Object { $_.archiveMs })
    $roots = [double[]]@($runs | Where-Object { $null -ne $_.rootMs } | ForEach-Object { $_.rootMs })
    $summary += [ordered]@{
      candidate = $candidate
      successfulRuns = $runs.Count
      failedAttempts = @($failures | Where-Object { $_.candidate -eq $candidate }).Count
      p50WallMs = Get-Percentile $walls 0.50
      p95WallMs = Get-Percentile $walls 0.95
      maxWallMs = if ($walls.Count) { ($walls | Measure-Object -Maximum).Maximum } else { $null }
      p50ArchiveMs = Get-Percentile $archives 0.50
      p95ArchiveMs = Get-Percentile $archives 0.95
      p50RootMs = Get-Percentile $roots 0.50
      p95RootMs = Get-Percentile $roots 0.95
      maxPeakWorkingSetBytes = if ($runs.Count) { ($runs.peakWorkingSetBytes | Measure-Object -Maximum).Maximum } else { $null }
      meanRequests = if ($runs.Count) { ($runs.network.requests | Measure-Object -Average).Average } else { $null }
      meanCanceledRequests = if ($runs.Count) { ($runs.network.canceledRequests | Measure-Object -Average).Average } else { $null }
      maxDuplicatePayloadBytes = if ($runs.Count) { ($runs.network.duplicatePayloadBytes | Measure-Object -Maximum).Maximum } else { $null }
    }
  }
  $hashes = @($successes.normalizedStdoutSha256 | Where-Object { $_ } | Sort-Object -Unique)
  $largeTransferMode = $Mode -in @("large-range", "chunk")
  $workloadBinary = if ($Mode -eq "chunk") { $rangeDownloadPath } else { $binaryPath }
  $report = [ordered]@{
    schema = "wowdata.network-tournament.v1"
    generatedAt = [DateTime]::UtcNow.ToString("o")
    binarySha256 = (Get-FileHash -LiteralPath $binaryPath -Algorithm SHA256).Hash.ToLowerInvariant()
    protocol = [ordered]@{
      mode = $Mode; candidates = $Candidates; successesPerCandidate = $SuccessesPerCandidate
      maxFailuresPerCandidate = $MaxFailuresPerCandidate
      metadataWorkers = $(if ($Mode -eq "metadata") { $Candidates } else { $MetadataWorkers })
      largeRangeWorkers = $(if ($Mode -eq "large-range") { $Candidates } else { $LargeRangeWorkers })
      chunkMiB = $(if ($Mode -eq "chunk") { $Candidates } else { $ChunkMiB })
      rootDeltaBatchKiB = $(if ($Mode -eq "root-batch") { $Candidates } else { $null })
      blteCoalesce = $(if ($Mode -eq "coalesce") { $Candidates } else { $null })
      target = "remote/cn/wow/$Build/zhCN"; build = $Build
      workload = $(
        if ($Mode -eq "large-range") { "on-demand-db2-point-query" }
        elseif ($Mode -eq "chunk") { "single-object-range-resume-write-sha" }
        else { "file-encoding-point-query" }
      )
      command = $(
        if ($Mode -eq "large-range") { "db2 rows $LargeRangeTable --id $LargeRangeRecordID" }
        elseif ($Mode -eq "chunk") { "range-download-calibration --url <build-encoding> --sha256 $ChunkSHA256" }
        else { "file encoding --file-data-id 136121" }
      )
      workloadBinarySha256 = (Get-FileHash -LiteralPath $workloadBinary -Algorithm SHA256).Hash.ToLowerInvariant()
      largeRangeTable = $(if ($Mode -eq "large-range") { $LargeRangeTable } else { $null })
      largeRangeRecordID = $(if ($Mode -eq "large-range") { $LargeRangeRecordID } else { $null })
      chunkObjectSHA256 = $(if ($Mode -eq "chunk") { $ChunkSHA256.ToLowerInvariant() } else { $null })
      independentHome = $true; cacheClearedAfterEach = $true
      orderPolicy = "rotating-latin"
    }
    summary = $summary
    failures = $failures
    samples = $successes
    validation = [ordered]@{
      distinctStdoutHashes = $hashes
      outputStable = $hashes.Count -eq 1
      duplicatePayloadBytesZero = @($successes | Where-Object { $_.network.duplicatePayloadBytes -ne 0 }).Count -eq 0
    }
  }
  $report | ConvertTo-Json -Depth 16 | Set-Content -LiteralPath (Join-Path $evidencePath "report.json") -Encoding utf8
  return $report
}

$targetArguments = @("--source", "remote", "--region", "cn", "--product", "wow", "--build", $Build, "--locale", "zhCN")
if ($Mode -eq "large-range") {
  $arguments = $targetArguments + @("--listfile=false", "--tables", $LargeRangeTable, "db2", "rows", $LargeRangeTable, "--id", $LargeRangeRecordID)
} elseif ($Mode -eq "chunk") {
  $arguments = @()
} else {
  $arguments = $targetArguments + @("file", "encoding", "--file-data-id", "136121")
}
$attempt = @(Get-ChildItem -LiteralPath $evidencePath -Directory -ErrorAction SilentlyContinue).Count
$successfulByCandidate = @{}
$failedByCandidate = @{}
foreach ($candidate in $Candidates) { $successfulByCandidate[$candidate] = 0; $failedByCandidate[$candidate] = 0 }
Get-ChildItem -LiteralPath $evidencePath -Directory -ErrorAction SilentlyContinue | ForEach-Object {
  $samplePath = Join-Path $_.FullName "sample.json"
  if (Test-Path -LiteralPath $samplePath) {
    $sample = Get-Content -LiteralPath $samplePath -Raw | ConvertFrom-Json
    if ($sample.exitCode -eq 0) { $successfulByCandidate[[int]$sample.candidate]++ }
    else { $failedByCandidate[[int]$sample.candidate]++ }
  }
}

$round = [Math]::Floor($attempt / [Math]::Max(1, $Candidates.Count))
while (@($Candidates | Where-Object { $successfulByCandidate[$_] -lt $SuccessesPerCandidate }).Count -gt 0) {
  $rotation = $round % $Candidates.Count
  $roundCandidates = @($Candidates[$rotation..($Candidates.Count - 1)])
  if ($rotation -gt 0) { $roundCandidates += @($Candidates[0..($rotation - 1)]) }
  $round++
  foreach ($candidate in $roundCandidates) {
    if ($successfulByCandidate[$candidate] -ge $SuccessesPerCandidate) { continue }
    if ($failedByCandidate[$candidate] -gt $MaxFailuresPerCandidate) {
      throw "candidate $candidate exceeded failure budget"
    }
    $attempt++
    $sampleDir = Join-Path $evidencePath ("attempt-{0:D3}-{1}-{2}" -f $attempt, $Mode, $candidate)
    $homePath = Join-Path $sampleDir "home"
    New-Item -ItemType Directory -Force -Path $homePath | Out-Null
    $stdoutPath = Join-Path $sampleDir "stdout.json"
    $stderrPath = Join-Path $sampleDir "stderr.log"
    $info = [Diagnostics.ProcessStartInfo]::new()
    $info.FileName = $(if ($Mode -eq "chunk") { $rangeDownloadPath } else { $binaryPath })
    $info.UseShellExecute = $false
    $info.RedirectStandardOutput = $true
    $info.RedirectStandardError = $true
    $info.CreateNoWindow = $true
    $info.Environment["WOWDATA_HOME"] = $homePath
    $info.Environment["WOWDATA_TIMING"] = "1"
    $info.Environment["WOWDATA_METADATA_WORKERS"] = $(if ($Mode -eq "metadata") { "$candidate" } else { "$MetadataWorkers" })
    $info.Environment["WOWDATA_LARGE_RANGE_WORKERS"] = $(if ($Mode -eq "large-range") { "$candidate" } else { "$LargeRangeWorkers" })
    $info.Environment["WOWDATA_RANGE_CHUNK_MIB"] = $(if ($Mode -eq "chunk") { "$candidate" } else { "$ChunkMiB" })
    if ($Mode -eq "tail") { $info.Environment["WOWDATA_ARCHIVE_TAIL_KIB"] = "$candidate" }
    if ($Mode -eq "root-batch") { $info.Environment["WOWDATA_ROOT_DELTA_BATCH_KIB"] = "$candidate" }
    if ($Mode -eq "coalesce") { $info.Environment["WOWDATA_BLTE_COALESCE"] = "$candidate" }
    $sampleArguments = $arguments
    if ($Mode -eq "chunk") {
      $sampleArguments = @(
        "-url", $ChunkURL,
        "-sha256", $ChunkSHA256,
        "-workers", "$LargeRangeWorkers",
        "-chunk-mib", "$candidate",
        "-workdir", (Join-Path $homePath "resume"),
        "-output", (Join-Path $homePath "published/payload")
      )
    }
    $info.Arguments = (($sampleArguments | ForEach-Object { ConvertTo-ProcessArgument $_ }) -join " ")
    $process = [Diagnostics.Process]::new()
    $process.StartInfo = $info
    $started = [DateTime]::UtcNow
    $watch = [Diagnostics.Stopwatch]::StartNew()
    [void]$process.Start()
    $stdoutRead = $process.StandardOutput.ReadToEndAsync()
    $stderrRead = $process.StandardError.ReadToEndAsync()
    $peakWorkingSet = 0L
    while (-not $process.WaitForExit(10)) {
      $process.Refresh()
      if ($process.WorkingSet64 -gt $peakWorkingSet) { $peakWorkingSet = $process.WorkingSet64 }
    }
    $process.Refresh()
    if ($process.WorkingSet64 -gt $peakWorkingSet) { $peakWorkingSet = $process.WorkingSet64 }
    $watch.Stop()
    [IO.File]::WriteAllText($stdoutPath, $stdoutRead.Result, [Text.UTF8Encoding]::new($false))
    [IO.File]::WriteAllText($stderrPath, $stderrRead.Result, [Text.UTF8Encoding]::new($false))
    $parsed = Get-StderrMetrics $stderrRead.Result
    $sample = [ordered]@{
      attempt = $attempt; candidate = $candidate; startedAt = $started.ToString("o")
      metadataWorkers = $(if ($Mode -eq "metadata") { $candidate } else { $MetadataWorkers })
      largeRangeWorkers = $(if ($Mode -eq "large-range") { $candidate } else { $LargeRangeWorkers })
      chunkMiB = $(if ($Mode -eq "chunk") { $candidate } else { $ChunkMiB })
      wallMs = $watch.Elapsed.TotalMilliseconds; cpuMs = $process.TotalProcessorTime.TotalMilliseconds
      peakWorkingSetBytes = [int64]$peakWorkingSet; exitCode = $process.ExitCode
      archiveMs = $parsed.archiveMs; rootMs = $parsed.rootMs; network = $parsed.network
      stdoutSha256 = (Get-FileHash -LiteralPath $stdoutPath -Algorithm SHA256).Hash.ToLowerInvariant()
      normalizedStdoutSha256 = Get-TextSHA256 (Normalize-SampleStdout $stdoutRead.Result $homePath)
      stdout = Get-RepoRelativePath $stdoutPath; stderr = Get-RepoRelativePath $stderrPath
    }
    $sample | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath (Join-Path $sampleDir "sample.json") -Encoding utf8
    if ($process.ExitCode -eq 0) { $successfulByCandidate[$candidate]++ } else { $failedByCandidate[$candidate]++ }
    Remove-Item -LiteralPath $homePath -Recurse -Force -ErrorAction SilentlyContinue
    $report = Write-Report
    Write-Host ("candidate={0} exit={1} wallMs={2:N1} successes={3}/{4} failures={5}" -f $candidate, $process.ExitCode, $watch.Elapsed.TotalMilliseconds, $successfulByCandidate[$candidate], $SuccessesPerCandidate, $failedByCandidate[$candidate])
  }
}

$final = Write-Report
$manifestFiles = Get-ChildItem -LiteralPath $evidencePath -Recurse -File | Where-Object { $_.Name -ne "sha256-manifest.json" } | Sort-Object FullName | ForEach-Object {
  [ordered]@{ path = Get-RepoRelativePath $_.FullName; bytes = $_.Length; sha256 = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant() }
}
[ordered]@{ schema = "wowdata.evidence-sha256.v1"; files = @($manifestFiles) } | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath (Join-Path $evidencePath "sha256-manifest.json") -Encoding utf8
if ($Mode -eq "chunk") { Remove-Item -LiteralPath $rangeDownloadPath -Force -ErrorAction SilentlyContinue }
$final | ConvertTo-Json -Depth 6
if (-not $final.validation.outputStable -or -not $final.validation.duplicatePayloadBytesZero) { exit 1 }
