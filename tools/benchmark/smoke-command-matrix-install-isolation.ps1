param(
  [string]$Binary = "analyze/benchmark/wowdata-current.exe",
  [string]$OutputRoot = "analyze/benchmark/command-matrix-install-isolation-smoke"
)

$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
Set-Location $repo
$root = [IO.Path]::GetFullPath((Join-Path $repo $OutputRoot))
$utf8 = [Text.UTF8Encoding]::new($false)
New-Item -ItemType Directory -Force -Path $root | Out-Null
$sentinelName = 'wowdata-benchmark-npm-touched.txt'
$realSentinel = Join-Path $env:USERPROFILE $sentinelName
$realSentinelBefore = if (Test-Path -LiteralPath $realSentinel) { (Get-FileHash -LiteralPath $realSentinel -Algorithm SHA256).Hash } else { $null }

function Write-Shim([string]$Path, [switch]$Fail) {
  $failure = if ($Fail) { 'exit /b 42' } else { '' }
  $source = @"
@echo off
if "%USERPROFILE%"=="" exit /b 91
if "%HOME%"=="" exit /b 92
if "%WOWDATA_HOME%"=="" exit /b 93
if "%AGENTS_HOME%"=="" exit /b 94
if "%npm_config_prefix%"=="" exit /b 95
if "%npm_config_cache%"=="" exit /b 96
if not exist "%USERPROFILE%" mkdir "%USERPROFILE%"
if not exist "%npm_config_cache%" mkdir "%npm_config_cache%"
if not exist "%npm_config_prefix%" mkdir "%npm_config_prefix%"
> "%USERPROFILE%\$sentinelName" echo sandbox-only
>> "%npm_config_cache%\calls.log" echo %*
$failure
if "%1"=="install" > "%npm_config_prefix%\installed.marker" echo installed
if "%1"=="uninstall" if exist "%npm_config_prefix%\installed.marker" del /q "%npm_config_prefix%\installed.marker"
echo fake npm %1 complete
exit /b 0
"@
  [IO.File]::WriteAllText($Path, $source, [Text.Encoding]::ASCII)
}

$matrixRoot = Join-Path $root 'matrix'
& powershell -NoProfile -File (Join-Path $PSScriptRoot 'generate-command-matrix.ps1') -OutputRoot $matrixRoot *> $null
if ($LASTEXITCODE -ne 0) { throw "matrix generation exited $LASTEXITCODE" }
$matrix = Join-Path $matrixRoot 'matrix.json'
$runner = Join-Path $PSScriptRoot 'run-command-matrix.ps1'
$passShim = Join-Path $root 'npm-pass.cmd'
Write-Shim $passShim
& powershell -NoProfile -File $runner -Binary $Binary -Matrix $matrix -OutputRoot (Join-Path $OutputRoot 'pass') -CommandPattern '^(update|uninstall)$' -CasePattern 'isolated-execution' -Protocols independent-cold -Repetitions 1 -ExecuteRecordOnly -NPMShim $passShim *> $null
if ($LASTEXITCODE -ne 0) { throw "isolated install pass runner exited $LASTEXITCODE" }
$pass = Get-Content -Raw (Join-Path $root 'pass/report.json') | ConvertFrom-Json
if ($pass.samples.Count -ne 2 -or $pass.status -ne 'pass') { throw 'isolated update/uninstall samples did not pass' }
foreach ($sample in $pass.samples) {
  $isolationRoot = [IO.Path]::GetFullPath($sample.installIsolation.root)
  if (-not $isolationRoot.StartsWith([IO.Path]::GetFullPath((Join-Path $root 'pass')), [StringComparison]::OrdinalIgnoreCase)) { throw "$($sample.commandPath) escaped pass root" }
  $sandboxSentinel = ('user-profile/' + $sentinelName)
  if (@($sample.installIsolation.files.path) -notcontains $sandboxSentinel) { throw "$($sample.commandPath) did not execute npm inside sandbox" }
  $calls = [string]$sample.installIsolation.npmCalls
  $verb = if ($sample.commandPath -eq 'update') { 'install -g' } else { 'uninstall -g' }
  if ($calls -notmatch [regex]::Escape($verb)) { throw "$($sample.commandPath) npm invocation is $calls" }
  if (Test-Path -LiteralPath $sample.installIsolation.root) { throw "$($sample.commandPath) install sandbox was not cleaned" }
}

$failShim = Join-Path $root 'npm-fail.cmd'
Write-Shim $failShim -Fail
& powershell -NoProfile -File $runner -Binary $Binary -Matrix $matrix -OutputRoot (Join-Path $OutputRoot 'fail') -CommandPattern '^update$' -CasePattern 'isolated-execution' -Protocols independent-cold -Repetitions 1 -ExecuteRecordOnly -NPMShim $failShim *> $null
if ($LASTEXITCODE -eq 0) { throw 'failing npm shim unexpectedly passed' }
$fail = Get-Content -Raw (Join-Path $root 'fail/report.json') | ConvertFrom-Json
if ($fail.status -ne 'fail' -or $fail.samples.Count -ne 1 -or $fail.samples[0].exitCode -eq 0) { throw 'npm failure was not preserved in matrix evidence' }
$failSentinel = ('user-profile/' + $sentinelName)
if (@($fail.samples[0].installIsolation.files.path) -notcontains $failSentinel) { throw 'failing npm did not run in its sandbox' }
if (Test-Path -LiteralPath $fail.samples[0].installIsolation.root) { throw 'failing install sandbox was not cleaned' }

$realSentinelAfter = if (Test-Path -LiteralPath $realSentinel) { (Get-FileHash -LiteralPath $realSentinel -Algorithm SHA256).Hash } else { $null }
if ($realSentinelBefore -ne $realSentinelAfter) { throw "real USERPROFILE sentinel changed: $realSentinel" }

[ordered]@{ schema = 'wowdata.command-matrix-install-isolation-smoke.v1'; status = 'pass'; passReport = (Join-Path $root 'pass/report.json').Replace('\','/'); failReport = (Join-Path $root 'fail/report.json').Replace('\','/'); realUserProfileUnchanged = $true } | ConvertTo-Json
