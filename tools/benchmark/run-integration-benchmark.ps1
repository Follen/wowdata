param(
  [string]$Binary = "analyze/benchmark/wowdata-current.exe",
  [string]$Matrix = "analyze/benchmark/integration/matrix.json",
  [string]$OutputRoot = "analyze/benchmark/integration-run",
  [string]$CommandPattern = ".*",
  [string]$CasePattern = ".*",
  [string[]]$Protocols = @("independent-cold", "shared-build-cold", "warm", "repeat-warm"),
  [int]$MaxCases = 0,
  [ValidateRange(1, 1000)][int]$Repetitions = 1,
  [ValidateRange(1, 86400)][int]$ProcessTimeoutSeconds = 1800
)

$ErrorActionPreference = "Stop"
$repo = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
Set-Location $repo
$binaryPath = (Resolve-Path $Binary).Path
$matrixPath = (Resolve-Path $Matrix).Path
$runRoot = [IO.Path]::GetFullPath((Join-Path $repo $OutputRoot))
$utf8NoBom = [Text.UTF8Encoding]::new($false)
New-Item -ItemType Directory -Force -Path $runRoot | Out-Null

function Fail-Contract([string]$Message) {
  [Console]::Error.WriteLine("integration matrix contract mismatch: $Message")
  exit 1
}

function Get-StringHash([string]$Value) {
  $sha = [Security.Cryptography.SHA256]::Create()
  try { return ([BitConverter]::ToString($sha.ComputeHash([Text.Encoding]::UTF8.GetBytes($Value)))).Replace('-', '').ToLowerInvariant() }
  finally { $sha.Dispose() }
}

function ConvertTo-ProcessArgument([string]$Value) {
  if ($Value -notmatch '[\s"]') { return $Value }
  return '"' + $Value.Replace('"', '\"') + '"'
}

function Get-TreeBytes([string]$Path) {
  if (-not (Test-Path -LiteralPath $Path)) { return 0L }
  $sum = (Get-ChildItem -LiteralPath $Path -Recurse -File -ErrorAction SilentlyContinue | Measure-Object Length -Sum).Sum
  if ($null -eq $sum) { return 0L }
  return [int64]$sum
}

