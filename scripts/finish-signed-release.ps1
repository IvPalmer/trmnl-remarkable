param(
    [Parameter(Mandatory=$true)][string]$Version,
    [switch]$RequireTrustedChain
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$packageRoot = Join-Path $root "build\release\TRMNL-for-reMarkable-$Version"
$installer = Join-Path $packageRoot 'TRMNL Installer.exe'
$releaseDir = Join-Path $root 'release'
$zip = Join-Path $releaseDir "TRMNL-for-reMarkable-$Version-Windows-x64.zip"

if (-not (Test-Path -LiteralPath $installer)) { throw "Installer not found: $installer" }
$signature = Get-AuthenticodeSignature -LiteralPath $installer
if (-not $signature.SignerCertificate) { throw 'The installer has no Authenticode signer certificate.' }

if ($RequireTrustedChain) {
    # Only a publicly trusted chain can be checked with signtool; the self-signed
    # path verifies its own certificate against the PFX it just used instead.
    $signTool = & (Join-Path $PSScriptRoot 'find-signtool.ps1')
    & $signTool verify /pa /all /v $installer
    if ($LASTEXITCODE -ne 0) { throw 'Authenticode signature verification failed.' }
    if (-not $signature.TimeStamperCertificate) { throw 'The signed installer is not timestamped.' }
}

$goCommand = Get-Command go -ErrorAction SilentlyContinue
$go = if ($goCommand) { $goCommand.Source } else { Join-Path $root '_tools\go\bin\go.exe' }
if (Test-Path -LiteralPath $zip) { Remove-Item -LiteralPath $zip }
& $go run (Join-Path $PSScriptRoot 'package-zip.go') $packageRoot $zip
if ($LASTEXITCODE -ne 0) { throw 'Signed release ZIP creation failed.' }
$hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $zip).Hash.ToLowerInvariant()
Set-Content -Encoding ascii -LiteralPath (Join-Path $releaseDir 'SHA256SUMS.txt') -Value "$hash  $(Split-Path -Leaf $zip)"
Write-Host "Signed by: $($signature.SignerCertificate.Subject)"
Write-Host "Signed release ready: $zip"
Write-Host "SHA-256: $hash"
