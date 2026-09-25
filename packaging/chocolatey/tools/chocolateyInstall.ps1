$ErrorActionPreference = 'Stop'

$toolsDir = Split-Path -Parent $MyInvocation.MyCommand.Definition

# URLs and checksums injected by the release workflow - DO NOT EDIT MANUALLY
$urlAmd64 = 'URL_AMD64_PLACEHOLDER'
$urlArm64 = 'URL_ARM64_PLACEHOLDER'
$checksumAmd64 = 'CHECKSUM_AMD64_PLACEHOLDER'
$checksumArm64 = 'CHECKSUM_ARM64_PLACEHOLDER'

if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') {
    $arch = 'arm64'
    $url = $urlArm64
    $checksum = $checksumArm64
} elseif ([Environment]::Is64BitOperatingSystem) {
    $arch = 'amd64'
    $url = $urlAmd64
    $checksum = $checksumAmd64
} else {
    throw "32-bit Windows is not supported. sptfy requires 64-bit Windows."
}

Write-Host "Installing sptfy for Windows ${arch}..."
Install-ChocolateyZipPackage -PackageName $env:ChocolateyPackageName `
    -Url $url `
    -UnzipLocation $toolsDir `
    -Checksum $checksum `
    -ChecksumType 'sha256'

Write-Host "sptfy installed successfully. Run 'sptfy --help' to get started."