function Get-TreeHashes([string]$Path) {
  if (-not (Test-Path -LiteralPath $Path)) { return @() }
  $root = [IO.Path]::GetFullPath($Path).TrimEnd('\','/')
  return @(Get-ChildItem -LiteralPath $Path -Recurse -File | Sort-Object FullName | ForEach-Object {
    $full = [IO.Path]::GetFullPath($_.FullName)
    if (-not $full.StartsWith($root + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw "output escaped root: $full" }
    [ordered]@{
      path = $full.Substring($root.Length + 1).Replace('\','/')
      bytes = [int64]$_.Length
      sha256 = (Get-FileHash -LiteralPath $full -Algorithm SHA256).Hash.ToLowerInvariant()
    }
  })
}

function Get-GameDirectory {
  if ($env:WOWDATA_GAME_DIR) { return [IO.Path]::GetFullPath($env:WOWDATA_GAME_DIR) }
  $envFile = Join-Path $HOME '.wowdata/.env'
  if (Test-Path -LiteralPath $envFile) {
    foreach ($line in [IO.File]::ReadAllLines($envFile)) {
      if ($line -match '^\s*WOWDATA_(?:GAME_DIR|WOW_DIR|LOCAL_PATH)\s*=\s*["'']?(.*?)["'']?\s*$') {
        if ($Matches[1]) { return [IO.Path]::GetFullPath($Matches[1]) }
      }
    }
  }
  return $null
}

function Expand-Value([string]$Value, [string]$CellRoot, [string]$GameDir) {
  $expanded = $Value.Replace('${RUN_ROOT}', $CellRoot)
  if ($expanded.Contains('${WOWDATA_GAME_DIR}')) {
    if (-not $GameDir) { throw 'WOWDATA_GAME_DIR is required by selected local cell' }
    $expanded = $expanded.Replace('${WOWDATA_GAME_DIR}', $GameDir)
  }
  return $expanded
}

function Resolve-Arguments([object[]]$Values, [string]$CellRoot, [string]$GameDir, [string]$Output) {
  $result = @($Values | ForEach-Object { Expand-Value ([string]$_) $CellRoot $GameDir })
  for ($i = 0; $i -lt $result.Count; $i++) {
    if ($result[$i] -eq '--output' -and $i + 1 -lt $result.Count) {
      $original = $result[$i + 1]
      $result[$i + 1] = if ([IO.Path]::GetExtension($original)) { Join-Path $Output ([IO.Path]::GetFileName($original)) } else { $Output }
      $i++
    } elseif ($result[$i] -like '--output=*') {
      $original = $result[$i].Substring('--output='.Length)
      $resolved = if ([IO.Path]::GetExtension($original)) { Join-Path $Output ([IO.Path]::GetFileName($original)) } else { $Output }
      $result[$i] = '--output=' + $resolved
    }
  }
  return [string[]]$result
}

function New-IsolatedNpmFixture([string]$InstallRoot) {
  if (-not $InstallRoot) { Fail-Contract 'isolated npm fixture requires installRoot' }
  $bin = Join-Path $InstallRoot 'bin'
  $prefix = Join-Path $InstallRoot 'npm-prefix'
  $npmCache = Join-Path $InstallRoot 'npm-cache'
  $user = Join-Path $InstallRoot 'user'
  New-Item -ItemType Directory -Force -Path $bin, $prefix, $npmCache, $user | Out-Null
  $script = @'
@echo off
if not exist "%NPM_CONFIG_PREFIX%" mkdir "%NPM_CONFIG_PREFIX%"
echo %*>>"%NPM_CONFIG_PREFIX%\npm-invocations.log"
echo integration npm fixture: %*
exit /b 0
'@
  [IO.File]::WriteAllText((Join-Path $bin 'npm.cmd'), $script, [Text.Encoding]::ASCII)
  return [ordered]@{ bin = $bin; prefix = $prefix; cache = $npmCache; user = $user }
}

function Invoke-Process([string[]]$Arguments, [string]$WowHome, [string]$Cache, [string]$Output, [string]$Evidence, [string]$InstallRoot, [string]$Fixture) {
  New-Item -ItemType Directory -Force -Path $WowHome, $Cache, $Output, $Evidence | Out-Null
  $stdoutPath, $stderrPath = (Join-Path $Evidence 'stdout.log'), (Join-Path $Evidence 'stderr.log')
  $cacheBefore, $outputBefore = Get-TreeBytes $Cache, Get-TreeBytes $Output
  $info = [Diagnostics.ProcessStartInfo]::new()
  $info.FileName = $binaryPath
  $info.UseShellExecute = $false
  $info.RedirectStandardOutput = $true
  $info.RedirectStandardError = $true
  $info.CreateNoWindow = $true
  $info.Environment['WOWDATA_HOME'] = $WowHome
  $info.Environment['WOWDATA_TIMING'] = '1'
  if ($Fixture) {
    if ($Fixture -ne 'isolated-npm-success') { Fail-Contract "unknown process fixture '$Fixture'" }
    $npm = New-IsolatedNpmFixture $InstallRoot
    $info.Environment['PATH'] = $npm.bin + [IO.Path]::PathSeparator + $info.Environment['PATH']
    $info.Environment['HOME'] = $npm.user
    $info.Environment['USERPROFILE'] = $npm.user
    $info.Environment['APPDATA'] = Join-Path $npm.user 'AppData/Roaming'
    $info.Environment['LOCALAPPDATA'] = Join-Path $npm.user 'AppData/Local'
    $info.Environment['NPM_CONFIG_PREFIX'] = $npm.prefix
    $info.Environment['NPM_CONFIG_CACHE'] = $npm.cache
    $info.Environment['npm_config_prefix'] = $npm.prefix
    $info.Environment['npm_config_cache'] = $npm.cache
  }
  if ($null -ne $info.ArgumentList) {
    foreach ($argument in $Arguments) { [void]$info.ArgumentList.Add($argument) }
  } else {
    $info.Arguments = (($Arguments | ForEach-Object { ConvertTo-ProcessArgument $_ }) -join ' ')
  }
  $process = [Diagnostics.Process]::new()
  $process.StartInfo = $info
  $watch = [Diagnostics.Stopwatch]::StartNew()
  $started = [DateTime]::UtcNow
  [void]$process.Start()
  $stdoutTask, $stderrTask = $process.StandardOutput.ReadToEndAsync(), $process.StandardError.ReadToEndAsync()
  $peak, $timedOut = 0L, $false
  while (-not $process.WaitForExit(10)) {
    $process.Refresh()
    if ($process.WorkingSet64 -gt $peak) { $peak = $process.WorkingSet64 }
    if ($watch.Elapsed.TotalSeconds -ge $ProcessTimeoutSeconds) {
      $timedOut = $true
      $process.Kill($true)
      $process.WaitForExit()
      break
    }
  }
  $process.Refresh()
  if ($process.WorkingSet64 -gt $peak) { $peak = $process.WorkingSet64 }
  $watch.Stop()
  $stdout, $stderr = $stdoutTask.Result, $stderrTask.Result
  [IO.File]::WriteAllText($stdoutPath, $stdout, $utf8NoBom)
  [IO.File]::WriteAllText($stderrPath, $stderr, $utf8NoBom)
  $network, $resource, $runtimeMemory = $null, $null, $null
  $timingLines = @($stderr -split "`r?`n" | Where-Object { $_ -match '^(stage|network|resource|runtime memory) ' })
  foreach ($line in $timingLines) {
	if ($line -match '^network metrics=(\{.*\})$') { try { $network = $Matches[1] | ConvertFrom-Json } catch {} }
	if ($line -match '^resource metrics=(\{.*\})$') { try { $resource = $Matches[1] | ConvertFrom-Json } catch {} }
	if ($line -match '^runtime memory metrics=(\{.*\})$') { try { $runtimeMemory = $Matches[1] | ConvertFrom-Json } catch {} }
  }
  $cacheAfter, $outputAfter = Get-TreeBytes $Cache, Get-TreeBytes $Output
  return [ordered]@{
    startedAt = $started.ToString('o'); finishedAt = [DateTime]::UtcNow.ToString('o')
    arguments = @($Arguments); exitCode = $process.ExitCode; timedOut = $timedOut
    wallMilliseconds = $watch.Elapsed.TotalMilliseconds
    cpuMilliseconds = $process.TotalProcessorTime.TotalMilliseconds
	peakWorkingSetBytes = [int64]$peak
	peakHeapBytes = if ($runtimeMemory) { [int64]$runtimeMemory.peakHeapBytes } else { $null }
	gcCollections = if ($runtimeMemory) { [int64]$runtimeMemory.gcCollections } else { $null }
	allocatedBytes = if ($runtimeMemory) { [int64]$runtimeMemory.allocatedBytes } else { $null }
	runtimeMemoryUnavailableReason = if ($runtimeMemory) { $null } else { 'runtime memory metrics line was absent' }
    cacheBeforeBytes = $cacheBefore; cacheAfterBytes = $cacheAfter; cacheDeltaBytes = $cacheAfter - $cacheBefore
    outputBeforeBytes = $outputBefore; outputAfterBytes = $outputAfter; outputDeltaBytes = $outputAfter - $outputBefore
    stdout = $stdoutPath.Replace('\','/'); stderr = $stderrPath.Replace('\','/')
    stdoutSha256 = Get-StringHash $stdout; stderrSha256 = Get-StringHash $stderr
    outputFiles = @(Get-TreeHashes $Output)
	timingLines = @($timingLines); networkMetrics = $network; resourceMetrics = $resource; runtimeMemoryMetrics = $runtimeMemory
  }
}

$definition = [IO.File]::ReadAllText($matrixPath, [Text.Encoding]::UTF8) | ConvertFrom-Json
if ($definition.schema -ne 'wowdata.integration-benchmark-matrix.v1') { Fail-Contract "schema is '$($definition.schema)'" }
foreach ($required in @('protocols','sources','builds','scenarios','commands','summary')) {
  if ($null -eq $definition.$required) { Fail-Contract "missing root field '$required'" }
}
foreach ($protocol in $Protocols) {
  if ($protocol -notin @($definition.protocols)) { Fail-Contract "unknown protocol '$protocol'" }
}

$selected, $allCellCount, $executableCount, $ids = @(), 0, 0, @{}
$buildNames = @($definition.builds | ForEach-Object { [string]$_.name })
foreach ($command in @($definition.commands)) {
  if (-not $command.path -or $null -eq $command.cases) { Fail-Contract 'command requires path and cases' }
  foreach ($cell in @($command.cases)) {
    $allCellCount++
    foreach ($field in @('id','stableKey','build','source','protocol','scenario','executable','home','cache','output','capture')) {
      if ($null -eq $cell.$field) { Fail-Contract "cell under '$($command.path)' missing '$field'" }
    }
    if ($ids.ContainsKey([string]$cell.id)) { Fail-Contract "duplicate cell id '$($cell.id)'" }
    $ids[[string]$cell.id] = $true
    if ([string]$cell.build -notin $buildNames) { Fail-Contract "cell '$($cell.id)' has unknown Build '$($cell.build)'" }
    if ([string]$cell.source -notin @($definition.sources)) { Fail-Contract "cell '$($cell.id)' has unknown source '$($cell.source)'" }
    if ([string]$cell.protocol -notin @($definition.protocols)) { Fail-Contract "cell '$($cell.id)' has unknown protocol '$($cell.protocol)'" }
    if ([string]$cell.scenario -notin @($definition.scenarios)) { Fail-Contract "cell '$($cell.id)' has unknown scenario '$($cell.scenario)'" }
    foreach ($pathField in @('home','cache','output')) {
      if (-not ([string]$cell.$pathField).Contains('${RUN_ROOT}')) { Fail-Contract "cell '$($cell.id)' $pathField is not RUN_ROOT-isolated" }
    }
    if (-not [bool]$cell.executable) { continue }
    $executableCount++
    if ([string]$command.path -in @('update','uninstall')) {
      if (-not $cell.installRoot -or [string]$cell.fixture -ne 'isolated-npm-success') {
        Fail-Contract "executable install-management cell '$($cell.id)' lacks isolated npm fixture"
      }
    }
    foreach ($capture in @('stdout','stderr','exit-code','output-sha256','wall-time','cpu','peak-working-set','cache-delta','network-bytes','stage-times')) {
      if ($capture -notin @($cell.capture)) { Fail-Contract "executable cell '$($cell.id)' lacks capture '$capture'" }
    }
    if ([string]$command.path -notmatch $CommandPattern) { continue }
    if ([string]$cell.id -notmatch $CasePattern -or [string]$cell.protocol -notin $Protocols) { continue }
    if ($null -eq $cell.args -or -not $cell.expectedKind) { Fail-Contract "executable cell '$($cell.id)' lacks args or expectedKind" }
    $selected += [pscustomobject]@{ command = $command; cell = $cell }
  }
}
if ([int]$definition.summary.leafCommands -ne @($definition.commands).Count) { Fail-Contract 'summary.leafCommands does not match commands' }
if ([int]$definition.summary.cells -ne $allCellCount) { Fail-Contract 'summary.cells does not match cases' }
if ([int]$definition.summary.executable -ne $executableCount) { Fail-Contract 'summary.executable does not match executable cells' }
if ($MaxCases -gt 0) { $selected = @($selected | Select-Object -First $MaxCases) }
if (@($selected).Count -eq 0) {
  [Console]::Error.WriteLine('integration benchmark selection matched no executable cells')
  exit 1
}

$gameDir = Get-GameDirectory
$samples = @()
foreach ($entry in $selected) {
  $cell, $command = $entry.cell, $entry.command
  for ($repetition = 1; $repetition -le $Repetitions; $repetition++) {
    $expansionRoot = Join-Path $runRoot ("rep-{0:D3}" -f $repetition)
    $evidenceRoot = Join-Path $expansionRoot (Join-Path 'evidence' ([string]$cell.id))
    $wowHome = Expand-Value ([string]$cell.home) $expansionRoot $gameDir
    $cache = Expand-Value ([string]$cell.cache) $expansionRoot $gameDir
    $output = Expand-Value ([string]$cell.output) $expansionRoot $gameDir
    $install = if ($cell.installRoot) { Expand-Value ([string]$cell.installRoot) $expansionRoot $gameDir } else { $null }
    foreach ($path in @($wowHome, $cache, $output, $install) | Where-Object { $_ }) {
      $full = [IO.Path]::GetFullPath($path)
      if (-not $full.StartsWith($runRoot, [StringComparison]::OrdinalIgnoreCase)) { Fail-Contract "cell '$($cell.id)' path escapes run root: $full" }
    }
    $evidence = $evidenceRoot
    $setupResults = @()
    foreach ($setup in @($cell.setup)) {
      if ($null -eq $setup -or @($setup).Count -eq 0) { continue }
      $setupArgs = Resolve-Arguments @($setup) $expansionRoot $gameDir $output
      if (@($setupArgs).Count -eq 0) { continue }
      $setupResults += Invoke-Process $setupArgs $wowHome $cache $output (Join-Path $evidence ("setup-{0:D2}" -f ($setupResults.Count + 1))) $install ([string]$cell.fixture)
      if ($setupResults[-1].exitCode -ne 0) { break }
    }
    $result = if (@($setupResults | Where-Object exitCode -ne 0).Count -eq 0) {
      Invoke-Process (Resolve-Arguments @($cell.args) $expansionRoot $gameDir $output) $wowHome $cache $output (Join-Path $evidence 'command') $install ([string]$cell.fixture)
    } else { $null }
    $samples += [ordered]@{
      commandPath = [string]$command.path; caseID = [string]$cell.id; stableCaseKey = [string]$cell.stableKey
      build = [string]$cell.build; source = [string]$cell.source; protocol = [string]$cell.protocol
      scenario = [string]$cell.scenario; repetition = $repetition; home = $wowHome.Replace('\','/')
      cache = $cache.Replace('\','/'); output = $output.Replace('\','/'); installRoot = if ($install) { $install.Replace('\','/') } else { $null }
      setup = @($setupResults); result = $result
      status = if ($null -ne $result -and -not $result.timedOut -and (($cell.expectedKind -in @('success','empty-result') -and $result.exitCode -eq 0) -or ($cell.expectedKind -notin @('success','empty-result') -and $result.exitCode -ne 0))) { 'pass' } else { 'fail' }
    }
  }
}

$report = [ordered]@{
  schema = 'wowdata.integration-benchmark-run.v1'
  generatedAt = [DateTime]::UtcNow.ToString('o')
  matrix = $matrixPath.Replace('\','/'); matrixSha256 = (Get-FileHash $matrixPath -Algorithm SHA256).Hash.ToLowerInvariant()
  binary = $binaryPath.Replace('\','/'); binarySha256 = (Get-FileHash $binaryPath -Algorithm SHA256).Hash.ToLowerInvariant()
  selection = [ordered]@{ commandPattern = $CommandPattern; casePattern = $CasePattern; protocols = @($Protocols); maxCases = $MaxCases; repetitions = $Repetitions }
  selectedCases = @($selected).Count; samples = @($samples)
  status = if (@($samples | Where-Object status -ne 'pass').Count -eq 0) { 'pass' } else { 'fail' }
}
$reportPath = Join-Path $runRoot 'report.json'
[IO.File]::WriteAllText($reportPath, (($report | ConvertTo-Json -Depth 30) + [Environment]::NewLine), $utf8NoBom)
Write-Output $reportPath
if ($report.status -ne 'pass') { exit 1 }
exit 0
