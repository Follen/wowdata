param(
  [string]$Binary = "analyze/benchmark/wowdata-current.exe",
  [string]$Report = "analyze/benchmark/db2-build-r3-timings.json",
  [string]$WowdataHome = "$env:USERPROFILE/.wowdata",
  [int]$WarmupRuns = 1,
  [int]$Runs = 20
)

$ErrorActionPreference = "Stop"
$repo = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
Set-Location $repo
$binaryPath = (Resolve-Path $Binary).Path
$reportPath = [IO.Path]::GetFullPath((Join-Path $repo $Report))
New-Item -ItemType Directory -Force -Path (Split-Path $reportPath) | Out-Null
$arguments = @(
  "--source", "remote", "--region", "cn", "--product", "wow",
  "--build", "latest", "--locale", "zhCN",
  "db2", "rows", "SpellName", "--id", "1"
)

function ConvertTo-ProcessArgument([string]$Value) {
  if ($Value -notmatch '[\s"]') { return $Value }
  return '"' + $Value.Replace('"', '\"') + '"'
}

function Invoke-Query([int]$Run, [bool]$Measured) {
  $info = [Diagnostics.ProcessStartInfo]::new()
  $info.FileName = $binaryPath
  $info.UseShellExecute = $false
  $info.RedirectStandardOutput = $true
  $info.RedirectStandardError = $true
  $info.CreateNoWindow = $true
  $info.Environment["WOWDATA_HOME"] = [IO.Path]::GetFullPath($WowdataHome)
  $info.Arguments = (($arguments | ForEach-Object { ConvertTo-ProcessArgument $_ }) -join " ")
  $process = [Diagnostics.Process]::new()
  $process.StartInfo = $info
  $watch = [Diagnostics.Stopwatch]::StartNew()
  [void]$process.Start()
  $stdout = $process.StandardOutput.ReadToEndAsync()
  $stderr = $process.StandardError.ReadToEndAsync()
  $peakWorkingSet = 0L
  while (-not $process.WaitForExit(10)) {
    $process.Refresh()
    if ($process.WorkingSet64 -gt $peakWorkingSet) { $peakWorkingSet = $process.WorkingSet64 }
  }
  $process.Refresh()
  if ($process.WorkingSet64 -gt $peakWorkingSet) { $peakWorkingSet = $process.WorkingSet64 }
  $watch.Stop()
  if (-not $Measured) { return $null }
  $sha = [Security.Cryptography.SHA256]::Create()
  try {
    $stdoutHash = ([BitConverter]::ToString($sha.ComputeHash([Text.Encoding]::UTF8.GetBytes($stdout.Result)))).Replace("-", "").ToLowerInvariant()
    $stderrHash = ([BitConverter]::ToString($sha.ComputeHash([Text.Encoding]::UTF8.GetBytes($stderr.Result)))).Replace("-", "").ToLowerInvariant()
  } finally {
    $sha.Dispose()
  }
  return [ordered]@{
    run = $Run; milliseconds = $watch.Elapsed.TotalMilliseconds; exitCode = $process.ExitCode
    cpuMilliseconds = $process.TotalProcessorTime.TotalMilliseconds
    peakWorkingSetBytes = [int64]$peakWorkingSet
    stdoutSha256 = $stdoutHash; stderrSha256 = $stderrHash
  }
}

for ($run = 1; $run -le $WarmupRuns; $run++) { [void](Invoke-Query $run $false) }
$samples = @()
for ($run = 1; $run -le $Runs; $run++) { $samples += Invoke-Query $run $true }
$sorted = [double[]]@($samples.milliseconds | Sort-Object)
$p50 = $sorted[[Math]::Max(0, [Math]::Ceiling(0.50 * $sorted.Count) - 1)]
$p95 = $sorted[[Math]::Max(0, [Math]::Ceiling(0.95 * $sorted.Count) - 1)]
$result = [ordered]@{
  schema = "wowdata.query-benchmark.v1"
  generatedAt = [DateTime]::UtcNow.ToString("o")
  binary = $binaryPath.Replace('\','/')
  binarySha256 = (Get-FileHash $binaryPath -Algorithm SHA256).Hash.ToLowerInvariant()
  command = (($binaryPath + " " + ($arguments -join " ")).Replace('\','/'))
  warmupRuns = $WarmupRuns; measuredRuns = $Runs; runs = $samples
  p50Milliseconds = $p50; p95Milliseconds = $p95
  maxMilliseconds = ($sorted | Measure-Object -Maximum).Maximum
  medianCpuMilliseconds = (@($samples.cpuMilliseconds | Sort-Object))[[Math]::Floor($samples.Count / 2)]
  peakWorkingSetBytes = ($samples.peakWorkingSetBytes | Measure-Object -Maximum).Maximum
  failedRuns = @($samples | Where-Object { $_.exitCode -ne 0 }).Count
  status = if ($p95 -le 500 -and @($samples | Where-Object { $_.exitCode -ne 0 }).Count -eq 0) { "pass" } else { "fail" }
}
$result | ConvertTo-Json -Depth 12 | Set-Content -Encoding utf8 $reportPath
$result | ConvertTo-Json -Depth 5
if ($result.status -ne "pass") { exit 1 }
