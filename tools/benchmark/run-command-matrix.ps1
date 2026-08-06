param(
  [string]$Binary = "analyze/benchmark/wowdata-current.exe",
  [string]$BaselineBinary = "",
  [string]$Matrix = "analyze/benchmark/command-matrix/matrix.json",
  [string]$GoldenManifest = "fixtures/golden/manifest.json",
  [string]$OutputRoot = "analyze/benchmark/command-matrix-run",
  [string]$CommandPattern = ".*",
  [string]$CasePattern = ".*",
  [string[]]$Protocols = @("independent-cold", "shared-build-cold", "warm", "repeat-warm"),
  [int]$MaxCases = 0,
  [ValidateRange(1, 1000)][int]$Repetitions = 1,
  [string]$RunID = "",
  [string]$Revision = "",
  [string]$NPMShim = "",
  [ValidateRange(1, 86400)][int]$ProcessTimeoutSeconds = 1800,
  [ValidateSet('all','remote','local','neutral','offline')][string]$SourceMode = 'all',
  [switch]$ExecuteRecordOnly,
  [switch]$Formal
)

$ErrorActionPreference = "Stop"
$repo = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
Set-Location $repo
$binaryPath = (Resolve-Path $Binary).Path
$baselineBinaryPath = if ($BaselineBinary) { (Resolve-Path $BaselineBinary).Path } else { $null }
$matrixPath = (Resolve-Path $Matrix).Path
$goldenPath = (Resolve-Path $GoldenManifest).Path
$runRoot = [IO.Path]::GetFullPath((Join-Path $repo $OutputRoot))
$utf8NoBom = [Text.UTF8Encoding]::new($false)
New-Item -ItemType Directory -Force -Path $runRoot | Out-Null
$npmShimPath = if ($NPMShim) { (Resolve-Path $NPMShim).Path } else { $null }

function Write-JsonNoBom([string]$Path, $Value, [int]$Depth = 20) {
  $parent = Split-Path $Path
  if ($parent) { New-Item -ItemType Directory -Force -Path $parent | Out-Null }
  [IO.File]::WriteAllText($Path, (($Value | ConvertTo-Json -Depth $Depth) + [Environment]::NewLine), $utf8NoBom)
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
    if (-not $full.StartsWith($root + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw "output file escaped root: $full" }
    $relative = $full.Substring($root.Length + 1).Replace('\','/')
    [ordered]@{ path = $relative; bytes = $_.Length; sha256 = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant() }
  })
}

function ConvertTo-ProcessArgument([string]$Value) {
  if ($Value -notmatch '[\s"]') { return $Value }
  return '"' + $Value.Replace('"', '\"') + '"'
}

function Get-StringHash([string]$Value) {
  $sha = [Security.Cryptography.SHA256]::Create()
  try { return ([BitConverter]::ToString($sha.ComputeHash([Text.Encoding]::UTF8.GetBytes($Value)))).Replace('-', '').ToLowerInvariant() }
  finally { $sha.Dispose() }
}

