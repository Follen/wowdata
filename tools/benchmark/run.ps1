param(
  [string]$OutputRoot = "analyze/benchmark",
  [switch]$SkipGolden,
  [string]$FuzzTime = "30s"
)

$ErrorActionPreference = "Continue"
$repo = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
Set-Location $repo
New-Item -ItemType Directory -Force -Path $OutputRoot | Out-Null

function Invoke-Recorded([string]$Name, [string]$File, [string[]]$Arguments) {
  $stdout = Join-Path $OutputRoot "$Name.stdout.log"
  $stderr = Join-Path $OutputRoot "$Name.stderr.log"
  $started = [DateTime]::UtcNow
  & $File @Arguments 1> $stdout 2> $stderr
  $exit = $LASTEXITCODE
  [ordered]@{
    name = $Name
    command = (($File + " " + ($Arguments -join " ")).Trim())
    startedAt = $started.ToString("o")
    finishedAt = [DateTime]::UtcNow.ToString("o")
    exitCode = $exit
    stdout = $stdout.Replace('\','/')
    stderr = $stderr.Replace('\','/')
    stdoutSha256 = (Get-FileHash $stdout -Algorithm SHA256).Hash.ToLowerInvariant()
    stderrSha256 = (Get-FileHash $stderr -Algorithm SHA256).Hash.ToLowerInvariant()
  }
}

$checks = @()
$checks += Invoke-Recorded "go-test-all" "go" @("test", "./...", "-count=1")
$checks += Invoke-Recorded "go-race-all" "go" @("test", "-race", "./...", "-count=1")
$checks += Invoke-Recorded "npm-test" "npm.cmd" @("test")
$checks += Invoke-Recorded "npm-pack-check" "npm.cmd" @("run", "pack:check")
$checks += Invoke-Recorded "fuzz-wdc" "go" @("test", "./internal/db2", "-run", "^$", "-fuzz", "^FuzzWDCReaderCorruptInput$", "-fuzztime", $FuzzTime)
$checks += Invoke-Recorded "fuzz-dbc" "go" @("test", "./internal/db2", "-run", "^$", "-fuzz", "^FuzzDBCReaderCorruptInput$", "-fuzztime", $FuzzTime)
$checks += Invoke-Recorded "go-bench" "go" @("test", "./...", "-run", "^$", "-bench", ".", "-benchmem")
if (-not $SkipGolden) {
  $checks += Invoke-Recorded "golden" "powershell" @("-NoProfile", "-File", "tools/golden/run-all.ps1")
}

$report = [ordered]@{
  generatedAt = [DateTime]::UtcNow.ToString("o")
  repository = $repo.Replace('\','/')
  environment = [ordered]@{
    os = [Environment]::OSVersion.VersionString
    processorCount = [Environment]::ProcessorCount
    go = (& go version)
  }
  checks = $checks
  status = if (@($checks | Where-Object { $_.exitCode -ne 0 }).Count -eq 0) { "pass" } else { "fail" }
}
$report | ConvertTo-Json -Depth 20 | Set-Content -Encoding utf8 (Join-Path $OutputRoot "report.json")
if ($report.status -ne "pass") { exit 1 }
exit 0
