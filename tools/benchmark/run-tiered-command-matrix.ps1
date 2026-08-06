param(
  [ValidateSet('online-small','offline-large','formal-seal')][string]$Tier = 'online-small',
  [string]$OutputRoot = '',
  [int]$Repetitions = 0
)
$ErrorActionPreference = 'Stop'
$common = @('-Binary','analyze/benchmark/calibration-r41-current/wowdata-selected.exe','-BaselineBinary','analyze/baseline/wowdata-baseline.exe','-Matrix','analyze/benchmark/command-matrix-r41/matrix.json/matrix.json','-GoldenManifest','fixtures/golden/manifest.json','-ExecuteRecordOnly')
switch ($Tier) {
  'online-small' {
    if (-not $OutputRoot) { $OutputRoot = 'analyze/benchmark/tiered-online-small' }
    if ($Repetitions -le 0) { $Repetitions = 1 }
    $casePattern = '^(casc-products-casc-products-remote-us|classic-era-us-casc-info|retail-cn-remote-decor-get-80|retail-cn-decor-list|retail-cn-remote-encounter-export-voidspire-boss-1|retail-cn-encounter-get-132|retail-cn-file-get-136121|retail-cn-file-export-136121|retail-cn-icon-export-136121|retail-cn-spell-info-100|retail-cn-item-get-25|warmup-warmup-remote-classic-era)$'
    & tools/benchmark/run-command-matrix.ps1 @common -OutputRoot $OutputRoot -RunID 'tiered-online-small' -Revision 'tiered-online-small' -SourceMode remote -CasePattern $casePattern -Protocols @('independent-cold','shared-build-cold','warm','repeat-warm') -Repetitions $Repetitions
  }
  'offline-large' {
    if (-not $OutputRoot) { $OutputRoot = 'analyze/benchmark/tiered-offline-large' }
    if ($Repetitions -le 0) { $Repetitions = 10 }
    & tools/benchmark/run-command-matrix.ps1 @common -OutputRoot $OutputRoot -RunID 'tiered-offline-large' -Revision 'tiered-offline-large' -SourceMode local -Protocols @('independent-cold','shared-build-cold','warm','repeat-warm') -Repetitions $Repetitions
  }
  'formal-seal' {
    if (-not $OutputRoot) { $OutputRoot = 'analyze/benchmark/command-matrix-r41d-formal-10x-final' }
    if ($Repetitions -le 0) { $Repetitions = 10 }
    & tools/benchmark/run-command-matrix.ps1 @common -OutputRoot $OutputRoot -RunID 'formal-seal' -Revision 'formal-seal' -SourceMode all -Protocols @('independent-cold','shared-build-cold','warm','repeat-warm') -Repetitions $Repetitions -Formal
  }
}
