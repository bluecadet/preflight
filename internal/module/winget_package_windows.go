//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/winutil"
)

type WingetPackageModule struct{}

func (m *WingetPackageModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	normalized, err := winutil.NormalizeWingetParams(params)
	if err != nil {
		return false, err
	}
	return runWindowsPowerShellBool(ctx, normalized, `
$pkgs = @($params.packages)
Get-Command winget.exe -ErrorAction Stop | Out-Null
$tempPath = Join-Path $env:TEMP ("preflight-winget-" + [guid]::NewGuid().ToString() + ".json")
try {
  $process = Start-Process -FilePath 'winget.exe' -ArgumentList @('export', '--output', $tempPath, '--include-versions', '--accept-source-agreements', '--disable-interactivity') -Wait -PassThru -NoNewWindow
  if ($process.ExitCode -ne 0) {
    throw "winget export failed with exit code $($process.ExitCode)"
  }
  $doc = Get-Content -LiteralPath $tempPath -Raw | ConvertFrom-Json
} finally {
  Remove-Item -LiteralPath $tempPath -Force -ErrorAction SilentlyContinue
}

$installedMap = @{}
foreach ($src in @($doc.Sources)) {
  foreach ($pkg in @($src.Packages)) {
    $installedMap[$pkg.PackageIdentifier] = $pkg
  }
}

foreach ($spec in $pkgs) {
  $id = [string]$spec.id
  $ensure = if ($spec.ensure) { [string]$spec.ensure } else { 'present' }
  $version = if ($spec.version) { [string]$spec.version } else { '' }
  $match = $installedMap[$id]
  $isInstalled = $null -ne $match
  if ($ensure -eq 'absent') {
    if ($isInstalled) { Write-Output 'true'; exit 0 }
  } else {
    if (-not $isInstalled) { Write-Output 'true'; exit 0 }
    if ($version -and [string]$match.Version -ne $version) { Write-Output 'true'; exit 0 }
  }
}
Write-Output 'false'
`)
}

func (m *WingetPackageModule) Apply(ctx context.Context, params map[string]any) error {
	normalized, err := winutil.NormalizeWingetParams(params)
	if err != nil {
		return err
	}
	_, err = runWindowsPowerShellWithParams(ctx, normalized, `
$pkgs = @($params.packages)
Get-Command winget.exe -ErrorAction Stop | Out-Null
$tempPath = Join-Path $env:TEMP ("preflight-winget-" + [guid]::NewGuid().ToString() + ".json")
try {
  $process = Start-Process -FilePath 'winget.exe' -ArgumentList @('export', '--output', $tempPath, '--include-versions', '--accept-source-agreements', '--disable-interactivity') -Wait -PassThru -NoNewWindow
  if ($process.ExitCode -ne 0) {
    throw "winget export failed with exit code $($process.ExitCode)"
  }
  $doc = Get-Content -LiteralPath $tempPath -Raw | ConvertFrom-Json
} finally {
  Remove-Item -LiteralPath $tempPath -Force -ErrorAction SilentlyContinue
}

$installedMap = @{}
foreach ($src in @($doc.Sources)) {
  foreach ($pkg in @($src.Packages)) {
    $installedMap[$pkg.PackageIdentifier] = $pkg
  }
}

foreach ($spec in $pkgs) {
  $id = [string]$spec.id
  $ensure = if ($spec.ensure) { [string]$spec.ensure } else { 'present' }
  $version = if ($spec.version) { [string]$spec.version } else { '' }
  $source = if ($spec.source) { [string]$spec.source } else { '' }
  $scope = if ($spec.scope) { [string]$spec.scope } else { 'machine' }
  $match = $installedMap[$id]
  $isInstalled = $null -ne $match
  if ($ensure -eq 'absent' -and -not $isInstalled) { continue }
  if ($ensure -ne 'absent' -and $isInstalled -and (-not $version -or [string]$match.Version -eq $version)) { continue }
  $args = @()
  if ($ensure -eq 'absent') {
    $args = @('uninstall', '--id', $id, '--exact', '--disable-interactivity', '--accept-source-agreements')
  } else {
    $args = @('install', '--id', $id, '--exact', '--silent', '--disable-interactivity', '--accept-package-agreements', '--accept-source-agreements', '--scope', $scope)
  }
  if ($version) { $args += @('--version', $version) }
  if ($source) { $args += @('--source', $source) }
  $process = Start-Process -FilePath 'winget.exe' -ArgumentList $args -Wait -PassThru -NoNewWindow
  if ($process.ExitCode -ne 0) {
    throw "winget command failed for '$id' with exit code $($process.ExitCode)"
  }
}
`)
	return err
}

