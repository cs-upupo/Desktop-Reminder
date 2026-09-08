$ErrorActionPreference = 'Stop'
& (Join-Path $PSScriptRoot 'setup.ps1')
$projectDirectory = Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $projectDirectory
try {
    & wails build -platform windows/amd64 -webview2 error
    if ($LASTEXITCODE -ne 0) { throw 'Wails build failed.' }
    $project = Get-Content -LiteralPath 'wails.json' -Raw -Encoding UTF8 | ConvertFrom-Json
    $binaryName = $project.outputfilename + '.exe'
    $distributionDirectory = Join-Path $projectDirectory ('release\windows-amd64-v' + $project.info.productVersion)
    $null = New-Item -ItemType Directory -Path $distributionDirectory -Force
    Copy-Item -LiteralPath (Join-Path 'build\bin' $binaryName) -Destination $distributionDirectory -Force
    Copy-Item -LiteralPath 'QUICK_START.txt' -Destination (Join-Path $distributionDirectory 'README.txt') -Force
    & (Join-Path $PSScriptRoot 'notices.ps1') -Destination (Join-Path $distributionDirectory 'THIRD_PARTY_NOTICES.txt')
    $archive = Join-Path $projectDirectory ('release\DesktopReminder-windows-amd64-v' + $project.info.productVersion + '.zip')
    $distributionFiles = @(Get-ChildItem -LiteralPath $distributionDirectory -File | Select-Object -ExpandProperty FullName)
    Compress-Archive -LiteralPath $distributionFiles -DestinationPath $archive -Force
    Write-Host "Distribution package: $archive"
} finally { Pop-Location }
