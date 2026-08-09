param(
    [Parameter(Mandatory=$true)][string]$Version,
    [Parameter(Mandatory=$true)][string]$PfxPath,
    [Parameter(Mandatory=$true)][string]$PfxPassword,
    [string]$TimestampUrl = 'http://timestamp.digicert.com',
    [switch]$AllowUntrusted,
    [string]$PublicCertificatePath = ''
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$packageRoot = Join-Path $root "build\release\TRMNL-for-reMarkable-$Version"
$installer = Join-Path $packageRoot 'TRMNL Installer.exe'

if (-not (Test-Path -LiteralPath $PfxPath)) { throw "Signing certificate not found: $PfxPath" }
if (-not (Test-Path -LiteralPath $installer)) { throw "Installer not found: $installer" }

$signTool = & (Join-Path $PSScriptRoot 'find-signtool.ps1')

& $signTool sign /fd SHA256 /td SHA256 /tr $TimestampUrl /f $PfxPath /p $PfxPassword $installer
if ($LASTEXITCODE -ne 0) { throw 'Authenticode signing failed.' }
$signature = Get-AuthenticodeSignature -LiteralPath $installer
if (-not $signature.SignerCertificate) { throw 'The signed installer has no Authenticode signer certificate.' }

if ($AllowUntrusted) {
    $expected = New-Object Security.Cryptography.X509Certificates.X509Certificate2($PfxPath, $PfxPassword)
    $untrustedRoot = $signature.Status -eq 'UnknownError' -and $signature.StatusMessage -match 'root certificate.*not trusted'
    if ($signature.SignatureType -ne 'Authenticode' -or
        $signature.SignerCertificate.Thumbprint -ne $expected.Thumbprint -or
        -not $signature.TimeStamperCertificate -or
        -not $untrustedRoot) {
        throw "Self-signed Authenticode verification failed: $($signature.Status) $($signature.StatusMessage)"
    }
}

if ($PublicCertificatePath) {
    if (-not (Test-Path -LiteralPath $PublicCertificatePath)) { throw "Public certificate not found: $PublicCertificatePath" }
    Copy-Item -LiteralPath $PublicCertificatePath -Destination (Join-Path $packageRoot 'SELF-SIGNED-CERTIFICATE.cer') -Force
    Copy-Item -LiteralPath (Join-Path $root 'docs\code-signing.md') -Destination (Join-Path $packageRoot 'SELF-SIGNED-SIGNATURE.md') -Force
    $certificateHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $PublicCertificatePath).Hash.ToLowerInvariant()
    $certificateDetails = @(
        "Subject: $($signature.SignerCertificate.Subject)"
        "Thumbprint (SHA-1 identifier): $($signature.SignerCertificate.Thumbprint)"
        "Public certificate SHA-256: $certificateHash"
        "Valid from: $($signature.SignerCertificate.NotBefore.ToUniversalTime().ToString('o'))"
        "Valid until: $($signature.SignerCertificate.NotAfter.ToUniversalTime().ToString('o'))"
    ) -join [Environment]::NewLine
    Set-Content -Encoding ascii -LiteralPath (Join-Path $packageRoot 'SELF-SIGNED-CERTIFICATE.txt') -Value $certificateDetails
}

if ($AllowUntrusted) {
    & (Join-Path $PSScriptRoot 'finish-signed-release.ps1') -Version $Version
} else {
    & (Join-Path $PSScriptRoot 'finish-signed-release.ps1') -Version $Version -RequireTrustedChain
}
