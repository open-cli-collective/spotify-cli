$ErrorActionPreference = 'Stop'

$version = $env:ChocolateyPackageVersion
$toolsDir = Split-Path -Parent $MyInvocation.MyCommand.Definition

# Checksums injected by the release workflow - DO NOT EDIT MANUALLY
$checksumAmd64 = 'CHECKSUM_AMD64_PLACEHOLDER'
$checksumArm64 = 'CHECKSUM_ARM64_PLACEHOLDER'

if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') {
    $arch = 'arm64'
    $checksum = $checksumArm64
} elseif ([Environment]::Is64BitOperatingSystem) {
    $arch = 'amd64'
    $checksum = $checksumAmd64
} else {
    throw "32-bit Windows is not supported. sptfy requires 64-bit Windows."
}

$baseUrl = "https://github.com/open-cli-collective/spotify-cli/releases/download/v${version}"
$zipFile = "sptfy_v${version}_windows_${arch}.zip"
$url = "${baseUrl}/${zipFile}"

Write-Host "Installing sptfy ${version} for Windows ${arch}..."
Install-ChocolateyZipPackage -PackageName $env:ChocolateyPackageName `
    -Url $url `
    -UnzipLocation $toolsDir `
    -Checksum $checksum `
    -ChecksumType 'sha256'

Write-Host "sptfy installed successfully. Run 'sptfy --help' to get started."

