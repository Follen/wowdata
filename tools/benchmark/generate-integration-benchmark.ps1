param(
  [string]$Output = "analyze/benchmark/integration/matrix.json",
  [string]$Builds = "tools/benchmark/integration/builds.json",
  [string]$Inputs = "tools/benchmark/target-inputs.json"
)

$ErrorActionPreference = "Stop"
$repo = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
Set-Location $repo
$absolute = [IO.Path]::GetFullPath((Join-Path $repo $Output))
$parent = Split-Path -Parent $absolute
New-Item -ItemType Directory -Force -Path $parent | Out-Null
$utf8NoBom = [Text.UTF8Encoding]::new($false)
$json = @(go run ./tools/benchmark/integration -builds $Builds -inputs $Inputs)
if ($LASTEXITCODE -ne 0) { throw "integration benchmark generation failed with exit $LASTEXITCODE" }
[IO.File]::WriteAllText($absolute, (($json -join [Environment]::NewLine) + [Environment]::NewLine), $utf8NoBom)
$matrix = Get-Content -LiteralPath $absolute -Raw | ConvertFrom-Json
[ordered]@{
  schema = "wowdata.integration-benchmark-generation.v1"
  matrix = $absolute.Replace('\','/')
  leafCommands = [int]$matrix.summary.leafCommands
  cells = [int]$matrix.summary.cells
  executable = [int]$matrix.summary.executable
  coverageGaps = [int]$matrix.summary.coverageGaps
  goldenLayer = [string]$matrix.goldenLayer
} | ConvertTo-Json -Depth 5
