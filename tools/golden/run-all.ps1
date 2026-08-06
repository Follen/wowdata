param(
  [string]$Manifest = "fixtures/golden/manifest.json",
  [string]$OutputRoot = "analyze/golden",
  [string]$Report = "analyze/golden/report.json",
  [int]$CommandTimeoutSeconds = 180,
	[string]$WowdataHome = "",
	[string]$Binary = "",
	[string]$CompareRoot = "fixtures/golden/go",
  [switch]$SkipCompare
)

$args = @(
  "run", "./tools/golden/runner",
  "-manifest", $Manifest,
  "-output", $OutputRoot,
  "-report", $Report,
	"-timeout", ("{0}s" -f $CommandTimeoutSeconds),
	"-compare-root", $CompareRoot
)
if ($WowdataHome) { $args += @("-home", $WowdataHome) }
if ($Binary) { $args += @("-binary", $Binary) }
if ($SkipCompare) { $args += "-skip-compare" }
& go @args
exit $LASTEXITCODE
