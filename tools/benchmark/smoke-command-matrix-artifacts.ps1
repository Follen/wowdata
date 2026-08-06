param(
  [string]$OutputRoot = "analyze/benchmark/command-matrix-artifact-smoke"
)

$ErrorActionPreference = "Stop"
$repo = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
Set-Location $repo
$root = [IO.Path]::GetFullPath((Join-Path $repo $OutputRoot))
$utf8 = [Text.UTF8Encoding]::new($false)
New-Item -ItemType Directory -Force -Path $root | Out-Null

function Write-JSON([string]$Path, $Value) {
  [IO.File]::WriteAllText($Path, (($Value | ConvertTo-Json -Depth 12) + [Environment]::NewLine), $utf8)
}

$helper = Join-Path $root 'artifact-fixture.ps1'
$helperSource = @'
param([string]$output, [switch]$omit)
$bytes = [Text.Encoding]::UTF8.GetBytes('wowdata-artifact-smoke')
$sha = [Security.Cryptography.SHA256]::Create()
try { $hash = ([BitConverter]::ToString($sha.ComputeHash($bytes))).Replace('-', '').ToLowerInvariant() } finally { $sha.Dispose() }
if (-not $omit) {
  $parent = Split-Path $output
  if ($parent) { New-Item -ItemType Directory -Force -Path $parent | Out-Null }
  [IO.File]::WriteAllBytes($output, $bytes)
}
[ordered]@{ ok = $true; command = 'artifact smoke'; data = [ordered]@{ name = [IO.Path]::GetFileName($output); path = [IO.Path]::GetFullPath($output); size = $bytes.Length; sha256 = $hash }; warnings = @() } | ConvertTo-Json -Depth 5
'@
[IO.File]::WriteAllText($helper, $helperSource, $utf8)

$referenceDir = Join-Path $root 'reference'
$referenceOutput = Join-Path $referenceDir 'artifact.bin'
$referenceStdout = & powershell -NoProfile -File $helper -output $referenceOutput | Out-String
$capturePath = Join-Path $root 'capture.json'
Write-JSON $capturePath ([ordered]@{ name = 'artifact/smoke'; command = 'powershell'; args = @(); exitCode = 0; stdout = $referenceStdout; stderr = '' })
$goldenPath = Join-Path $root 'golden.json'
Write-JSON $goldenPath ([ordered]@{ schema = 'wowdata.golden-manifest.v1'; fixtures = @([ordered]@{ name = 'artifact/smoke'; fixture = $capturePath.Replace('\','/') }) })

function Write-Matrix([string]$Path, [switch]$Omit) {
  $args = @('-NoProfile', '-File', $helper, '--output', $referenceOutput)
  if ($Omit) { $args += '--omit' }
  $case = [ordered]@{ id = 'artifact-smoke'; stableKey = $(if ($Omit) { 'artifact-missing' } else { 'artifact-present' }); fixture = 'artifact/smoke'; args = $args; expectedKind = 'success' }
  Write-JSON $Path ([ordered]@{ schema = 'wowdata.command-benchmark-matrix.v1'; targets = @(); commands = @([ordered]@{ path = 'artifact smoke'; execution = 'execute'; requiredProtocols = @('independent-cold'); cases = @($case) }) })
}

$runner = Join-Path $PSScriptRoot 'run-command-matrix.ps1'
$powershell = (Get-Command powershell).Source
$passMatrix = Join-Path $root 'pass-matrix.json'
Write-Matrix $passMatrix
& powershell -NoProfile -File $runner -Binary $powershell -BaselineBinary $powershell -Matrix $passMatrix -GoldenManifest $goldenPath -OutputRoot (Join-Path $OutputRoot 'pass') -Protocols independent-cold -Repetitions 2 *> $null
if ($LASTEXITCODE -ne 0) { throw "artifact pass runner exited $LASTEXITCODE" }
$passReport = Get-Content -Raw (Join-Path $root 'pass/report.json') | ConvertFrom-Json
foreach ($sample in $passReport.samples) {
  if (-not $sample.goldenArtifactMatch -or -not $sample.baseline.artifactMatch -or -not $sample.crossProtocolArtifactMatch -or $sample.outputFiles.Count -ne 1) {
    throw "artifact pass sample did not verify all manifests"
  }
}

$missingMatrix = Join-Path $root 'missing-matrix.json'
Write-Matrix $missingMatrix -Omit
& powershell -NoProfile -File $runner -Binary $powershell -Matrix $missingMatrix -GoldenManifest $goldenPath -OutputRoot (Join-Path $OutputRoot 'missing') -Protocols independent-cold -Repetitions 1 *> $null
if ($LASTEXITCODE -eq 0) { throw 'missing artifact unexpectedly passed' }
$missingReport = Get-Content -Raw (Join-Path $root 'missing/report.json') | ConvertFrom-Json
if ($missingReport.summary.goldenArtifactViolations -ne 1 -or $missingReport.samples[0].goldenStdoutMatch -ne $true -or $missingReport.samples[0].goldenArtifactMatch -ne $false) {
  throw 'missing artifact was not isolated as a golden artifact failure'
}

[ordered]@{ schema = 'wowdata.command-matrix-artifact-smoke.v1'; status = 'pass'; passReport = (Join-Path $root 'pass/report.json').Replace('\','/'); missingReport = (Join-Path $root 'missing/report.json').Replace('\','/') } | ConvertTo-Json
