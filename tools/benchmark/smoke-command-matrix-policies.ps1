param([string]$OutputRoot = "analyze/benchmark/command-matrix-policy-smoke")

$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
Set-Location $repo
$root = [IO.Path]::GetFullPath((Join-Path $repo $OutputRoot))
$utf8 = [Text.UTF8Encoding]::new($false)
New-Item -ItemType Directory -Force -Path $root | Out-Null

function Write-JSON([string]$Path, $Value) {
  [IO.File]::WriteAllText($Path, (($Value | ConvertTo-Json -Depth 12) + [Environment]::NewLine), $utf8)
}

$helper = Join-Path $root 'policy-fixture.ps1'
$helperSource = @'
param([ValidateSet('unsupported','mismatch')][string]$mode)
$baseline = $env:WOWDATA_HOME -match '[\\/](bline|bshare)[\\/]'
if ($baseline -and $mode -eq 'unsupported') {
  [Console]::Error.WriteLine('unknown command "export" for "wowdata encounter"')
  exit 2
}
$value = if ($baseline) { 'baseline' } else { 'current' }
[ordered]@{ ok = $true; command = 'policy fixture'; data = [ordered]@{ value = $value }; warnings = @() } | ConvertTo-Json -Compress
'@
[IO.File]::WriteAllText($helper, $helperSource, $utf8)

function Write-Matrix([string]$Path, [string]$CommandPath, [string]$Policy, [string]$Mode) {
  $case = [ordered]@{ id = "$Mode-case"; stableKey = "$Mode-case"; args = @('-NoProfile','-File',$helper,'-mode',$Mode); expectedKind = 'success' }
  Write-JSON $Path ([ordered]@{ schema = 'wowdata.command-benchmark-matrix.v1'; targets = @(); commands = @([ordered]@{ path = $CommandPath; risk = 'read-only'; execution = 'execute'; outputComparison = 'strict-cross-protocol'; baselineComparison = $Policy; requiredProtocols = @('independent-cold','shared-build-cold','warm','repeat-warm'); cases = @($case) }) })
}

$runner = Join-Path $PSScriptRoot 'run-command-matrix.ps1'
$powershell = (Get-Command powershell).Source
$golden = Join-Path $root 'golden.json'
Write-JSON $golden ([ordered]@{ fixtures = @() })

$unsupportedMatrix = Join-Path $root 'unsupported-matrix.json'
Write-Matrix $unsupportedMatrix 'encounter export' 'unsupported-or-strict' 'unsupported'
& powershell -NoProfile -File $runner -Binary $powershell -BaselineBinary $powershell -Matrix $unsupportedMatrix -GoldenManifest $golden -OutputRoot (Join-Path $OutputRoot 'unsupported') -Protocols independent-cold *> $null
if ($LASTEXITCODE -ne 0) { throw "unsupported baseline case exited $LASTEXITCODE" }
$unsupported = Get-Content -Raw (Join-Path $root 'unsupported/report.json') | ConvertFrom-Json
if (-not $unsupported.samples[0].baseline.unsupported -or $unsupported.samples[0].baseline.comparisonStatus -ne 'unsupported') { throw 'unsupported baseline was not recorded explicitly' }

$strictMatrix = Join-Path $root 'strict-matrix.json'
Write-Matrix $strictMatrix 'db2 rows' 'strict' 'mismatch'
& powershell -NoProfile -File $runner -Binary $powershell -BaselineBinary $powershell -Matrix $strictMatrix -GoldenManifest $golden -OutputRoot (Join-Path $OutputRoot 'strict') -Protocols independent-cold *> $null
if ($LASTEXITCODE -eq 0) { throw 'strict baseline mismatch unexpectedly passed' }
$strict = Get-Content -Raw (Join-Path $root 'strict/report.json') | ConvertFrom-Json
if ($strict.samples[0].baseline.comparisonStatus -ne 'mismatch' -or $strict.summary.baselineMismatches -ne 1) { throw 'strict mismatch was not reported' }

$savedErrorActionPreference = $ErrorActionPreference
$ErrorActionPreference = 'Continue'
& powershell -NoProfile -File $runner -Binary $powershell -BaselineBinary $powershell -Matrix $unsupportedMatrix -GoldenManifest $golden -OutputRoot (Join-Path $OutputRoot 'formal-rejected') -Formal -ExecuteRecordOnly -Repetitions 9 *> $null
$ErrorActionPreference = $savedErrorActionPreference
if ($LASTEXITCODE -eq 0) { throw 'formal protocol accepted fewer than 10 repetitions' }

& powershell -NoProfile -File $runner -Binary $powershell -BaselineBinary $powershell -Matrix $unsupportedMatrix -GoldenManifest $golden -OutputRoot (Join-Path $OutputRoot 'formal-pass') -Formal -ExecuteRecordOnly -Repetitions 10 *> $null
if ($LASTEXITCODE -ne 0) { throw "complete formal protocol exited $LASTEXITCODE" }
$formal = Get-Content -Raw (Join-Path $root 'formal-pass/report.json') | ConvertFrom-Json
if (-not $formal.formal -or -not $formal.formalProtocolComplete -or $formal.repetitions -ne 10 -or $formal.samples.Count -ne 40) { throw 'formal report is incomplete' }

[ordered]@{ schema = 'wowdata.command-matrix-policy-smoke.v1'; status = 'pass'; unsupportedReport = (Join-Path $root 'unsupported/report.json').Replace('\','/'); strictReport = (Join-Path $root 'strict/report.json').Replace('\','/'); formalReport = (Join-Path $root 'formal-pass/report.json').Replace('\','/') } | ConvertTo-Json
