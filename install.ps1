param(
    [string]$Version = $env:PREFLIGHT_VERSION,
    [string]$InstallDir = $env:PREFLIGHT_INSTALL_DIR
)

# Preflight installer for Windows
# Usage: irm https://raw.githubusercontent.com/bluecadet/preflight/main/install.ps1 | iex

$ErrorActionPreference = "Stop"

$repo = "bluecadet/preflight"
$checksumAsset = "preflight_checksums.txt"
if ([string]::IsNullOrWhiteSpace($InstallDir)) {
    $InstallDir = Join-Path $env:LOCALAPPDATA "preflight"
}

if (-not [string]::IsNullOrWhiteSpace($Version) -and -not $Version.StartsWith("v")) {
    $Version = "v$Version"
}

$arch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq "Arm64") { "arm64" } else { "amd64" }
$assetOs = "Windows"
$assetArch = if ($arch -eq "amd64") { "x86_64" } else { "arm64" }
$assetName = "preflight_${assetOs}_${assetArch}.zip"

$releaseUrl = if ([string]::IsNullOrWhiteSpace($Version)) {
    "https://api.github.com/repos/$repo/releases/latest"
} else {
    "https://api.github.com/repos/$repo/releases/tags/$Version"
}

$release = Invoke-RestMethod $releaseUrl
$tag = $release.tag_name
if ([string]::IsNullOrWhiteSpace($tag)) {
    throw "Failed to determine release version."
}

$asset = $release.assets | Where-Object { $_.name -eq $assetName } | Select-Object -First 1
if (-not $asset) {
    throw "Could not find release asset: $assetName"
}

$checksum = $release.assets | Where-Object { $_.name -eq $checksumAsset } | Select-Object -First 1
if (-not $checksum) {
    throw "Could not find checksum asset: $checksumAsset"
}

$tmp = Join-Path $env:TEMP "preflight-install"
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
$zipPath = Join-Path $tmp $assetName
$checksumPath = Join-Path $tmp $checksumAsset

Write-Host "Downloading preflight $tag ($arch)..."
Invoke-WebRequest -Uri $asset.browser_download_url -OutFile $zipPath
Invoke-WebRequest -Uri $checksum.browser_download_url -OutFile $checksumPath

$expectedLine = Select-String -Path $checksumPath -Pattern ([regex]::Escape($assetName) + '$') | Select-Object -First 1
if (-not $expectedLine) {
    throw "Could not find checksum entry for $assetName in $checksumAsset"
}
$expected = ($expectedLine.Line -split '\s+')[0]
$actual = (Get-FileHash $zipPath -Algorithm SHA256).Hash.ToLowerInvariant()
if ($expected.ToLowerInvariant() -ne $actual) {
    throw "Checksum verification failed for $assetName"
}

Expand-Archive -Path $zipPath -DestinationPath $tmp -Force
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
Copy-Item (Join-Path $tmp "preflight.exe") (Join-Path $InstallDir "preflight.exe") -Force

$userPath = [Environment]::GetEnvironmentVariable("PATH", "User")
if ($userPath -notlike "*$InstallDir*") {
    [Environment]::SetEnvironmentVariable("PATH", "$userPath;$InstallDir", "User")
    Write-Host "Added $InstallDir to your PATH."
    Write-Host "Restart your terminal for the PATH change to take effect."
}

& (Join-Path $InstallDir "preflight.exe") --version | Out-Null

Remove-Item -Recurse -Force $tmp

Write-Host "preflight $tag installed to $InstallDir\preflight.exe"
