param(
  [string]$Binary = "analyze/benchmark/wowdata-current.exe",
  [string]$EvidenceRoot = "analyze/benchmark/resume-kill",
  [string]$WowdataHome,
  [string]$BuildKey,
  [string]$TargetName = "build_encoding",
  [double[]]$Fractions = @(0.25, 0.50, 0.90),
  [string]$Region = "cn",
  [string]$Product = "wow",
  [string]$Build = "12.0.7.68974",
  [string]$Locale = "zhCN",
  [string]$URL = "https://blzdist-wow.necdn.leihuo.netease.com/tpr/wow/data/25/18/2518c738f444acfdafde485c7f19e9f2",
  [string]$ExpectedSHA256 = "9e0a17ada7e5530ac8ea421901f361aeb5f1ca2e5c4faa6c46da6bc6f002fda5",
  [int64]$TargetBytes = 184690499,
  [int]$ExpectedChunkMiB = 8,
  [int]$DeadlineSeconds = 300,
  [int]$ResumeTimeoutSeconds = 300
)

$ErrorActionPreference = "Stop"
if (-not $WowdataHome -or -not $BuildKey -or -not $URL -or -not $ExpectedSHA256) { throw "WowdataHome, BuildKey, URL, and ExpectedSHA256 are required" }
if ($ExpectedChunkMiB -lt 1 -or $ExpectedChunkMiB -gt 64) { throw "ExpectedChunkMiB must be between 1 and 64" }
if ($DeadlineSeconds -lt 1 -or $ResumeTimeoutSeconds -lt 1) { throw "timeouts must be positive" }
$repo = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
Set-Location $repo
$binaryPath = (Resolve-Path $Binary).Path
$homePath = [IO.Path]::GetFullPath((Join-Path $repo $WowdataHome))
New-Item -ItemType Directory -Force -Path $homePath | Out-Null
$buildRoot = Join-Path $homePath "cache/casc/builds/$BuildKey"
$targetPath = Join-Path $buildRoot $TargetName
$resumeRoot = Join-Path $homePath "cache/casc/resume"
$quotaState = Join-Path $homePath "cache/casc/quota.json"
$evidencePath = [IO.Path]::GetFullPath((Join-Path $repo $EvidenceRoot))
if ((Test-Path -LiteralPath $evidencePath) -and @(Get-ChildItem -LiteralPath $evidencePath -Force).Count -gt 0) {
  throw "evidence directory must be empty: $evidencePath"
}
New-Item -ItemType Directory -Force $evidencePath, $resumeRoot | Out-Null
$targetSize = $TargetBytes
$expectedHash = $ExpectedSHA256.ToLowerInvariant()
$rangeDownloadPath = Join-Path ([IO.Path]::GetTempPath()) ("wowdata-resume-download-{0}.exe" -f $PID)
& go build -o $rangeDownloadPath ./tools/benchmark/rangedownload
if ($LASTEXITCODE -ne 0) { throw "failed to build range download helper" }

function ConvertTo-ProcessArgument([string]$Value) {
  if ($Value -notmatch '[\s"]') { return $Value }
  return '"' + $Value.Replace('"', '\"') + '"'
}

function Start-Downloader {
  $info = [Diagnostics.ProcessStartInfo]::new()
  $info.FileName = $rangeDownloadPath
  $info.UseShellExecute = $false
  $info.RedirectStandardOutput = $true
  $info.RedirectStandardError = $true
  $info.CreateNoWindow = $true
  $info.Environment["WOWDATA_HOME"] = $homePath
  [void]$info.Environment.Remove("WOWDATA_RANGE_CHUNK_MIB")
  [void]$info.Environment.Remove("WOWDATA_LARGE_RANGE_WORKERS")
  [void]$info.Environment.Remove("WOWDATA_METADATA_WORKERS")
  $arguments = @("-url", $URL, "-sha256", $expectedHash, "-workdir", $resumeRoot, "-output", $targetPath)
  $info.Arguments = (($arguments | ForEach-Object { ConvertTo-ProcessArgument $_ }) -join " ")
  $process = [Diagnostics.Process]::new()
  $process.StartInfo = $info
  [void]$process.Start()
  return [PSCustomObject]@{
    Process = $process
    Stdout = $process.StandardOutput.ReadToEndAsync()
    Stderr = $process.StandardError.ReadToEndAsync()
  }
}

