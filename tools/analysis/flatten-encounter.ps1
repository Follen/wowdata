param(
  [string]$InputPath = "fixtures/golden/inputs/encounter-virtual-spire-2738-retail-zhCN.json",
  [string]$Output = "analyze/encounter/virtual-spire-2738-flat.json"
)

$j = Get-Content -LiteralPath $InputPath -Raw | ConvertFrom-Json

function Walk($nodes, $depth = 0) {
  foreach ($n in @($nodes)) {
    [pscustomobject]@{
      depth    = $depth
      order    = $n.orderIndex
      id       = $n.id
      title    = $n.title
      mask     = $n.difficultyMask
      spellID  = $n.spellID
      spellIDs = (@($n.spellIDs) -join ',')
      body     = ($n.bodyText -replace "`r?`n", ' ')
    }
    if ($n.children) { Walk $n.children ($depth + 1) }
  }
}

@(Walk $j.data.sections | Sort-Object order) |
  ConvertTo-Json -Depth 8 |
  Set-Content -LiteralPath $Output -Encoding utf8
