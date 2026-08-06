param([string]$OutputRoot = "analyze/benchmark/command-matrix-network-gate-smoke")

$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
Set-Location $repo
$root = [IO.Path]::GetFullPath((Join-Path $repo $OutputRoot))
$utf8 = [Text.UTF8Encoding]::new($false)
New-Item -ItemType Directory -Force -Path $root | Out-Null

function Write-JSON([string]$Path, $Value) { [IO.File]::WriteAllText($Path, (($Value | ConvertTo-Json -Depth 12) + [Environment]::NewLine), $utf8) }

$helper = Join-Path $root 'network-fixture.ps1'
$source = @'
param([ValidateSet('efficient','wasteful')][string]$mode)
$metrics = if ($mode -eq 'efficient') {
  [ordered]@{ requests=2; rangeRequests=2; failedRequests=0; canceledRequests=1; connections=1; reusedConnections=1; responseBytes=100; uniquePayloadBytes=100; duplicatePayloadBytes=0; requestTimeNanos=1; protocols=@{ 'HTTP/2.0'=2 } }
} else {
  [ordered]@{ requests=2; rangeRequests=2; failedRequests=0; canceledRequests=0; connections=1; reusedConnections=1; responseBytes=100; uniquePayloadBytes=90; duplicatePayloadBytes=10; requestTimeNanos=1; protocols=@{ 'HTTP/2.0'=2 } }
}
[Console]::Error.WriteLine('network metrics=' + ($metrics | ConvertTo-Json -Compress))
'{"ok":true,"command":"network fixture","data":{},"warnings":[]}'
'@
[IO.File]::WriteAllText($helper, $source, $utf8)

function Write-Matrix([string]$Path, [string]$Mode) {
  $case = [ordered]@{ id = "network-$Mode"; stableKey = "network-$Mode"; args = @('-NoProfile','-File',$helper,'-mode',$Mode); expectedKind = 'success' }
  Write-JSON $Path ([ordered]@{ schema='wowdata.command-benchmark-matrix.v1'; targets=@(); commands=@([ordered]@{ path='network fixture'; risk='read-only'; execution='execute'; outputComparison='strict-cross-protocol'; baselineComparison='strict'; requiredProtocols=@('independent-cold'); cases=@($case) }) })
}

$runner = Join-Path $PSScriptRoot 'run-command-matrix.ps1'
$powershell = (Get-Command powershell).Source
$golden = Join-Path $root 'golden.json'
Write-JSON $golden ([ordered]@{ fixtures=@() })

$passMatrix = Join-Path $root 'pass-matrix.json'
Write-Matrix $passMatrix 'efficient'
& powershell -NoProfile -File $runner -Binary $powershell -Matrix $passMatrix -GoldenManifest $golden -OutputRoot (Join-Path $OutputRoot 'pass') -Protocols independent-cold *> $null
if ($LASTEXITCODE -ne 0) { throw "efficient transfer exited $LASTEXITCODE" }
$pass = Get-Content -Raw (Join-Path $root 'pass/report.json') | ConvertFrom-Json
if (-not $pass.samples[0].networkTransferEfficient -or $pass.samples[0].transferredPayloadBytes -ne 100 -or $pass.summary.canceledRequests -ne 1) { throw 'efficient transfer metrics were not preserved' }

$failMatrix = Join-Path $root 'fail-matrix.json'
Write-Matrix $failMatrix 'wasteful'
& powershell -NoProfile -File $runner -Binary $powershell -Matrix $failMatrix -GoldenManifest $golden -OutputRoot (Join-Path $OutputRoot 'fail') -Protocols independent-cold *> $null
if ($LASTEXITCODE -eq 0) { throw 'duplicate transfer unexpectedly passed' }
$fail = Get-Content -Raw (Join-Path $root 'fail/report.json') | ConvertFrom-Json
if ($fail.summary.networkDuplicatePayloadViolations -ne 1 -or $fail.summary.networkTransferEfficiencyViolations -ne 1 -or $fail.samples[0].networkTransferRatio -ne 0.9) { throw 'wasteful transfer violations were not reported' }

[ordered]@{ schema='wowdata.command-matrix-network-gate-smoke.v1'; status='pass'; passReport=(Join-Path $root 'pass/report.json').Replace('\','/'); failReport=(Join-Path $root 'fail/report.json').Replace('\','/') } | ConvertTo-Json
