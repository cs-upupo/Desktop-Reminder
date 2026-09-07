$ErrorActionPreference = 'Stop'
& (Join-Path $PSScriptRoot 'setup.ps1')
$projectDirectory = Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $projectDirectory
try {
    & wails dev
    if ($LASTEXITCODE -ne 0) { throw 'Development run failed.' }
} finally { Pop-Location }
