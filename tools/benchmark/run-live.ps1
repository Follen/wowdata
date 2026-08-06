param(
  [string]$Binary = "analyze/benchmark/wowdata-current.exe",
  [string]$EvidenceRoot = "analyze/benchmark/live-encounter",
  [string]$WowdataHome = "$env:USERPROFILE/.wowdata",
  [string]$ExportRoot = "$env:USERPROFILE/Desktop/png",
  [string]$Region = "cn",
  [string]$Product = "wow",
  [string]$Build = "latest",
  [string]$Locale = "zhCN",
  [string]$Instance = "",
  [int]$Boss = 1,
  [int]$Runs = 10
)

$ErrorActionPreference = "Stop"
if ([string]::IsNullOrWhiteSpace($Instance)) {
  $Instance = -join @([char]0x865A, [char]0x5F71, [char]0x5C16, [char]0x5854)
}
$repo = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
Set-Location $repo
$binaryPath = (Resolve-Path $Binary).Path
$evidencePath = [IO.Path]::GetFullPath((Join-Path $repo $EvidenceRoot))
$exportPath = [IO.Path]::GetFullPath($ExportRoot)
$homePath = [IO.Path]::GetFullPath($WowdataHome)
New-Item -ItemType Directory -Force -Path $evidencePath, $exportPath | Out-Null

function Get-TreeBytes([string]$Path) {
  if (-not (Test-Path -LiteralPath $Path)) { return 0L }
  return [int64]((Get-ChildItem -LiteralPath $Path -Recurse -File -ErrorAction SilentlyContinue | Measure-Object Length -Sum).Sum)
}

function Get-Percentile([double[]]$Values, [double]$Percentile) {
  $sorted = @($Values | Sort-Object)
  if ($sorted.Count -eq 0) { return 0.0 }
  $index = [Math]::Max(0, [Math]::Ceiling($Percentile * $sorted.Count) - 1)
  return [double]$sorted[$index]
}

function ConvertTo-ProcessArgument([string]$Value) {
  if ($Value -notmatch '[\s"]') { return $Value }
  return '"' + $Value.Replace('"', '\"') + '"'
}

