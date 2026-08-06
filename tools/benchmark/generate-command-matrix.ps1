param(
  [string]$OutputRoot = "analyze/benchmark/command-matrix",
  [string]$Fixtures = "fixtures/golden/manifest.json",
  [string]$Inputs = "tools/benchmark/target-inputs.json"
)

$ErrorActionPreference = "Stop"
$repo = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
Set-Location $repo
New-Item -ItemType Directory -Force -Path $OutputRoot | Out-Null

$commands = Join-Path $OutputRoot "commands.json"
$coverage = Join-Path $OutputRoot "coverage.json"
$matrix = Join-Path $OutputRoot "matrix.json"
$utf8NoBom = New-Object System.Text.UTF8Encoding($false)

function Write-Utf8NoBom([string]$Path, [string[]]$Lines) {
  $absolute = [IO.Path]::GetFullPath($Path)
  [IO.File]::WriteAllText($absolute, (($Lines -join [Environment]::NewLine) + [Environment]::NewLine), $utf8NoBom)
}

$commandJSON = @(go run ./tools/benchmark/manifest)
if ($LASTEXITCODE -ne 0) { throw "command manifest generation failed with exit $LASTEXITCODE" }
Write-Utf8NoBom $commands $commandJSON

# Coverage can intentionally return 1 while the matrix records generated isolated cases.
$previousErrorAction = $ErrorActionPreference
$ErrorActionPreference = "Continue"
$coverageJSON = @(go run ./tools/benchmark/coverage --commands $commands --fixtures $Fixtures 2>$null)
$coverageExit = $LASTEXITCODE
$ErrorActionPreference = $previousErrorAction
if ($coverageExit -ne 0 -and $coverageExit -ne 1) { throw "fixture coverage generation failed with exit $coverageExit" }
Write-Utf8NoBom $coverage $coverageJSON

$matrixJSON = @(go run ./tools/benchmark/matrix --fixtures $Fixtures --inputs $Inputs)
if ($LASTEXITCODE -ne 0) { throw "benchmark matrix generation failed with exit $LASTEXITCODE" }
Write-Utf8NoBom $matrix $matrixJSON

$result = [ordered]@{
  schema = "wowdata.command-matrix-generation.v1"
  commands = (Resolve-Path $commands).Path.Replace('\','/')
  coverage = (Resolve-Path $coverage).Path.Replace('\','/')
  matrix = (Resolve-Path $matrix).Path.Replace('\','/')
  fixtureCoverageComplete = ($coverageExit -eq 0)
}
$result | ConvertTo-Json -Depth 5
