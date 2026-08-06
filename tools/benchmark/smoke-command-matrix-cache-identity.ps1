param([string]$OutputRoot = "analyze/benchmark/command-matrix-cache-identity-smoke")

$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
Set-Location $repo
$root = [IO.Path]::GetFullPath((Join-Path $repo $OutputRoot))
$utf8 = [Text.UTF8Encoding]::new($false)
New-Item -ItemType Directory -Force -Path $root | Out-Null

function Write-JSON([string]$Path, $Value) { [IO.File]::WriteAllText($Path, (($Value | ConvertTo-Json -Depth 12) + [Environment]::NewLine), $utf8) }

$helper = Join-Path $root 'cache-fixture.ps1'
$source = @'
param([ValidateSet('stable','toggle')][string]$mode)
$cache = Join-Path $env:WOWDATA_HOME 'cache'
New-Item -ItemType Directory -Force -Path $cache | Out-Null
$payload = Join-Path $cache 'payload.bin'
if ($mode -eq 'stable') { $value = 'AAAA' }
elseif ((Test-Path -LiteralPath $payload) -and [IO.File]::ReadAllText($payload) -eq 'AAAA') { $value = 'BBBB' }
else { $value = 'AAAA' }
[IO.File]::WriteAllText($payload, $value, [Text.Encoding]::ASCII)
'{"ok":true,"command":"cache fixture","data":{"bytes":4},"warnings":[]}'
'@
[IO.File]::WriteAllText($helper, $source, $utf8)

function Write-Matrix([string]$Path, [string]$Mode) {
  $case = [ordered]@{ id = "cache-$Mode"; stableKey = "cache-$Mode"; args = @('-NoProfile','-File',$helper,'-mode',$Mode); expectedKind = 'success' }
  Write-JSON $Path ([ordered]@{ schema = 'wowdata.command-benchmark-matrix.v1'; targets = @(); commands = @([ordered]@{ path = 'cache fixture'; risk = 'read-only'; execution = 'execute'; requiredProtocols = @('independent-cold','shared-build-cold','warm','repeat-warm'); cases = @($case) }) })
}

$runner = Join-Path $PSScriptRoot 'run-command-matrix.ps1'
$powershell = (Get-Command powershell).Source
$golden = Join-Path $root 'golden.json'
Write-JSON $golden ([ordered]@{ fixtures = @() })

$stableMatrix = Join-Path $root 'stable-matrix.json'
Write-Matrix $stableMatrix 'stable'
& powershell -NoProfile -File $runner -Binary $powershell -Matrix $stableMatrix -GoldenManifest $golden -OutputRoot (Join-Path $OutputRoot 'stable') *> $null
if ($LASTEXITCODE -ne 0) { throw "stable cache fixture exited $LASTEXITCODE" }
$stable = Get-Content -Raw (Join-Path $root 'stable/report.json') | ConvertFrom-Json
$stableRepeat = @($stable.samples | Where-Object protocol -eq 'repeat-warm')[0]
if (-not $stableRepeat.repeatWarmCacheStable -or $stableRepeat.cacheManifestBeforeSha256 -ne $stableRepeat.cacheManifestAfterSha256) { throw 'stable cache identity did not pass' }

$toggleMatrix = Join-Path $root 'toggle-matrix.json'
Write-Matrix $toggleMatrix 'toggle'
& powershell -NoProfile -File $runner -Binary $powershell -Matrix $toggleMatrix -GoldenManifest $golden -OutputRoot (Join-Path $OutputRoot 'toggle') *> $null
if ($LASTEXITCODE -eq 0) { throw 'same-size cache mutation unexpectedly passed' }
$toggle = Get-Content -Raw (Join-Path $root 'toggle/report.json') | ConvertFrom-Json
$toggleRepeat = @($toggle.samples | Where-Object protocol -eq 'repeat-warm')[0]
if ($toggleRepeat.cacheDeltaBytes -ne 0 -or $toggleRepeat.repeatWarmCacheStable -or @($toggleRepeat.cacheManifestDiff.changed) -notcontains 'payload.bin') { throw 'same-size mutation was not reported as a changed cache path' }

[ordered]@{ schema = 'wowdata.command-matrix-cache-identity-smoke.v1'; status = 'pass'; stableReport = (Join-Path $root 'stable/report.json').Replace('\','/'); toggleReport = (Join-Path $root 'toggle/report.json').Replace('\','/') } | ConvertTo-Json
