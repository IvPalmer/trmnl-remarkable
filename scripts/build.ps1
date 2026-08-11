param([string]$Version = '')
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
if (-not $Version) {
    if ($env:TRMNL_VERSION) { $Version = $env:TRMNL_VERSION }
    else { $Version = (Get-Content -Raw -LiteralPath (Join-Path $root 'VERSION')).Trim() }
}
if ($Version -notmatch '^\d+\.\d+\.\d+([-.][0-9A-Za-z.-]+)?$') { throw "Invalid version: $Version" }

$goCommand = Get-Command go -ErrorAction SilentlyContinue
$go = if ($goCommand) { $goCommand.Source } else { Join-Path $root '_tools\go\bin\go.exe' }
$gofmtCommand = Get-Command gofmt -ErrorAction SilentlyContinue
$gofmt = if ($gofmtCommand) { $gofmtCommand.Source } else { Join-Path $root '_tools\go\bin\gofmt.exe' }
if (-not (Test-Path -LiteralPath $go)) { throw 'Go 1.25 or newer is required: https://go.dev/dl/' }
if (-not (Test-Path -LiteralPath $gofmt)) { throw 'gofmt was not found beside the Go toolchain' }
$rcc = (Get-Command rcc -ErrorAction SilentlyContinue).Source
if (-not $rcc) { throw 'Qt rcc is required. Install Qt 6 and add its bin directory to PATH.' }
$qmllint = (Get-Command qmllint -ErrorAction SilentlyContinue).Source
if (-not $qmllint) { throw 'Qt qmllint is required. Install Qt 6 declarative tools and add them to PATH.' }

$unformatted = & $gofmt -l (Join-Path $root 'backend') (Join-Path $root 'installer')
if ($unformatted) { throw "Run gofmt on:`n$($unformatted -join "`n")" }

Push-Location $root
try {
    & $go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'Tests failed' }
    & $go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'go vet failed' }
    & $qmllint -W 0 -I (Join-Path $root 'tests\qml-stubs') (Join-Path $root 'app\ui\TRMNL.qml')
    if ($LASTEXITCODE -ne 0) { throw 'QML lint failed' }

    $dist = Join-Path $root 'dist'
    # The Paper Pro is 64-bit ARM; the reMarkable 1 and 2 are 32-bit. Each gets
    # its own AppLoad bundle, and the installer uploads whichever matches the
    # tablet it finds. The unsuffixed directory stays the aarch64 bundle so the
    # device scripts and older tooling keep working unchanged.
    $bundles = @(
        @{ Payload = 'aarch64'; GoArch = 'arm64'; GoArm = ''; Directory = 'trmnl-remarkable-app' },
        @{ Payload = 'arm32'; GoArch = 'arm'; GoArm = '7'; Directory = 'trmnl-remarkable-app-arm32' }
    )

    $oldGOOS=$env:GOOS; $oldGOARCH=$env:GOARCH; $oldGOARM=$env:GOARM; $oldCGO=$env:CGO_ENABLED
    try {
        $env:GOOS = 'linux'; $env:CGO_ENABLED = '0'
        foreach ($bundle in $bundles) {
            $appOut = Join-Path $dist $bundle.Directory
            if (Test-Path -LiteralPath $appOut) { Remove-Item -Recurse -LiteralPath $appOut }
            New-Item -ItemType Directory -Path (Join-Path $appOut 'backend'),(Join-Path $appOut 'scripts') -Force | Out-Null
            $env:GOARCH = $bundle.GoArch; $env:GOARM = $bundle.GoArm
            & $go build -buildvcs=false -trimpath -ldflags "-s -w -X main.version=$Version" -o (Join-Path $appOut 'backend\entry') ./backend/cmd/trmnl-remarkable
            if ($LASTEXITCODE -ne 0) { throw "Backend build failed for $($bundle.Payload)" }
        }
        $env:GOARCH=$oldGOARCH; $env:GOARM=$oldGOARM; $env:GOOS=$oldGOOS
        & $go build -buildvcs=false -trimpath -ldflags '-s -w' -o (Join-Path $dist 'trmnl-mock') ./backend/cmd/trmnl-mock
        if ($LASTEXITCODE -ne 0) { throw 'Mock build failed' }
        & $go build -buildvcs=false -trimpath -ldflags '-s -w' -o (Join-Path $dist 'trmnl-input') ./backend/cmd/trmnl-input
        if ($LASTEXITCODE -ne 0) { throw 'Input test utility build failed' }
        & $go build -buildvcs=false -trimpath -ldflags '-s -w' -o (Join-Path $dist 'trmnl-screenshot') ./backend/cmd/trmnl-screenshot
        if ($LASTEXITCODE -ne 0) { throw 'Screenshot test utility build failed' }
    } finally {
        $env:GOOS=$oldGOOS; $env:GOARCH=$oldGOARCH; $env:GOARM=$oldGOARM; $env:CGO_ENABLED=$oldCGO
    }

    foreach ($bundle in $bundles) {
        $appOut = Join-Path $dist $bundle.Directory
        Copy-Item -LiteralPath (Join-Path $root 'app\manifest.json'),(Join-Path $root 'app\icon.png') -Destination $appOut -Force
        Copy-Item -LiteralPath (Join-Path $root 'app\scripts\brightness_guard.sh') -Destination (Join-Path $appOut 'scripts') -Force
        Push-Location (Join-Path $root 'app')
        try {
            & $rcc --binary -o (Join-Path $appOut 'resources.rcc') 'application.qrc'
            if ($LASTEXITCODE -ne 0) { throw 'rcc failed' }
        } finally { Pop-Location }

        foreach ($required in @('manifest.json','icon.png','resources.rcc','backend\entry','scripts\brightness_guard.sh')) {
            $requiredPath = Join-Path $appOut $required
            if (-not (Test-Path -LiteralPath $requiredPath) -or (Get-Item -LiteralPath $requiredPath).Length -eq 0) { throw "$($bundle.Payload) bundle is missing $required" }
        }
        Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $appOut 'backend\entry'),(Join-Path $appOut 'resources.rcc') | Format-Table -AutoSize
    }
} finally { Pop-Location }
