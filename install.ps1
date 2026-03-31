# Preflight installer for Windows
# Usage: irm https://raw.githubusercontent.com/bluecadet/preflight/main/install.ps1 | iex

$ErrorActionPreference = "Stop"

$repo = "bluecadet/preflight"
$installDir = "$env:LOCALAPPDATA\preflight"

# Detect architecture
$arch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq "Arm64") { "arm64" } else { "amd64" }

# Fetch latest release tag
$release = Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest"
$version = $release.tag_name

# Find the matching asset
$assetName = "preflight-$version-windows-$arch.zip"
$asset = $release.assets | Where-Object { $_.name -eq $assetName }
if (-not $asset) {
    Write-Error "Could not find release asset: $assetName"
    exit 1
}

# Download and extract
$tmp = Join-Path $env:TEMP "preflight-install"
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
$zipPath = Join-Path $tmp $assetName

Write-Host "Downloading preflight $version ($arch)..."
Invoke-WebRequest -Uri $asset.browser_download_url -OutFile $zipPath

Expand-Archive -Path $zipPath -DestinationPath $tmp -Force
New-Item -ItemType Directory -Force -Path $installDir | Out-Null
Copy-Item "$tmp\preflight.exe" "$installDir\preflight.exe" -Force

# Add to user PATH if not already present
$userPath = [Environment]::GetEnvironmentVariable("PATH", "User")
if ($userPath -notlike "*$installDir*") {
    [Environment]::SetEnvironmentVariable("PATH", "$userPath;$installDir", "User")
    Write-Host "Added $installDir to your PATH."
    Write-Host "Restart your terminal for the PATH change to take effect."
}

# Clean up
Remove-Item -Recurse -Force $tmp

Write-Host "preflight $version installed to $installDir\preflight.exe"
