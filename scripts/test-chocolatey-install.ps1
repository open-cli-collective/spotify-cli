$ErrorActionPreference = 'Stop'

$templatePath = Join-Path $PSScriptRoot '..\packaging\chocolatey\tools\chocolateyInstall.ps1'
$tempRoot = $env:RUNNER_TEMP
if (-not $tempRoot) { $tempRoot = [IO.Path]::GetTempPath() }
$renderedPath = Join-Path $tempRoot "spotify-chocolatey-$([guid]::NewGuid()).ps1"

$amd64Url = 'https://example.invalid/sptfy-amd64.zip'
$arm64Url = 'https://example.invalid/sptfy-arm64.zip'
$amd64Checksum = '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef'
$arm64Checksum = 'fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210'
$rendered = Get-Content $templatePath -Raw
$rendered = $rendered.Replace('URL_AMD64_PLACEHOLDER', $amd64Url)
$rendered = $rendered.Replace('URL_ARM64_PLACEHOLDER', $arm64Url)
$rendered = $rendered.Replace('CHECKSUM_AMD64_PLACEHOLDER', $amd64Checksum)
$rendered = $rendered.Replace('CHECKSUM_ARM64_PLACEHOLDER', $arm64Checksum)

if ($rendered -match 'URL_.*_PLACEHOLDER|CHECKSUM_.*_PLACEHOLDER') {
    throw 'Rendered Chocolatey installer still contains a placeholder'
}
Set-Content $renderedPath $rendered

function Install-ChocolateyZipPackage {
    param(
        [string]$PackageName,
        [string]$Url,
        [string]$UnzipLocation,
        [string]$Checksum,
        [string]$ChecksumType
    )
    $global:capturedInstall = [pscustomobject]@{
        Url = $Url
        Checksum = $Checksum
        ChecksumType = $ChecksumType
    }
}

$env:ChocolateyPackageName = 'spotify-cli'
$architectures = @(
    [pscustomobject]@{ Name = 'AMD64'; Environment = 'AMD64'; Url = $amd64Url; Checksum = $amd64Checksum },
    [pscustomobject]@{ Name = 'ARM64'; Environment = 'ARM64'; Url = $arm64Url; Checksum = $arm64Checksum }
)

foreach ($architecture in $architectures) {
    $previousArchitecture = $env:PROCESSOR_ARCHITECTURE
    try {
        $env:PROCESSOR_ARCHITECTURE = $architecture.Environment
        $global:capturedInstall = $null
        . $renderedPath
    } finally {
        $env:PROCESSOR_ARCHITECTURE = $previousArchitecture
    }

    if (-not $global:capturedInstall) { throw "$($architecture.Name) did not call Install-ChocolateyZipPackage" }
    if ($global:capturedInstall.Url -ne $architecture.Url) {
        throw "$($architecture.Name) selected URL '$($global:capturedInstall.Url)', expected '$($architecture.Url)'"
    }
    if ($global:capturedInstall.Checksum -ne $architecture.Checksum) {
        throw "$($architecture.Name) selected checksum '$($global:capturedInstall.Checksum)', expected '$($architecture.Checksum)'"
    }
    if ($global:capturedInstall.ChecksumType -ne 'sha256') {
        throw "$($architecture.Name) selected checksum type '$($global:capturedInstall.ChecksumType)', expected 'sha256'"
    }
}

Write-Host 'Chocolatey AMD64/ARM64 URL and checksum bindings passed.'