function Get-RepoRelativePath([string]$Path) {
  $rootUri = [Uri]($repo.TrimEnd('\') + '\')
  $pathUri = [Uri]([IO.Path]::GetFullPath($Path))
  return [Uri]::UnescapeDataString($rootUri.MakeRelativeUri($pathUri).ToString())
}

function Get-WowdataCacheUsage {
  $previousHome = $env:WOWDATA_HOME
  try {
    $env:WOWDATA_HOME = $homePath
    $text = (& $binaryPath cache status) -join "`n"
    if ($LASTEXITCODE -ne 0) { throw "cache status exited $LASTEXITCODE" }
    return ($text | ConvertFrom-Json).data.usage
  } finally {
    $env:WOWDATA_HOME = $previousHome
  }
}

function Invoke-WowdataRun([int]$Run) {
  $stdout = Join-Path $evidencePath ("run-{0:D2}.stdout.json" -f $Run)
  $stderr = Join-Path $evidencePath ("run-{0:D2}.stderr.log" -f $Run)
  $info = [Diagnostics.ProcessStartInfo]::new()
  $info.FileName = $binaryPath
  $info.UseShellExecute = $false
  $info.RedirectStandardOutput = $true
  $info.RedirectStandardError = $true
  $info.CreateNoWindow = $true
  $info.Environment["WOWDATA_HOME"] = $homePath
  $arguments = @(
    "--source", "remote", "--region", $Region, "--product", $Product,
    "--build", $Build, "--locale", $Locale,
    "encounter", "export", "--instance", $Instance,
    "--boss", $Boss.ToString(), "--output", $exportPath
  )
  $info.Arguments = (($arguments | ForEach-Object { ConvertTo-ProcessArgument $_ }) -join " ")
  $process = [Diagnostics.Process]::new()
  $process.StartInfo = $info
  $started = [DateTime]::UtcNow
  $stopwatch = [Diagnostics.Stopwatch]::StartNew()
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
  $stopwatch.Stop()
  [IO.File]::WriteAllText($stdout, $stdoutRead.Result, [Text.UTF8Encoding]::new($false))
  [IO.File]::WriteAllText($stderr, $stderrRead.Result, [Text.UTF8Encoding]::new($false))
  return [ordered]@{
    run = $Run
    startedAt = $started.ToString("o")
    milliseconds = $stopwatch.Elapsed.TotalMilliseconds
    cpuMilliseconds = $process.TotalProcessorTime.TotalMilliseconds
    peakWorkingSetBytes = [int64]$peakWorkingSet
    exitCode = $process.ExitCode
    stdout = Get-RepoRelativePath $stdout
    stderr = Get-RepoRelativePath $stderr
    stdoutSha256 = (Get-FileHash -LiteralPath $stdout -Algorithm SHA256).Hash.ToLowerInvariant()
    stderrSha256 = (Get-FileHash -LiteralPath $stderr -Algorithm SHA256).Hash.ToLowerInvariant()
  }
}

$cacheBefore = Get-TreeBytes $homePath
$cacheUsageBefore = Get-WowdataCacheUsage
$samples = @()
for ($run = 1; $run -le $Runs; $run++) {
  $samples += Invoke-WowdataRun $run
}
$cacheAfter = Get-TreeBytes $homePath
$cacheUsageAfter = Get-WowdataCacheUsage

$verificationErrors = @()
$manifestPath = Join-Path $exportPath "manifest.json"
$manifest = $null
if (-not (Test-Path -LiteralPath $manifestPath)) {
  $verificationErrors += "missing manifest.json"
} else {
  $manifestText = [IO.File]::ReadAllText($manifestPath, [Text.Encoding]::UTF8)
  $manifest = $manifestText | ConvertFrom-Json
  foreach ($item in $manifest.items) {
    if ($item.status -ne "success" -and $item.status -ne "reused") {
      $verificationErrors += "item $($item.semanticID) status $($item.status)"
      continue
    }
    if (-not (Test-Path -LiteralPath $item.path)) {
      $verificationErrors += "item $($item.semanticID) missing file"
      continue
    }
    $bytes = [IO.File]::ReadAllBytes($item.path)
    if ($bytes.Length -lt 8 -or $bytes[0] -ne 137 -or $bytes[1] -ne 80 -or $bytes[2] -ne 78 -or $bytes[3] -ne 71) {
      $verificationErrors += "item $($item.semanticID) invalid PNG signature"
    }
    $sha = (Get-FileHash -LiteralPath $item.path -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($sha -ne $item.sha256) {
      $verificationErrors += "item $($item.semanticID) SHA-256 mismatch"
    }
  }
}

$milliseconds = [double[]]@($samples | ForEach-Object { $_.milliseconds })
$failedRuns = @($samples | Where-Object { $_.exitCode -ne 0 }).Count
$processor = Get-CimInstance Win32_Processor | Select-Object -First 1 -ExpandProperty Name
$report = [ordered]@{
  schema = "wowdata.live-benchmark.v1"
  generatedAt = [DateTime]::UtcNow.ToString("o")
  repository = $repo.Replace('\','/')
  gitCommit = (& git rev-parse HEAD).Trim()
  gitDirty = [bool](& git status --porcelain)
  binary = $binaryPath.Replace('\','/')
  binarySha256 = (Get-FileHash -LiteralPath $binaryPath -Algorithm SHA256).Hash.ToLowerInvariant()
  environment = [ordered]@{
    os = [Environment]::OSVersion.VersionString
    processor = $processor
    processorCount = [Environment]::ProcessorCount
    go = (& go version)
  }
  protocol = [ordered]@{
    source = "remote"; region = $Region; product = $Product; build = $Build; locale = $Locale
    instance = $Instance; boss = $Boss; runs = $Runs; wowdataHome = $homePath.Replace('\','/')
    exportRoot = $exportPath.Replace('\','/')
  }
  cache = [ordered]@{
    beforeBytes = $cacheBefore; afterBytes = $cacheAfter; deltaBytes = $cacheAfter - $cacheBefore
    usageBefore = $cacheUsageBefore; usageAfter = $cacheUsageAfter
  }
  samples = $samples
  summary = [ordered]@{
    p50Milliseconds = Get-Percentile $milliseconds 0.50
    p95Milliseconds = Get-Percentile $milliseconds 0.95
    failedRuns = $failedRuns
    logicalItems = if ($manifest) { $manifest.logicalItemCount } else { 0 }
    uniqueFileDataIDs = if ($manifest) { $manifest.uniqueFileDataIDCount } else { 0 }
    verificationErrors = $verificationErrors
  }
  status = if ($failedRuns -eq 0 -and $verificationErrors.Count -eq 0) { "pass" } else { "fail" }
}
$reportPath = Join-Path $evidencePath "report.json"
$report | ConvertTo-Json -Depth 20 | Set-Content -Encoding utf8 $reportPath

$hashes = @()
Get-ChildItem -LiteralPath $evidencePath -File | Where-Object { $_.Name -ne "sha256-manifest.json" } | Sort-Object Name | ForEach-Object {
  $hashes += [ordered]@{
    path = Get-RepoRelativePath $_.FullName
    bytes = $_.Length
    sha256 = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
  }
}
[ordered]@{ schema = "wowdata.evidence-sha256.v1"; files = $hashes } | ConvertTo-Json -Depth 8 | Set-Content -Encoding utf8 (Join-Path $evidencePath "sha256-manifest.json")
$report | ConvertTo-Json -Depth 8
if ($report.status -ne "pass") { exit 1 }
