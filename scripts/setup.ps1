$ErrorActionPreference = 'Stop'
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'
$requiredWails = 'v2.15.0'
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw 'Go 1.25 or newer is required. Install the Windows Go toolchain and run this script again.'
}
$goVersion = & go env GOVERSION
if ($LASTEXITCODE -ne 0 -or $goVersion -notmatch '^go(\d+)\.(\d+)' -or [int]$Matches[1] -lt 1 -or ([int]$Matches[1] -eq 1 -and [int]$Matches[2] -lt 25)) {
    throw 'This pinned Wails version requires Go 1.25 or newer.'
}
$goWorkspace = ((& go env GOPATH) -split ';')[0]
$toolDirectory = Join-Path $goWorkspace 'bin'
$env:Path = $toolDirectory + ';' + $env:Path
$wailsFile = Join-Path $toolDirectory 'wails.exe'
$needsInstall = -not (Test-Path -LiteralPath $wailsFile)
if (-not $needsInstall) {
    $installedVersion = (& $wailsFile version | Out-String)
    $needsInstall = $LASTEXITCODE -ne 0 -or $installedVersion -notmatch [regex]::Escape($requiredWails)
}
if ($needsInstall) {
    Write-Host "Installing Wails $requiredWails from the configured public Go module source..."
    & go install "github.com/wailsapp/wails/v2/cmd/wails@$requiredWails"
    if ($LASTEXITCODE -ne 0) { throw 'Wails installation failed. Check access to your Go module source.' }
    $hostArchitecture = & go env GOHOSTARCH
    if ($hostArchitecture -ne 'amd64') {
        $crossCompiled = Join-Path $toolDirectory 'windows_amd64\wails.exe'
        if (-not (Test-Path -LiteralPath $crossCompiled)) { throw 'Compiled Wails executable was not found.' }
        Copy-Item -LiteralPath $crossCompiled -Destination $wailsFile -Force
    }
}
Write-Host "Ready: $goVersion, Wails $requiredWails, Windows amd64. No npm packages required."