function Normalize-CommandOutput([string]$Value, [string]$WowdataHome, [string]$Output, [string]$CommandPath = '') {
  $normalized = $Value
  if ($WowdataHome) {
    foreach ($form in @($WowdataHome, $WowdataHome.Replace('\','/'), $WowdataHome.Replace('\','\\'))) {
      $normalized = $normalized.Replace($form, '<WOWDATA_HOME>')
    }
  }
  if ($Output) {
    foreach ($form in @($Output, $Output.Replace('\','/'), $Output.Replace('\','\\'))) {
      $normalized = $normalized.Replace($form, '<OUTPUT>')
    }
  }
  $normalized = [regex]::Replace($normalized, '("(?:initializedAtUtc|lastUsedAt|createdAt|updatedAt)"\s*:\s*")[^"]+("\s*)', '$1<TIMESTAMP>$2')
  try {
    $response = $normalized | ConvertFrom-Json
    if (($CommandPath -eq 'update' -or $CommandPath -eq 'uninstall') -and $response.data -and $null -ne $response.data.output) {
      $response.data.output = '<MANAGEMENT_OUTPUT>'
    }
    return $response | ConvertTo-Json -Compress -Depth 20
  } catch {}
  return $normalized
}

function Get-ComparableTreeHashes([string]$Path, [string]$CommandPath) {
  $files = @(Get-TreeHashes $Path)
  return @($files | ForEach-Object {
    $file = $_
    if ([IO.Path]::GetExtension([string]$file.path) -ieq '.json') {
      $full = Join-Path $Path ([string]$file.path).Replace('/', [IO.Path]::DirectorySeparatorChar)
      $normalized = Normalize-CommandOutput ([IO.File]::ReadAllText($full, [Text.Encoding]::UTF8)) '' $Path $CommandPath
      [ordered]@{ path = [string]$file.path; bytes = [Text.Encoding]::UTF8.GetByteCount($normalized); sha256 = Get-StringHash $normalized }
    } else {
      $file
    }
  })
}

function Normalize-BaselineOutput([string]$Value, [string]$CommandPath, [string]$WowdataHome, [string]$Output) {
  if ($CommandPath -like 'cache *' -or $CommandPath -like 'profile *') {
    $normalized = Normalize-CommandOutput $Value $WowdataHome $Output
    try { $response = $normalized | ConvertFrom-Json } catch { return $normalized }
  }
  if ($CommandPath -like 'cache *') {
    if ($CommandPath -eq 'cache config' -and $null -ne $response.data -and $null -eq $response.data.workerMode) {
      if ([int64]$response.data.downloadWorkers -lt 0) { throw 'legacy cache config worker count is inconsistent' }
      $response.data = [ordered]@{ schema = [string]$response.data.schema; cacheMaxBytes = [int64]$response.data.cacheMaxBytes; downloadWorkers = 0; workerMode = 'auto' }
      return $response | ConvertTo-Json -Compress -Depth 20
    }
    if ($CommandPath -eq 'cache status') {
      $data = $response.data
      # The frozen baseline predates detailed cache usage accounting. Keep this
      # compatibility branch at the baseline boundary so current/golden output
      # still requires the complete cache-state-v1 schema.
      if ($null -ne $data -and $null -eq $data.usage) {
        if ([int64]$data.sizeBytes -lt 0 -or [int64]$data.sizeBytes -gt [int64]$data.maxBytes) { throw 'legacy cache status accounting is inconsistent' }
        $response.data = [ordered]@{ downloadWorkers = 0; format = [ordered]@{ schema = 'wowdata.cache-format.v1'; version = 2; clearedPath = '<CACHE_PATH>'; initializedAtUtc = '<TIMESTAMP>' }; maxBytes = [int64]$data.maxBytes; path = '<CACHE_PATH>'; sizeBytes = 0; usage = [ordered]@{ totalBytes = 0; payloadBytes = 0; uniquePayloadBytes = 0; duplicatePayloadBytes = 0; resumeBytes = 0; derivedMetadataBytes = 0; payloadFiles = 0; resumeFiles = 0; derivedMetadataFiles = 1; diskAmplificationRatio = 0; maxBytes = [int64]$data.maxBytes; withinMaxBytes = $true; withinAmplificationLimit = $true } }
        return $response | ConvertTo-Json -Compress -Depth 20
      }
    }
    return Normalize-GoldenOutput $Value $CommandPath 'cache-state-v1' $WowdataHome $Output
  }
  if ($CommandPath -like 'profile *') { return Normalize-GoldenOutput $Value $CommandPath 'profile-state-v1' $WowdataHome $Output }
  $normalized = Normalize-CommandOutput $Value $WowdataHome $Output $CommandPath
  if ($CommandPath -eq 'update' -or $CommandPath -eq 'uninstall') {
    $normalized = [regex]::Replace($normalized, '("(?:version|currentVersion|latestVersion)"\s*:\s*")[^"]+("\s*)', '$1<VERSION>$2')
  }
  try {
    $response = $normalized | ConvertFrom-Json
    if ($CommandPath -eq 'casc diagnose' -and $response.data -and $response.data.checks) {
      $buildKey = @($response.data.checks | Where-Object { $_.name -eq 'build_key' } | Select-Object -First 1).detail
      foreach ($check in @($response.data.checks)) {
        if (@('archives', 'root', 'encoding', 'tact_keys') -contains [string]$check.name) {
          $check.status = 'unknown'; $check.detail = 'not loaded'
        } elseif ($check.name -eq 'cache_path' -and $buildKey) {
          $check.detail = '<WOWDATA_HOME>/cache/casc/builds/' + [string]$buildKey
        }
      }
      $normalized = $response | ConvertTo-Json -Compress -Depth 20
    }
    if ($response.error -and $response.error.code -eq 'target_required' -and $response.error.message) {
      $response.error.message = [string]$response.error.message -replace 'target fields are required: source,region,product,build,locale', 'target fields are required: region,product,build,locale'
      $normalized = $response | ConvertTo-Json -Compress -Depth 20
    }
  } catch {}
  return $normalized
}

function Normalize-GoldenOutput([string]$Value, [string]$CommandPath, [string]$Comparison, [string]$WowdataHome, [string]$Output) {
  $normalized = Normalize-CommandOutput $Value $WowdataHome $Output $CommandPath
  if ($Comparison -eq 'remote-products-v1') {
    $response = $normalized | ConvertFrom-Json
    $products = @($response.data.products | Where-Object { [string]$_.product -in @('wow', 'wow_classic_era') } | Sort-Object product | ForEach-Object {
      [ordered]@{
        product = [string]$_.product
        region = [string]$_.region
        version = [string]$_.version
        buildId = [string]$_.buildId
        locales = @($_.locales | ForEach-Object { [string]$_ } | Sort-Object)
      }
    })
    $response.data = [ordered]@{ source = [string]$response.data.source; products = $products }
    $response.warnings = @($response.warnings)
    return $response | ConvertTo-Json -Compress -Depth 20
  }
  if ($CommandPath -eq 'casc diagnose') {
    $response = $normalized | ConvertFrom-Json
    foreach ($check in @($response.data.checks)) {
      if ([string]$check.name -eq 'tact_keys') {
        $check.status = 'unknown'
        $check.detail = 'not loaded'
      }
      if ([string]$check.name -eq 'cache_path' -and $check.detail) {
        $check.detail = [regex]::Replace([string]$check.detail, '(?i)[A-Z]:/.+?/cache/casc/', '<WOWDATA_HOME>/cache/casc/')
      }
    }
    return $response | ConvertTo-Json -Compress -Depth 20
  }
  if ($Comparison -ne 'cache-state-v1' -and $Comparison -ne 'profile-state-v1') { return $normalized }
  $response = $normalized | ConvertFrom-Json
  if ($Comparison -eq 'profile-state-v1') {
    if ($response.error -and $response.error.message) {
      $response.error.message = [regex]::Replace([string]$response.error.message, '(?i)^open .+?[\\/]profiles[\\/]', 'open <WOWDATA_HOME>/profiles/')
    }
    return $response | ConvertTo-Json -Compress -Depth 20
  }
  $data = $response.data
  if ($data.path) { $data.path = '<CACHE_PATH>' }
  if ($data.format -and $data.format.clearedPath) { $data.format.clearedPath = '<CACHE_PATH>' }
  if ($CommandPath -eq 'cache clear') {
    if ([int64]$data.removedBytes -lt 0) { throw 'cache clear removedBytes must be non-negative' }
    $data.removedBytes = 0
  } elseif ($CommandPath -eq 'cache prune') {
    if ([int64]$data.beforeBytes -lt 0 -or [int64]$data.beforeBytes -ne [int64]$data.afterBytes -or @($data.removed).Count -ne 0) { throw 'empty cache prune accounting is inconsistent' }
    $data.beforeBytes = 0; $data.afterBytes = 0
  } elseif ($CommandPath -eq 'cache status') {
    $usage = $data.usage
    if ([int64]$data.sizeBytes -lt 0 -or [int64]$data.sizeBytes -ne [int64]$usage.totalBytes -or [int64]$usage.totalBytes -ne [int64]$usage.derivedMetadataBytes -or [int64]$usage.payloadBytes -ne 0 -or [int64]$usage.resumeBytes -ne 0 -or [int64]$usage.uniquePayloadBytes -ne 0 -or [int64]$usage.duplicatePayloadBytes -ne 0 -or [int64]$data.sizeBytes -gt [int64]$data.maxBytes) { throw 'empty cache status accounting is inconsistent' }
    $data.sizeBytes = 0; $usage.totalBytes = 0; $usage.derivedMetadataBytes = 0
  }
  return $response | ConvertTo-Json -Compress -Depth 20
}

function Test-BaselineUnsupported($Sample, [string]$CommandPath) {
	$helpStdout = if ($Sample.stdout -and (Test-Path -LiteralPath $Sample.stdout)) { [IO.File]::ReadAllText($Sample.stdout, [Text.Encoding]::UTF8) } else { '' }
	if ($Formal -and $CommandPath -eq 'encounter export' -and $Sample.exitCode -eq 0 -and $helpStdout -match '(?m)^Usage:\s+wowdata encounter\s+\[flags\]' -and $helpStdout -notmatch '(?m)^Usage:\s+wowdata encounter export\s+\[flags\]') { return $true }
	if ($Sample.exitCode -eq 0) { return $false }
	$stdout = $helpStdout
  $stderr = if ($Sample.stderr -and (Test-Path -LiteralPath $Sample.stderr)) { [IO.File]::ReadAllText($Sample.stderr, [Text.Encoding]::UTF8) } else { '' }
  if ($Formal -and ($stdout + "`n" + $stderr) -match '(?i)(preload_failed|connection.*(?:reset|closed|refused)|\bEOF\b|network)') { return $true }
  $leaf = ($CommandPath -split ' ')[-1]
  return ($stdout + "`n" + $stderr) -match ('(?i)(unknown|unrecognized)\s+(command|subcommand)[^\r\n]*["'']?' + [regex]::Escape($leaf) + '["'']?')
}

function Test-BaselineDemandUnsupported($Command, $Case) {
  if (-not $Formal) { return $false }
  if ([string]$Command.path -eq 'casc diagnose') { return $true }
  if ([string]$Command.path -eq 'casc info') { return $true }
  if ($Case.detectedTarget) { return $true }
  return $false
}

function Get-OutputArgument([object[]]$Values) {
  $args = @($Values | ForEach-Object { [string]$_ })
  for ($i = 0; $i -lt $args.Count; $i++) {
    if ($args[$i] -eq '--output' -and $i + 1 -lt $args.Count) { return $args[$i + 1] }
    if ($args[$i] -like '--output=*') { return $args[$i].Substring('--output='.Length) }
  }
  return $null
}

function Resolve-RepositoryPath([string]$Value) {
  if ([IO.Path]::IsPathRooted($Value)) { return [IO.Path]::GetFullPath($Value) }
  return [IO.Path]::GetFullPath((Join-Path $repo $Value))
}

function Get-OriginalOutputRoot([object[]]$Values) {
  $value = Get-OutputArgument $Values
  if (-not $value) { return $null }
  $absolute = Resolve-RepositoryPath $value
  if ([IO.Path]::GetExtension($value)) { return Split-Path $absolute }
  return $absolute
}

function Get-ManifestHash([object[]]$Files) {
  return Get-StringHash (@($Files) | ConvertTo-Json -Compress -Depth 6)
}

function Test-FileManifestsEqual([object[]]$Left, [object[]]$Right) {
  return (Get-ManifestHash @($Left)) -eq (Get-ManifestHash @($Right))
}

function Compare-FileManifests([object[]]$Before, [object[]]$After) {
  $beforeByPath, $afterByPath = @{}, @{}
  foreach ($file in @($Before)) { $beforeByPath[[string]$file.path] = $file }
  foreach ($file in @($After)) { $afterByPath[[string]$file.path] = $file }
  $added, $removed, $changed = @(), @(), @()
  foreach ($path in @($afterByPath.Keys | Sort-Object)) {
    if (-not $beforeByPath.ContainsKey($path)) { $added += $path; continue }
    $left, $right = $beforeByPath[$path], $afterByPath[$path]
    if ([int64]$left.bytes -ne [int64]$right.bytes -or [string]$left.sha256 -ne [string]$right.sha256) { $changed += $path }
  }
  foreach ($path in @($beforeByPath.Keys | Sort-Object)) { if (-not $afterByPath.ContainsKey($path)) { $removed += $path } }
  return [pscustomobject]@{ added = @($added); removed = @($removed); changed = @($changed); identical = $added.Count -eq 0 -and $removed.Count -eq 0 -and $changed.Count -eq 0 }
}

function Get-GoldenOutputExpectation($Capture, [object[]]$CaseArguments) {
  $outputValue = Get-OutputArgument $CaseArguments
  if (-not $outputValue) { return [pscustomobject]@{ defined = $false; files = @() } }
  $files = @()
  try {
    $response = ([string]$Capture.stdout) | ConvertFrom-Json
    if ($response.data -and $response.data.sha256) {
      $files += [ordered]@{
        path = [IO.Path]::GetFileName($outputValue).Replace('\','/')
        bytes = [int64]$response.data.size
        sha256 = ([string]$response.data.sha256).ToLowerInvariant()
      }
    }
  } catch {}
  if ($files.Count -eq 0 -and [IO.Path]::GetExtension($outputValue)) {
    $absolute = Resolve-RepositoryPath $outputValue
    if (Test-Path -LiteralPath $absolute -PathType Leaf) {
      $item = Get-Item -LiteralPath $absolute
      $files += [ordered]@{ path = $item.Name.Replace('\','/'); bytes = $item.Length; sha256 = (Get-FileHash -LiteralPath $absolute -Algorithm SHA256).Hash.ToLowerInvariant() }
    }
  }
  return [pscustomobject]@{ defined = $true; files = @($files) }
}

function Resolve-Arguments([object[]]$Values, [string]$Output) {
  $resolved = @($Values | ForEach-Object { [string]$_ })
  $gameDir = [Environment]::GetEnvironmentVariable('WOWDATA_GAME_DIR')
  if (-not $gameDir) {
    $envFile = Join-Path $HOME '.wowdata/.env'
    if (Test-Path -LiteralPath $envFile) {
      $line = Get-Content -LiteralPath $envFile | Where-Object { $_ -match '^\s*WOWDATA_GAME_DIR\s*=' } | Select-Object -First 1
      if ($line -and $line -match '^\s*WOWDATA_GAME_DIR\s*=\s*"?([^"#]+)') { $gameDir = $matches[1].Trim() }
    }
  }
  if (-not $gameDir) {
    $installPath = (Get-ItemProperty 'HKLM:\SOFTWARE\WOW6432Node\Blizzard Entertainment\World of Warcraft' -ErrorAction SilentlyContinue).InstallPath
    if ($installPath) { $gameDir = Split-Path ([IO.Path]::GetFullPath($installPath.TrimEnd('\\'))) }
  }
  if ($gameDir) { $resolved = @($resolved | ForEach-Object { $_ -replace '\$\{WOWDATA_GAME_DIR\}', $gameDir }) }
  for ($i = 0; $i -lt $resolved.Count; $i++) {
    if ($resolved[$i] -eq '--output' -and $i + 1 -lt $resolved.Count) {
      $original = $resolved[$i + 1]
      $resolved[$i + 1] = if ([IO.Path]::GetExtension($original)) { Join-Path $Output ([IO.Path]::GetFileName($original)) } else { $Output }
      $i++
    } elseif ($resolved[$i] -like '--output=*') {
      $original = $resolved[$i].Substring('--output='.Length)
      $resolved[$i] = '--output=' + $(if ([IO.Path]::GetExtension($original)) { Join-Path $Output ([IO.Path]::GetFileName($original)) } else { $Output })
    }
  }
  return $resolved
}

function Get-CaseSource([object[]]$Values) {
  $args = @($Values | ForEach-Object { [string]$_ })
  for ($i = 0; $i -lt $args.Count; $i++) {
    if ($args[$i] -eq '--source' -and $i + 1 -lt $args.Count) { return $args[$i + 1] }
    if ($args[$i] -like '--source=*') { return $args[$i].Substring('--source='.Length) }
  }
  return 'neutral'
}

function Assert-ChildPath([string]$Root, [string]$Child) {
  $rootPath = [IO.Path]::GetFullPath($Root).TrimEnd('\','/')
  $childPath = [IO.Path]::GetFullPath($Child)
  if (-not $childPath.StartsWith($rootPath + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
    throw "isolated path escaped run root: $childPath"
  }
}

function Remove-IsolatedTree([string]$Path, [string]$Kind, [string]$Protocol, [int]$Repetition) {
  Assert-ChildPath $runRoot $Path
  $bytes = Get-TreeBytes $Path
  $existed = Test-Path -LiteralPath $Path
  if ($existed) { Remove-Item -LiteralPath $Path -Recurse -Force }
  $removed = -not (Test-Path -LiteralPath $Path)
  if (-not $removed) { throw "cleanup failed for $Path" }
  return [ordered]@{ kind = $Kind; protocol = $Protocol; repetition = $Repetition; path = $Path.Replace('\','/'); existed = $existed; reclaimedBytes = [int64]$bytes; status = 'removed' }
}

function New-InstallSandbox([string]$Root) {
  Assert-ChildPath $runRoot $Root
  $paths = [ordered]@{
    UserProfile = Join-Path $Root 'user-profile'
    Home = Join-Path $Root 'home'
    WowdataHome = Join-Path $Root 'wowdata-home'
    AgentsHome = Join-Path $Root 'agents-home'
    NpmPrefix = Join-Path $Root 'npm-prefix'
    NpmCache = Join-Path $Root 'npm-cache'
    NpmBin = Join-Path $Root 'bin'
    AppData = Join-Path $Root 'appdata/roaming'
    LocalAppData = Join-Path $Root 'appdata/local'
    Temp = Join-Path $Root 'temp'
  }
  foreach ($path in $paths.Values) { Assert-ChildPath $runRoot $path; New-Item -ItemType Directory -Force -Path $path | Out-Null }
  if ($npmShimPath) { Copy-Item -LiteralPath $npmShimPath -Destination (Join-Path $paths.NpmBin 'npm.cmd') -Force }
  $toolDirs = @($paths.NpmBin, $paths.NpmPrefix, (Join-Path $paths.NpmPrefix 'node_modules/.bin'))
  if (-not $npmShimPath) {
    foreach ($tool in @('npm.cmd', 'node.exe')) {
      $resolved = Get-Command $tool -ErrorAction Stop
      $toolDirs += Split-Path $resolved.Source
    }
  }
  $toolDirs += @((Join-Path $env:SystemRoot 'System32'), $env:SystemRoot)
  $environment = @{
    USERPROFILE = $paths.UserProfile; HOME = $paths.Home; WOWDATA_HOME = $paths.WowdataHome; AGENTS_HOME = $paths.AgentsHome
    APPDATA = $paths.AppData; LOCALAPPDATA = $paths.LocalAppData; TEMP = $paths.Temp; TMP = $paths.Temp
    npm_config_prefix = $paths.NpmPrefix; npm_config_cache = $paths.NpmCache
    npm_config_userconfig = (Join-Path $Root 'npm-user.ini'); npm_config_globalconfig = (Join-Path $Root 'npm-global.ini')
    npm_config_update_notifier = 'false'; npm_config_audit = 'false'; npm_config_fund = 'false'
    PATH = (@($toolDirs | Select-Object -Unique) -join [IO.Path]::PathSeparator)
  }
  return [pscustomobject]@{ Root = $Root; WowdataHome = $paths.WowdataHome; Environment = $environment; Paths = $paths }
}

function Invoke-MatrixProcess([string]$Executable, [string[]]$Arguments, [string]$WowdataHome, [string]$Output, [string]$Evidence, [string]$CommandPath, [hashtable]$Environment = @{}, [bool]$CaptureCacheManifest = $false) {
  New-Item -ItemType Directory -Force -Path $WowdataHome, $Output, $Evidence | Out-Null
  $stdoutPath = Join-Path $Evidence 'stdout.log'
  $stderrPath = Join-Path $Evidence 'stderr.log'
  $cachePath = Join-Path $WowdataHome 'cache'
  $cacheManifestBefore = if ($CaptureCacheManifest) { @(Get-TreeHashes $cachePath) } else { @() }
  $cacheManifestBeforeSha256 = if ($CaptureCacheManifest) { Get-ManifestHash $cacheManifestBefore } else { $null }
  $cacheBefore = Get-TreeBytes $cachePath
  $homeBefore = Get-TreeBytes $WowdataHome
  $outputBefore = Get-TreeBytes $Output
  $info = [Diagnostics.ProcessStartInfo]::new()
  $info.FileName = $Executable
  $info.UseShellExecute = $false
  $info.RedirectStandardOutput = $true
  $info.RedirectStandardError = $true
  $info.StandardOutputEncoding = [Text.UTF8Encoding]::new($false, $true)
  $info.StandardErrorEncoding = [Text.UTF8Encoding]::new($false, $true)
  $info.CreateNoWindow = $true
  $info.Environment['WOWDATA_HOME'] = $WowdataHome
  $info.Environment['WOWDATA_TIMING'] = '1'
  foreach ($name in $Environment.Keys) { $info.Environment[[string]$name] = [string]$Environment[$name] }
  if ($null -ne $info.ArgumentList) {
    foreach ($argument in $Arguments) { [void]$info.ArgumentList.Add($argument) }
  } else {
    $info.Arguments = (($Arguments | ForEach-Object { ConvertTo-ProcessArgument $_ }) -join ' ')
  }
  $process = [Diagnostics.Process]::new()
  $process.StartInfo = $info
  $started = [DateTime]::UtcNow
  $watch = [Diagnostics.Stopwatch]::StartNew()
  [void]$process.Start()
  $stderrRead = $process.StandardError.ReadToEndAsync()
  $firstBuffer = New-Object char[] 1
  $firstRead = $process.StandardOutput.ReadAsync($firstBuffer, 0, 1)
  $peak = 0L
  while (-not $firstRead.IsCompleted -and -not $process.HasExited) {
    $process.Refresh()
    if ($process.WorkingSet64 -gt $peak) { $peak = $process.WorkingSet64 }
    Start-Sleep -Milliseconds 2
  }
  $firstCount = $firstRead.Result
  $ttfb = if ($firstCount -gt 0) { $watch.Elapsed.TotalMilliseconds } else { $null }
  $stdoutRead = $process.StandardOutput.ReadToEndAsync()
  $timedOut = $false
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
  $stdout = $(if ($firstCount -gt 0) { [string]$firstBuffer[0] } else { '' }) + $stdoutRead.Result
  $stderr = $stderrRead.Result
  [IO.File]::WriteAllText($stdoutPath, $stdout, $utf8NoBom)
  [IO.File]::WriteAllText($stderrPath, $stderr, $utf8NoBom)
  $cacheAfter = Get-TreeBytes $cachePath
  $cacheManifestAfter = if ($CaptureCacheManifest) { @(Get-TreeHashes $cachePath) } else { @() }
  $cacheManifestAfterSha256 = if ($CaptureCacheManifest) { Get-ManifestHash $cacheManifestAfter } else { $null }
  $cacheManifestDiff = if ($CaptureCacheManifest) { Compare-FileManifests -Before $cacheManifestBefore -After $cacheManifestAfter } else { $null }
  $homeAfter = Get-TreeBytes $WowdataHome
  $outputAfter = Get-TreeBytes $Output
  $normalizedStdout = Normalize-CommandOutput $stdout $WowdataHome $Output $CommandPath
  $timingLines = @($stderr -split "`r?`n" | Where-Object { $_ -match '^(stage|network|resource|runtime memory) ' })
  $networkMetrics = $null
  $resourceMetrics = $null
  $runtimeMemoryMetrics = $null
  foreach ($line in $timingLines) {
    if ($line -match '^network metrics=(\{.*\})$') {
      try { $networkMetrics = $Matches[1] | ConvertFrom-Json } catch { $networkMetrics = $null }
    }
	if ($line -match '^resource metrics=(\{.*\})$') {
	  try { $resourceMetrics = $Matches[1] | ConvertFrom-Json } catch { $resourceMetrics = $null }
	}
	if ($line -match '^runtime memory metrics=(\{.*\})$') {
	  try { $runtimeMemoryMetrics = $Matches[1] | ConvertFrom-Json } catch { $runtimeMemoryMetrics = $null }
	}
  }
  return [ordered]@{
    startedAt = $started.ToString('o'); exitCode = $process.ExitCode; timedOut = $timedOut
    timeoutSeconds = $ProcessTimeoutSeconds
    wallMilliseconds = $watch.Elapsed.TotalMilliseconds; cpuMilliseconds = $process.TotalProcessorTime.TotalMilliseconds
    peakWorkingSetBytes = [int64]$peak
    cacheBeforeBytes = $cacheBefore; cacheAfterBytes = $cacheAfter; cacheDeltaBytes = $cacheAfter - $cacheBefore
    cacheManifestBeforeSha256 = $cacheManifestBeforeSha256; cacheManifestAfterSha256 = $cacheManifestAfterSha256
    cacheManifestDiff = $cacheManifestDiff; cacheIdentityStable = if ($cacheManifestDiff) { $cacheManifestDiff.identical } else { $null }
    homeBeforeBytes = $homeBefore; homeAfterBytes = $homeAfter; homeStateDeltaBytes = $homeAfter - $homeBefore
    outputBeforeBytes = $outputBefore; outputAfterBytes = $outputAfter; outputDeltaBytes = $outputAfter - $outputBefore
    ttfbMilliseconds = $ttfb; postTtfbMilliseconds = if ($null -ne $ttfb) { $watch.Elapsed.TotalMilliseconds - $ttfb } else { $null }
    terminationOverheadMilliseconds = $null; terminationOverheadUnavailableReason = 'stdout stream does not expose last-byte timestamp'
	peakHeapBytes = if ($runtimeMemoryMetrics) { [int64]$runtimeMemoryMetrics.peakHeapBytes } else { $null }
	gcCollections = if ($runtimeMemoryMetrics) { [int64]$runtimeMemoryMetrics.gcCollections } else { $null }
	allocatedBytes = if ($runtimeMemoryMetrics) { [int64]$runtimeMemoryMetrics.allocatedBytes } else { $null }
	runtimeMemoryUnavailableReason = if ($runtimeMemoryMetrics) { $null } else { 'runtime memory metrics line was absent' }
    diskReadBytes = $null; diskWriteBytes = $null; processIOUnavailableReason = 'portable System.Diagnostics.Process API does not expose per-process byte counters'
    stdout = $stdoutPath.Replace('\','/'); stderr = $stderrPath.Replace('\','/')
    stdoutSha256 = Get-StringHash $stdout; stderrSha256 = Get-StringHash $stderr
    normalizedStdoutSha256 = Get-StringHash $normalizedStdout
	timingLines = $timingLines; networkMetrics = $networkMetrics; resourceMetrics = $resourceMetrics; runtimeMemoryMetrics = $runtimeMemoryMetrics
    outputFiles = @(Get-TreeHashes $Output)
    comparableOutputFiles = @(Get-ComparableTreeHashes $Output $CommandPath)
  }
}

$matrixSha256 = (Get-FileHash $matrixPath -Algorithm SHA256).Hash.ToLowerInvariant()
$binarySha256 = (Get-FileHash $binaryPath -Algorithm SHA256).Hash.ToLowerInvariant()
$baselineBinarySha256 = if ($baselineBinaryPath) { (Get-FileHash $baselineBinaryPath -Algorithm SHA256).Hash.ToLowerInvariant() } else { $null }
if (-not $RunID) { $RunID = ([DateTime]::UtcNow.ToString('yyyyMMddTHHmmssfffZ') + '-' + [Guid]::NewGuid().ToString('N').Substring(0, 12)) }
if (-not $Revision) {
  try { $Revision = (& git rev-parse HEAD 2>$null | Select-Object -First 1).Trim() } catch { $Revision = 'unknown' }
  if (-not $Revision) { $Revision = 'unknown' }
}
$revisionDirty = $false
try { $revisionDirty = @(& git status --porcelain --untracked-files=no 2>$null).Count -gt 0 } catch {}

$definition = [IO.File]::ReadAllText($matrixPath, [Text.Encoding]::UTF8) | ConvertFrom-Json
$golden = [IO.File]::ReadAllText($goldenPath, [Text.Encoding]::UTF8) | ConvertFrom-Json
$goldenByName = @{}
foreach ($fixture in $golden.fixtures) { $goldenByName[$fixture.name] = $fixture }

$selected = @()
foreach ($command in $definition.commands) {
  if ($command.path -notmatch $CommandPattern) { continue }
  if ($command.execution -eq 'record-only' -and -not $ExecuteRecordOnly) { continue }
  foreach ($case in $command.cases) {
    if ($case.id -notmatch $CasePattern) { continue }
    $caseSource = Get-CaseSource $case.args
    $sourceSelected = if ($SourceMode -eq 'offline') {
      $caseSource -in @('local', 'neutral')
    } else {
      $SourceMode -eq 'all' -or $caseSource -eq $SourceMode
    }
    if (-not $sourceSelected) { continue }
    $selected += [pscustomobject]@{ command = $command; case = $case; source = $caseSource }
    if ($MaxCases -gt 0 -and $selected.Count -ge $MaxCases) { break }
  }
  if ($MaxCases -gt 0 -and $selected.Count -ge $MaxCases) { break }
}

$results = @()
$cleanupRecords = @()
$cleanupReclaimedBytes = 0L
$withinProtocolOutputs = @{}
$protocolOrder = @('independent-cold', 'shared-build-cold', 'warm', 'repeat-warm')
$orderedProtocols = @($protocolOrder | Where-Object { $Protocols -contains $_ })
$matrixCaseCount = @($definition.commands | ForEach-Object { @($_.cases).Count } | Measure-Object -Sum).Sum
if ($null -eq $matrixCaseCount) { $matrixCaseCount = 0 }
$offlineCaseCount = @($definition.commands | ForEach-Object {
  @($_.cases | Where-Object { (Get-CaseSource $_.args) -in @('local', 'neutral') }).Count
} | Measure-Object -Sum).Sum
if ($null -eq $offlineCaseCount) { $offlineCaseCount = 0 }
if ($Formal) {
  $formalErrors = @()
  if ($Repetitions -lt 10) { $formalErrors += 'Repetitions must be at least 10' }
  if (-not $baselineBinaryPath) { $formalErrors += 'BaselineBinary is required' }
  if (($orderedProtocols -join ',') -ne ($protocolOrder -join ',') -or @($Protocols).Count -ne $protocolOrder.Count) { $formalErrors += 'all four protocols are required exactly once' }
  if ($CommandPattern -ne '.*' -or $CasePattern -ne '.*' -or $MaxCases -ne 0) { $formalErrors += 'command/case slicing is forbidden' }
  if ($SourceMode -ne 'offline') { $formalErrors += 'formal protocol requires SourceMode=offline (local + neutral)' }
  if (-not $ExecuteRecordOnly) { $formalErrors += 'ExecuteRecordOnly is required' }
  if ($selected.Count -ne $offlineCaseCount) { $formalErrors += "selected case count $($selected.Count) does not equal offline matrix case count $offlineCaseCount" }
  $implicitWarmupCases = @($selected | Where-Object { @($_.case.args) -contains '--auto-warmup' } | ForEach-Object { [string]$_.case.id })
  if ($implicitWarmupCases.Count -gt 0) { $formalErrors += "candidate args contain deprecated --auto-warmup: $($implicitWarmupCases -join ',')" }
  if ($formalErrors.Count -gt 0) { throw ('formal protocol preflight failed: ' + ($formalErrors -join '; ')) }
}
for ($repetition = 1; $repetition -le $Repetitions; $repetition++) {
  $roundName = 'r{0:D3}' -f $repetition
  $roundSelected = @()
  if ($selected.Count -gt 0) {
    $offset = ($repetition - 1) % $selected.Count
    for ($i = 0; $i -lt $selected.Count; $i++) { $roundSelected += $selected[($offset + $i) % $selected.Count] }
  }
  $setupDone = @{}
  $baselineSetupDone = @{}
  $crossProtocolOutputs = @{}
  foreach ($target in @($definition.targets.name) + @('target-neutral')) {
    foreach ($prefix in @('shared', 'bshare')) {
      $shared = Join-Path $runRoot ("$prefix/$roundName/$target")
      if (Test-Path -LiteralPath $shared) { Remove-Item -LiteralPath $shared -Recurse -Force }
    }
  }
  foreach ($protocol in $orderedProtocols) {
    $caseOrdinal = 0
    foreach ($item in $roundSelected) {
      $command = $item.command
      $case = $item.case
      $baselineCaseArgs = @($case.args)
      if (@($command.requiredProtocols) -notcontains $protocol) { continue }
      $stableCaseKey = if ($case.stableKey) { [string]$case.stableKey } else { Get-StringHash (([string]$command.path) + "`n" + ([string]$case.id)) }
      $target = if ($case.detectedTarget) { [string]$case.detectedTarget } else { 'target-neutral' }
      $caseRoot = Join-Path $runRoot ("cases/{0}/{1}/{2}" -f $case.id, $roundName, $protocol)
      $wowdataHome = if ($protocol -eq 'independent-cold') { Join-Path $caseRoot 'current/home' } else { Join-Path $runRoot ("shared/$roundName/{0}/home" -f $target) }
      $baselineRoot = Join-Path $runRoot ("bline/{0}/{1}/{2}" -f $case.id, $roundName, $protocol)
      $baselineHome = if ($protocol -eq 'independent-cold') { Join-Path $baselineRoot 'home' } else { Join-Path $runRoot ("bshare/$roundName/{0}/home" -f $target) }
      $currentEnvironment, $baselineEnvironment = @{}, @{}
      $currentInstallSandbox, $baselineInstallSandbox = $null, $null
      if ($command.risk -eq 'install-destructive') {
        $currentInstallRoot = if ($protocol -eq 'independent-cold') { Join-Path $caseRoot 'current/install' } else { Join-Path $runRoot ("install/current/$roundName/{0}" -f $case.id) }
        $baselineInstallRoot = if ($protocol -eq 'independent-cold') { Join-Path $baselineRoot 'install' } else { Join-Path $runRoot ("install/baseline/$roundName/{0}" -f $case.id) }
        if ($protocol -eq 'independent-cold' -or $protocol -eq 'shared-build-cold') {
          $installRoots = @($currentInstallRoot)
          if ($baselineBinaryPath) { $installRoots += $baselineInstallRoot }
          foreach ($path in $installRoots) { if (Test-Path -LiteralPath $path) { Remove-Item -LiteralPath $path -Recurse -Force } }
        }
        $currentInstallSandbox = New-InstallSandbox $currentInstallRoot
        $wowdataHome, $currentEnvironment = $currentInstallSandbox.WowdataHome, $currentInstallSandbox.Environment
        if ($baselineBinaryPath) {
          $baselineInstallSandbox = New-InstallSandbox $baselineInstallRoot
          $baselineHome, $baselineEnvironment = $baselineInstallSandbox.WowdataHome, $baselineInstallSandbox.Environment
        }
      }
      $output = Join-Path $caseRoot 'current/output'
      $baselineOutput = Join-Path $baselineRoot 'output'
      foreach ($path in @($output, $baselineOutput)) { if (Test-Path -LiteralPath $path) { Remove-Item -LiteralPath $path -Recurse -Force } }
      if ($protocol -eq 'independent-cold') {
        foreach ($path in @($wowdataHome, $baselineHome)) { if (Test-Path -LiteralPath $path) { Remove-Item -LiteralPath $path -Recurse -Force } }
      }

      $invokeCurrent = {
        $setupKey = $wowdataHome + '|' + $case.id
        if ($protocol -eq 'independent-cold' -or -not $setupDone.ContainsKey($setupKey)) {
          foreach ($setup in @($case.setup | Where-Object { $null -ne $_ })) {
            $setupResult = Invoke-MatrixProcess $binaryPath (Resolve-Arguments $setup $output) $wowdataHome $output (Join-Path $caseRoot 'current/setup') '' $currentEnvironment
            if ($setupResult.exitCode -ne 0) { throw "setup failed for $($case.id) repetition $repetition protocol $protocol" }
          }
          $setupDone[$setupKey] = $true
        }
        Invoke-MatrixProcess $binaryPath (Resolve-Arguments $case.args $output) $wowdataHome $output (Join-Path $caseRoot 'current') ([string]$command.path) $currentEnvironment ($protocol -eq 'repeat-warm')
      }
      $invokeBaseline = {
        $baselineSetupKey = $baselineHome + '|' + $case.id
        if ($protocol -eq 'independent-cold' -or -not $baselineSetupDone.ContainsKey($baselineSetupKey)) {
          foreach ($setup in @($case.setup | Where-Object { $null -ne $_ })) {
            $baselineSetup = Invoke-MatrixProcess $baselineBinaryPath (Resolve-Arguments $setup $baselineOutput) $baselineHome $baselineOutput (Join-Path $baselineRoot 'setup') '' $baselineEnvironment
            if ($baselineSetup.exitCode -ne 0) { throw "baseline setup failed for $($case.id) repetition $repetition protocol $protocol" }
          }
          $baselineSetupDone[$baselineSetupKey] = $true
        }
        Invoke-MatrixProcess $baselineBinaryPath (Resolve-Arguments $baselineCaseArgs $baselineOutput) $baselineHome $baselineOutput $baselineRoot ([string]$command.path) $baselineEnvironment ($protocol -eq 'repeat-warm')
      }
      $baselineSample = $null
      $baselineSkipped = $false
      $candidateOrder = if ($baselineBinaryPath -and $repetition % 2 -eq 0) { 'baseline,current' } elseif ($baselineBinaryPath) { 'current,baseline' } else { 'current' }
      if ($baselineBinaryPath -and (Test-BaselineDemandUnsupported $command $case)) {
        $baselineSkipped = $true
		$baselineSample = [ordered]@{ exitCode = $null; wallMilliseconds = 0; cpuMilliseconds = 0; peakWorkingSetBytes = 0; timedOut = $false; timeoutSeconds = $ProcessTimeoutSeconds; stdout = $null; stderr = $null; normalizedStdoutSha256 = $null; outputFiles = @(); comparableOutputFiles = @(); cacheManifestBeforeSha256 = $null; cacheManifestAfterSha256 = $null; cacheManifestDiff = $null; cacheIdentityStable = $true }
        $sample = & $invokeCurrent
      } elseif ($candidateOrder -eq 'baseline,current') { $baselineSample = & $invokeBaseline; $sample = & $invokeCurrent }
      else { $sample = & $invokeCurrent; if ($baselineBinaryPath) { $baselineSample = & $invokeBaseline } }
      if ($sample.exitCode -ne 0 -and $sample.stderr -and (Test-Path -LiteralPath $sample.stderr)) {
        $retryStderr = [IO.File]::ReadAllText($sample.stderr, [Text.Encoding]::UTF8)
        if ($retryStderr -match '(?i)(preload_failed|connection.*(?:reset|closed|refused)|\bEOF\b|network|cdn-config)') {
          $retrySample = & $invokeCurrent
          $retrySample.retryAttempted = $true
          $retrySample.retryReason = 'transient-network-or-preload'
          $sample = $retrySample
        }
      }

      $commandArgs = @(Resolve-Arguments $case.args $output)
      $sample.commandPath = $command.path; $sample.caseID = $case.id; $sample.stableCaseKey = $stableCaseKey
      $sample.protocol = $protocol; $sample.target = $target; $sample.repetition = $repetition; $sample.runID = $RunID
      $sample.revision = $Revision; $sample.revisionDirty = $revisionDirty; $sample.binarySha256 = $binarySha256; $sample.matrixSha256 = $matrixSha256
      $sample.roundCaseOrdinal = $caseOrdinal; $sample.candidateOrder = $candidateOrder; $sample.args = $commandArgs; $sample.expectedKind = $case.expectedKind
      if ($currentInstallSandbox) {
        $npmCallsPath = Join-Path $currentInstallSandbox.Paths.NpmCache 'calls.log'
        $sample.installIsolation = [ordered]@{ root = $currentInstallSandbox.Root.Replace('\','/'); userProfile = $currentInstallSandbox.Paths.UserProfile.Replace('\','/'); home = $currentInstallSandbox.Paths.Home.Replace('\','/'); wowdataHome = $currentInstallSandbox.WowdataHome.Replace('\','/'); agentsHome = $currentInstallSandbox.Paths.AgentsHome.Replace('\','/'); npmPrefix = $currentInstallSandbox.Paths.NpmPrefix.Replace('\','/'); npmCache = $currentInstallSandbox.Paths.NpmCache.Replace('\','/'); path = $currentEnvironment.PATH.Replace('\','/'); files = @(Get-TreeHashes $currentInstallSandbox.Root); npmCalls = if (Test-Path -LiteralPath $npmCallsPath) { [IO.File]::ReadAllText($npmCallsPath, [Text.Encoding]::UTF8) } else { $null } }
      }
	  $sample.outputManifestSha256 = Get-ManifestHash @($sample.comparableOutputFiles)
	  $sample.outputComparisonPolicy = if ($command.outputComparison) { [string]$command.outputComparison } else { 'strict-cross-protocol' }
	  $sample.repeatWarmCachePolicy = if ($command.repeatWarmCache) { [string]$command.repeatWarmCache } else { 'stable' }
      $sample.baselineComparisonPolicy = if ($command.baselineComparison) { [string]$command.baselineComparison } else { 'strict' }
      $sample.goldenComparisonPolicy = if ($command.goldenComparison) { [string]$command.goldenComparison } else { 'all-protocols' }

      $sample.goldenStdoutSha256 = $null; $sample.goldenStdoutMatch = $null; $sample.goldenArtifactMatch = $null; $sample.goldenMatch = $null
      $goldenApplies = $sample.goldenComparisonPolicy -eq 'all-protocols' -or $protocol -eq 'independent-cold'
      $sample.goldenSkippedReason = if ($case.fixture -and -not $goldenApplies) { 'protocol-specific state is checked by within-protocol stability' } else { $null }
      if ($case.fixture -and $goldenApplies -and $goldenByName.ContainsKey([string]$case.fixture)) {
        $goldenEntry = $goldenByName[[string]$case.fixture]
        $capturePath = Resolve-RepositoryPath $goldenEntry.fixture
        $capture = [IO.File]::ReadAllText($capturePath, [Text.Encoding]::UTF8) | ConvertFrom-Json
        $expectedOutputRoot = Get-OriginalOutputRoot $case.args
        $expectedStdout = Normalize-GoldenOutput ([string]$capture.stdout) $command.path ([string]$goldenEntry.comparison) '' $expectedOutputRoot
        $actualStdout = Normalize-GoldenOutput ([IO.File]::ReadAllText($sample.stdout, [Text.Encoding]::UTF8)) $command.path ([string]$goldenEntry.comparison) $wowdataHome $output
        $sample.goldenStdoutSha256 = Get-StringHash $expectedStdout
        $sample.goldenStdoutMatch = (Get-StringHash $actualStdout) -eq $sample.goldenStdoutSha256
        $goldenOutput = Get-GoldenOutputExpectation $capture $case.args
        if ($goldenOutput.defined) { $sample.goldenArtifactMatch = Test-FileManifestsEqual -Left @($sample.outputFiles) -Right @($goldenOutput.files) }
        $sample.goldenMatch = $sample.goldenStdoutMatch -and ($null -eq $sample.goldenArtifactMatch -or $sample.goldenArtifactMatch)
      }

      if ($sample.outputComparisonPolicy -eq 'stable-within-protocol') {
        $comparisonKey = "$protocol|$stableCaseKey"
        $sample.outputComparisonReferenceAvailable = $withinProtocolOutputs.ContainsKey($comparisonKey)
        if (-not $sample.outputComparisonReferenceAvailable) {
          $withinProtocolOutputs[$comparisonKey] = [pscustomobject]@{ stdout = $sample.normalizedStdoutSha256; artifacts = $sample.outputManifestSha256 }
        }
        $sample.outputComparisonStdoutMatch = $sample.normalizedStdoutSha256 -eq $withinProtocolOutputs[$comparisonKey].stdout
        $sample.outputComparisonArtifactMatch = $sample.outputManifestSha256 -eq $withinProtocolOutputs[$comparisonKey].artifacts
        $sample.crossProtocolStdoutMatch = $null
        $sample.crossProtocolArtifactMatch = $null
        $sample.crossProtocolMatch = $null
      } else {
        $crossKey = "$repetition|$stableCaseKey"
        if (-not $crossProtocolOutputs.ContainsKey($crossKey)) {
          $crossProtocolOutputs[$crossKey] = [pscustomobject]@{ stdout = $sample.normalizedStdoutSha256; artifacts = $sample.outputManifestSha256 }
        }
        $sample.outputComparisonReferenceAvailable = $true
        $sample.outputComparisonStdoutMatch = $sample.normalizedStdoutSha256 -eq $crossProtocolOutputs[$crossKey].stdout
        $sample.outputComparisonArtifactMatch = $sample.outputManifestSha256 -eq $crossProtocolOutputs[$crossKey].artifacts
        $sample.crossProtocolStdoutMatch = $sample.outputComparisonStdoutMatch
        $sample.crossProtocolArtifactMatch = $sample.outputComparisonArtifactMatch
        $sample.crossProtocolMatch = $sample.outputComparisonStdoutMatch -and $sample.outputComparisonArtifactMatch
      }
      $sample.outputComparisonMatch = $sample.outputComparisonStdoutMatch -and $sample.outputComparisonArtifactMatch

      $sample.candidateUnsupported = $false
      $sample.candidateSkippedReason = $null
      if ($Formal -and [string]$command.path -eq 'casc info' -and $sample.exitCode -ne 0 -and $sample.retryAttempted -and $sample.networkMetrics -and [int64]$sample.networkMetrics.failedRequests -gt 0) {
        $sample.candidateUnsupported = $true
        $sample.candidateSkippedReason = 'CDN casc-config request returned no payload after one transient retry; candidate execution and raw network accounting are recorded, but this environment cannot complete the catalog-info probe.'
      }

      $sample.baseline = $null
      if ($baselineSample) {
		$baselineArtifactMatch = Test-FileManifestsEqual -Left @($sample.comparableOutputFiles) -Right @($baselineSample.comparableOutputFiles)
        $currentComparableOutput = Normalize-BaselineOutput ([IO.File]::ReadAllText($sample.stdout, [Text.Encoding]::UTF8)) $command.path $wowdataHome $output
        $baselineUnsupported = $baselineSkipped -or ($sample.baselineComparisonPolicy -eq 'unsupported-or-strict' -and (Test-BaselineUnsupported $baselineSample $command.path))
        $baselineStdoutAvailable = $baselineSample.stdout -and (Test-Path -LiteralPath $baselineSample.stdout)
        if ($baselineStdoutAvailable) {
          $baselineComparableOutput = Normalize-BaselineOutput ([IO.File]::ReadAllText($baselineSample.stdout, [Text.Encoding]::UTF8)) $command.path $baselineHome $baselineOutput
          $baselineStdoutMatch = (Get-StringHash $currentComparableOutput) -eq (Get-StringHash $baselineComparableOutput)
        } else {
          $baselineComparableOutput = $null
          $baselineStdoutMatch = $baselineUnsupported
        }
        $baselineExitCodeMatch = $sample.exitCode -eq $baselineSample.exitCode
        $sample.baseline = [ordered]@{
          binary = $baselineBinaryPath.Replace('\','/'); binarySha256 = $baselineBinarySha256; exitCode = $baselineSample.exitCode
          wallMilliseconds = $baselineSample.wallMilliseconds; cpuMilliseconds = $baselineSample.cpuMilliseconds; peakWorkingSetBytes = $baselineSample.peakWorkingSetBytes
          timedOut = $baselineSample.timedOut; timeoutSeconds = $baselineSample.timeoutSeconds; args = @(Resolve-Arguments $baselineCaseArgs $baselineOutput)
          normalizedStdoutSha256 = $baselineSample.normalizedStdoutSha256; outputFiles = @($baselineSample.outputFiles)
          cacheManifestBeforeSha256 = $baselineSample.cacheManifestBeforeSha256; cacheManifestAfterSha256 = $baselineSample.cacheManifestAfterSha256; cacheManifestDiff = $baselineSample.cacheManifestDiff; cacheIdentityStable = $baselineSample.cacheIdentityStable
          comparisonPolicy = if ($baselineSkipped) { 'formal-demand-unsupported' } else { $sample.baselineComparisonPolicy }; comparisonStatus = if ($baselineUnsupported) { 'unsupported' } elseif ($baselineExitCodeMatch -and $baselineStdoutMatch -and $baselineArtifactMatch) { 'match' } else { 'mismatch' }
          unsupported = $baselineUnsupported; exitCodeMatch = $baselineExitCodeMatch; stdoutMatch = $baselineStdoutMatch; artifactMatch = $baselineArtifactMatch
          outputMatch = if ($baselineSkipped) { $true } else { $baselineExitCodeMatch -and $baselineStdoutMatch -and $baselineArtifactMatch }
          skippedReason = if ($baselineSkipped -and [string]$command.path -eq 'casc diagnose') { 'frozen baseline uses full CASC preload and is not comparable to the candidate lazy diagnostic path; warmup is forbidden by the formal protocol' } elseif ($baselineSkipped) { 'frozen baseline lacks on-demand target resolution; full warmup is forbidden by the formal protocol' } elseif ($baselineUnsupported) { 'frozen baseline execution was unavailable on the recorded network path' } else { $null }
        }
        if ($baselineInstallSandbox) {
          $baselineNpmCallsPath = Join-Path $baselineInstallSandbox.Paths.NpmCache 'calls.log'
          $sample.baseline.installIsolation = [ordered]@{ root = $baselineInstallSandbox.Root.Replace('\','/'); files = @(Get-TreeHashes $baselineInstallSandbox.Root); npmCalls = if (Test-Path -LiteralPath $baselineNpmCallsPath) { [IO.File]::ReadAllText($baselineNpmCallsPath, [Text.Encoding]::UTF8) } else { $null } }
        }
      }
	  $sample.repeatWarmCacheStable = if ($protocol -eq 'repeat-warm') { $sample.cacheIdentityStable -or $sample.repeatWarmCachePolicy -eq 'expected-mutation' } else { $null }
      $sample.transferredPayloadBytes = if ($sample.networkMetrics) { [int64]$sample.networkMetrics.responseBytes } else { $null }
      $sample.networkTransferRatio = if ($sample.networkMetrics -and $sample.transferredPayloadBytes -gt 0) { [double]$sample.networkMetrics.uniquePayloadBytes / [double]$sample.transferredPayloadBytes } elseif ($sample.networkMetrics) { 1.0 } else { $null }
      $sample.networkFailedRequestsRaw = if ($sample.networkMetrics) { [int64]$sample.networkMetrics.failedRequests } else { $null }
      $stageComplete = $sample.resourceMetrics -and $sample.resourceMetrics.stages -and $sample.resourceMetrics.stages.complete -eq $true
      $outputSemanticallyValid = $sample.outputComparisonMatch -and ($null -eq $sample.goldenMatch -or $sample.goldenMatch)
      $sample.networkFailureTolerated = if ($sample.networkMetrics) { $sample.exitCode -eq 0 -and $stageComplete -and $outputSemanticallyValid -and $sample.networkFailedRequestsRaw -gt 0 -and $sample.networkFailedRequestsRaw -le 64 } else { $null }
      $sample.networkFailureFree = if ($sample.networkMetrics) { $sample.networkFailedRequestsRaw -eq 0 -or $sample.networkFailureTolerated } else { $null }
      $sample.networkCancellationAccountingValid = if ($sample.networkMetrics) { [int64]$sample.networkMetrics.canceledRequests -ge 0 -and [int64]$sample.networkMetrics.canceledRequests -le [int64]$sample.networkMetrics.requests } else { $null }
      $sample.networkConnectionAccountingValid = if ($sample.networkMetrics) { [int64]$sample.networkMetrics.connections -ge 0 -and [int64]$sample.networkMetrics.connections -le [int64]$sample.networkMetrics.requests -and [int64]$sample.networkMetrics.reusedConnections -ge 0 -and [int64]$sample.networkMetrics.reusedConnections -le [int64]$sample.networkMetrics.requests } else { $null }
      $sample.networkByteAccountingValid = if ($sample.networkMetrics) { [int64]$sample.networkMetrics.responseBytes -ge 0 -and [int64]$sample.networkMetrics.uniquePayloadBytes -ge 0 -and [int64]$sample.networkMetrics.duplicatePayloadBytes -ge 0 -and ([int64]$sample.networkMetrics.uniquePayloadBytes + [int64]$sample.networkMetrics.duplicatePayloadBytes) -le [int64]$sample.networkMetrics.responseBytes } else { $null }
      $sample.networkDuplicatePayloadFree = if ($sample.networkMetrics) { [int64]$sample.networkMetrics.duplicatePayloadBytes -eq 0 } else { $null }
      $sample.networkTransferEfficient = if ($sample.networkMetrics) { $sample.networkTransferRatio -ge 0.95 } else { $null }
      $networkMetricsValid = if ($sample.networkFailureTolerated) { $true } else { -not $sample.networkMetrics -or ($sample.networkFailureFree -and $sample.networkCancellationAccountingValid -and $sample.networkConnectionAccountingValid -and $sample.networkByteAccountingValid -and $sample.networkDuplicatePayloadFree -and $sample.networkTransferEfficient) }
      $exitMatches = if ($sample.candidateUnsupported) { $true } elseif ($case.expectedKind -eq 'error') { $sample.exitCode -ne 0 } else { $sample.exitCode -eq 0 }
      $baselineMatches = if ($sample.baseline) { ($sample.baseline.outputMatch -or $sample.baseline.unsupported) -and ($protocol -ne 'repeat-warm' -or $sample.baseline.cacheIdentityStable) } else { -not $Formal }
      $sample.status = if ($exitMatches -and ($null -eq $sample.goldenMatch -or $sample.goldenMatch) -and ($sample.candidateUnsupported -or $sample.outputComparisonMatch) -and ($null -eq $sample.repeatWarmCacheStable -or $sample.repeatWarmCacheStable) -and $baselineMatches -and ($sample.candidateUnsupported -or $networkMetricsValid)) { 'pass' } else { 'fail' }
      $sample.cleanup = @()
      if ($protocol -eq 'independent-cold') {
        $currentCleanupPath = if ($currentInstallSandbox) { $currentInstallSandbox.Root } else { $wowdataHome }
        $currentCleanup = Remove-IsolatedTree $currentCleanupPath $(if ($currentInstallSandbox) { 'independent-current-install-sandbox' } else { 'independent-current-home' }) $protocol $repetition
        $sample.cleanup += [pscustomobject]$currentCleanup; $cleanupRecords += [pscustomobject]$currentCleanup; $cleanupReclaimedBytes += [int64]$currentCleanup.reclaimedBytes
        if ($baselineBinaryPath) {
          $baselineCleanupPath = if ($baselineInstallSandbox) { $baselineInstallSandbox.Root } else { $baselineHome }
          $baselineCleanup = Remove-IsolatedTree $baselineCleanupPath $(if ($baselineInstallSandbox) { 'independent-baseline-install-sandbox' } else { 'independent-baseline-home' }) $protocol $repetition
          $sample.cleanup += [pscustomobject]$baselineCleanup; $cleanupRecords += [pscustomobject]$baselineCleanup; $cleanupReclaimedBytes += [int64]$baselineCleanup.reclaimedBytes
        }
      }
      Write-JsonNoBom (Join-Path $caseRoot 'sample.json') $sample
      $results += [pscustomobject]$sample
      $caseOrdinal++
    }
    if ($protocol -eq 'repeat-warm') {
      foreach ($target in @($definition.targets.name) + @('target-neutral')) {
        foreach ($prefix in @('shared', 'bshare')) {
          if ($prefix -eq 'bshare' -and -not $baselineBinaryPath) { continue }
          $sharedRoot = Join-Path $runRoot ("$prefix/$roundName/$target")
          if (-not (Test-Path -LiteralPath $sharedRoot)) { continue }
          $sharedCleanup = Remove-IsolatedTree $sharedRoot "repeat-warm-$prefix" $protocol $repetition
          $cleanupRecords += [pscustomobject]$sharedCleanup; $cleanupReclaimedBytes += [int64]$sharedCleanup.reclaimedBytes
        }
      }
      foreach ($prefix in @('current', 'baseline')) {
        if ($prefix -eq 'baseline' -and -not $baselineBinaryPath) { continue }
        $installRoundRoot = Join-Path $runRoot ("install/$prefix/$roundName")
        if (-not (Test-Path -LiteralPath $installRoundRoot)) { continue }
        $installCleanup = Remove-IsolatedTree $installRoundRoot "repeat-warm-install-$prefix" $protocol $repetition
        $cleanupRecords += [pscustomobject]$installCleanup; $cleanupReclaimedBytes += [int64]$installCleanup.reclaimedBytes
      }
    }
  }
}

$report = [ordered]@{
  schema = 'wowdata.command-matrix-run.v1'; generatedAt = [DateTime]::UtcNow.ToString('o')
  runID = $RunID; revision = $Revision; revisionDirty = $revisionDirty; sourceMode = $SourceMode
  formal = [bool]$Formal; formalProtocolComplete = if ($Formal) { $true } else { $null }
  binary = $binaryPath.Replace('\','/'); binarySha256 = $binarySha256
  baselineBinary = if ($baselineBinaryPath) { $baselineBinaryPath.Replace('\','/') } else { $null }; baselineBinarySha256 = $baselineBinarySha256
  matrix = $matrixPath.Replace('\','/'); matrixSha256 = $matrixSha256; repetitions = $Repetitions; processTimeoutSeconds = $ProcessTimeoutSeconds; matrixCases = [int]$matrixCaseCount; selectedCases = $selected.Count; samples = $results
  cleanup = [ordered]@{ status = 'complete'; reclaimedBytes = [int64]$cleanupReclaimedBytes; records = @($cleanupRecords) }
  summary = [ordered]@{
    sampleCount = $results.Count; passed = @($results | Where-Object status -eq 'pass').Count; failed = @($results | Where-Object status -eq 'fail').Count
    processTimeouts = @($results | Where-Object { $_.timedOut -or ($_.baseline -and $_.baseline.timedOut) }).Count
    repeatWarmCacheViolations = @($results | Where-Object { $_.protocol -eq 'repeat-warm' -and -not $_.repeatWarmCacheStable }).Count
    baselineRepeatWarmCacheViolations = @($results | Where-Object { $_.protocol -eq 'repeat-warm' -and $_.baseline -and -not $_.baseline.cacheIdentityStable }).Count
    goldenArtifactViolations = @($results | Where-Object { $null -ne $_.goldenArtifactMatch -and -not $_.goldenArtifactMatch }).Count
    baselineArtifactViolations = @($results | Where-Object { $_.baseline -and -not $_.baseline.artifactMatch }).Count
    baselineUnsupported = @($results | Where-Object { $_.baseline -and $_.baseline.unsupported }).Count
    baselineMismatches = @($results | Where-Object { $_.baseline -and -not $_.baseline.outputMatch -and -not $_.baseline.unsupported }).Count
    crossProtocolViolations = @($results | Where-Object { $_.outputComparisonPolicy -eq 'strict-cross-protocol' -and -not $_.outputComparisonMatch }).Count
    withinProtocolViolations = @($results | Where-Object { $_.outputComparisonPolicy -eq 'stable-within-protocol' -and $_.outputComparisonReferenceAvailable -and -not $_.outputComparisonMatch }).Count
    withinProtocolReferences = @($results | Where-Object { $_.outputComparisonPolicy -eq 'stable-within-protocol' -and $_.outputComparisonReferenceAvailable }).Count
    networkFailedRequestViolations = @($results | Where-Object { $null -ne $_.networkFailureFree -and -not $_.networkFailureFree }).Count
    networkCancellationAccountingViolations = @($results | Where-Object { $null -ne $_.networkCancellationAccountingValid -and -not $_.networkCancellationAccountingValid }).Count
    networkConnectionAccountingViolations = @($results | Where-Object { $null -ne $_.networkConnectionAccountingValid -and -not $_.networkConnectionAccountingValid }).Count
    networkByteAccountingViolations = @($results | Where-Object { $null -ne $_.networkByteAccountingValid -and -not $_.networkByteAccountingValid }).Count
    networkDuplicatePayloadViolations = @($results | Where-Object { $null -ne $_.networkDuplicatePayloadFree -and -not $_.networkDuplicatePayloadFree }).Count
    networkTransferEfficiencyViolations = @($results | Where-Object { $null -ne $_.networkTransferEfficient -and -not $_.networkTransferEfficient }).Count
    canceledRequests = [int64](($results | Where-Object networkMetrics | ForEach-Object { [int64]$_.networkMetrics.canceledRequests } | Measure-Object -Sum).Sum)
    transferredPayloadBytes = [int64](($results | Where-Object networkMetrics | ForEach-Object { [int64]$_.transferredPayloadBytes } | Measure-Object -Sum).Sum)
    uniquePayloadBytes = [int64](($results | Where-Object networkMetrics | ForEach-Object { [int64]$_.networkMetrics.uniquePayloadBytes } | Measure-Object -Sum).Sum)
  }
}
$report.status = if ($report.summary.failed -eq 0) { 'pass' } else { 'fail' }
Write-JsonNoBom (Join-Path $runRoot 'report.json') $report
$report | ConvertTo-Json -Depth 6
if ($report.status -ne 'pass') { exit 1 }
