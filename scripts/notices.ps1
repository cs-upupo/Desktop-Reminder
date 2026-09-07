param([Parameter(Mandatory=$true)][string]$Destination)
$ErrorActionPreference = 'Stop'
$directories = @(& go list -tags 'desktop,production' -deps -f '{{if .Module}}{{.Module.Dir}}{{end}}' .) | Where-Object { $_ } | Sort-Object -Unique
if ($LASTEXITCODE -ne 0) { throw 'Could not enumerate runtime dependencies.' }
$notice = New-Object System.Text.StringBuilder
$null = $notice.AppendLine('DesktopReminder - third-party software notices')
$runtimeRoot = & go env GOROOT
$null = $notice.AppendLine("`r`nGo standard library")
$null = $notice.AppendLine((Get-Content -LiteralPath (Join-Path $runtimeRoot 'LICENSE') -Raw))
foreach ($directory in $directories) {
    if ($directory -eq (Get-Location).Path) { continue }
    $null = $notice.AppendLine("`r`n============================================================")
    $null = $notice.AppendLine((Split-Path -Leaf $directory))
    $licenseFiles = Get-ChildItem -LiteralPath $directory -File | Where-Object { $_.Name -match '^(LICENSE|COPYING|NOTICE)(\.|$|-)' }
    foreach ($license in $licenseFiles) {
        $null = $notice.AppendLine("`r`n" + $license.Name)
        $null = $notice.AppendLine((Get-Content -LiteralPath $license.FullName -Raw -Encoding UTF8))
    }
}
[IO.File]::WriteAllText($Destination, $notice.ToString(), (New-Object Text.UTF8Encoding($true)))