function Get-ResumeSnapshot {
  $states = @(Get-ChildItem -LiteralPath $resumeRoot -Filter *.json -File -ErrorAction SilentlyContinue)
  foreach ($file in $states) {
    try {
      $state = [IO.File]::ReadAllText($file.FullName) | ConvertFrom-Json
      if ([int64]$state.size -ne $targetSize) { continue }
      $complete = @($state.complete)
      $completed = @($complete | Where-Object { $_ }).Count
      return [PSCustomObject]@{ File = $file; State = $state; Completed = $completed; Total = $complete.Count }
    } catch {
      continue
    }
  }
  return $null
}

function Get-CompletedBytes($State) {
  $bytes = 0L
  for ($index = 0; $index -lt $State.complete.Count; $index++) {
    if (-not $State.complete[$index]) { continue }
    $start = [int64]$index * [int64]$State.chunkSize
    $bytes += [Math]::Min([int64]$State.chunkSize, [int64]$State.size - $start)
  }
  return $bytes
}

$samples = @()
foreach ($fraction in $Fractions) {
  if ($fraction -le 0 -or $fraction -ge 1) { throw "fraction must be between 0 and 1: $fraction" }
  Remove-Item -LiteralPath $targetPath -Force -ErrorAction SilentlyContinue
  Remove-Item -LiteralPath $quotaState -Force -ErrorAction SilentlyContinue
  Get-ChildItem -LiteralPath $resumeRoot -File -ErrorAction SilentlyContinue | Remove-Item -Force

  $started = Start-Downloader
  $deadline = [DateTime]::UtcNow.AddSeconds($DeadlineSeconds)
  $snapshot = $null
  while ([DateTime]::UtcNow -lt $deadline -and -not $started.Process.HasExited) {
    $snapshot = Get-ResumeSnapshot
    if ($snapshot -and $snapshot.Total -gt 0 -and ($snapshot.Completed / $snapshot.Total) -ge $fraction) { break }
    Start-Sleep -Milliseconds 20
  }
  if (-not $snapshot -or $snapshot.Total -eq 0 -or ($snapshot.Completed / $snapshot.Total) -lt $fraction) {
    if (-not $started.Process.HasExited) { $started.Process.Kill($true); $started.Process.WaitForExit() }
    throw "download did not reach fraction $fraction before exit/deadline"
  }
  $started.Process.Kill($true)
  $started.Process.WaitForExit()
  $killStdout = $started.Stdout.Result
  $killStderr = $started.Stderr.Result
  $snapshot = Get-ResumeSnapshot
  if (-not $snapshot -or $snapshot.Total -eq 0) { throw "resume checkpoint missing after kill for fraction $fraction" }
  $expectedChunkBytes = [int64]$ExpectedChunkMiB * 1MB
  if ([int64]$snapshot.State.chunkSize -ne $expectedChunkBytes) {
    throw "resume chunk size is $($snapshot.State.chunkSize), want $expectedChunkBytes"
  }
  $completedBytes = Get-CompletedBytes $snapshot.State
  $label = ("{0:D2}" -f [int][Math]::Round($fraction * 100))
  $stateEvidence = Join-Path $evidencePath "fraction-$label.state.json"
  Copy-Item -LiteralPath $snapshot.File.FullName -Destination $stateEvidence -Force
  [IO.File]::WriteAllText((Join-Path $evidencePath "fraction-$label.killed.stdout.json"), $killStdout, [Text.UTF8Encoding]::new($false))
  [IO.File]::WriteAllText((Join-Path $evidencePath "fraction-$label.killed.stderr.log"), $killStderr, [Text.UTF8Encoding]::new($false))

  $resumed = Start-Downloader
  if (-not $resumed.Process.WaitForExit($ResumeTimeoutSeconds * 1000)) {
    $resumed.Process.Kill($true)
    $resumed.Process.WaitForExit()
    throw "resumed download timed out for fraction $fraction"
  }
  $resumeStdout = $resumed.Stdout.Result
  $resumeStderr = $resumed.Stderr.Result
  [IO.File]::WriteAllText((Join-Path $evidencePath "fraction-$label.resumed.stdout.json"), $resumeStdout, [Text.UTF8Encoding]::new($false))
  [IO.File]::WriteAllText((Join-Path $evidencePath "fraction-$label.resumed.stderr.log"), $resumeStderr, [Text.UTF8Encoding]::new($false))
  if ($resumed.Process.ExitCode -ne 0) { throw "resumed command exited $($resumed.Process.ExitCode) for fraction $fraction" }
  $line = @($resumeStderr -split "`r?`n" | Where-Object { $_ -match "method=range-resume" -and $_ -match "bytes=$targetSize(?:\s|$)" }) | Select-Object -Last 1
  if (-not $line -or $line -notmatch 'reusedBytes=(\d+) downloadedBytes=(\d+) reusedChunks=(\d+) downloadedChunks=(\d+)') {
    throw "missing resume accounting for fraction $fraction"
  }
  $reusedBytes = [int64]$Matches[1]
  $downloadedBytes = [int64]$Matches[2]
  $reusedChunks = [int]$Matches[3]
  $downloadedChunks = [int]$Matches[4]
  if ($reusedBytes -ne $completedBytes -or $reusedBytes + $downloadedBytes -ne $targetSize) {
    throw "resume accounting mismatch for fraction $fraction"
  }
  $actualHash = (Get-FileHash -LiteralPath $targetPath -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($actualHash -ne $expectedHash) { throw "final SHA-256 mismatch for fraction $fraction" }
  $samples += [ordered]@{
    requestedFraction = $fraction
    observedFraction = $snapshot.Completed / $snapshot.Total
    completedChunksAtKill = $snapshot.Completed
    totalChunks = $snapshot.Total
    reusedBytes = $reusedBytes
    downloadedBytes = $downloadedBytes
    reusedChunks = $reusedChunks
    downloadedChunks = $downloadedChunks
    killedExitCode = $started.Process.ExitCode
    resumedExitCode = $resumed.Process.ExitCode
    finalSha256 = $actualHash
  }
}

$report = [ordered]@{
  schema = "wowdata.resume-kill-benchmark.v1"
  generatedAt = [DateTime]::UtcNow.ToString("o")
  binary = $binaryPath.Replace('\','/')
  binarySha256 = (Get-FileHash -LiteralPath $binaryPath -Algorithm SHA256).Hash.ToLowerInvariant()
  workloadBinarySha256 = (Get-FileHash -LiteralPath $rangeDownloadPath -Algorithm SHA256).Hash.ToLowerInvariant()
  wowdataHome = $homePath.Replace('\','/')
  target = $targetPath.Replace('\','/')
  targetBytes = $targetSize
  expectedSha256 = $expectedHash
  build = $Build
  buildKey = $BuildKey
  targetName = $TargetName
  chunkSizeBytes = [int64]$ExpectedChunkMiB * 1MB
  inheritedExperimentOverrides = $false
  command = ($rangeDownloadPath + " -url " + $URL + " -sha256 " + $expectedHash + " -workdir <resume> -output <target>").Replace('\','/')
  samples = $samples
  status = "pass"
}
$reportPath = Join-Path $evidencePath "report.json"
$report | ConvertTo-Json -Depth 10 | Set-Content -Encoding utf8 $reportPath
$hashes = Get-ChildItem -LiteralPath $evidencePath -File | Where-Object Name -ne "sha256-manifest.json" | Sort-Object Name | ForEach-Object {
  [ordered]@{ path = $_.FullName.Substring($repo.Length + 1).Replace('\','/'); bytes = $_.Length; sha256 = (Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant() }
}
[ordered]@{ schema = "wowdata.evidence-sha256.v1"; files = @($hashes) } | ConvertTo-Json -Depth 6 | Set-Content -Encoding utf8 (Join-Path $evidencePath "sha256-manifest.json")
Remove-Item -LiteralPath $rangeDownloadPath -Force -ErrorAction SilentlyContinue
$report | ConvertTo-Json -Depth 8
