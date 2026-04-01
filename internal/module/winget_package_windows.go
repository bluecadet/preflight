//go:build windows

package module

import "context"

type WingetPackageModule struct{}

func (m *WingetPackageModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	if _, err := paramStringRequired(params, "id"); err != nil {
		return false, err
	}
	if _, err := paramString(params, "ensure", "present"); err != nil {
		return false, err
	}

	return runWindowsPowerShellBool(ctx, params, `
$id = [string]$params.id
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
$source = if ($params.source) { [string]$params.source } else { '' }
$version = if ($params.version) { [string]$params.version } else { '' }
Get-Command winget.exe -ErrorAction Stop | Out-Null
$tempPath = Join-Path $env:TEMP ("preflight-winget-" + [guid]::NewGuid().ToString() + ".json")
try {
  $args = @('export', '--output', $tempPath, '--include-versions', '--accept-source-agreements', '--disable-interactivity')
  if ($source) {
    $args += @('--source', $source)
  }
  $process = Start-Process -FilePath 'winget.exe' -ArgumentList $args -Wait -PassThru -NoNewWindow
  if ($process.ExitCode -ne 0) {
    throw "winget export failed with exit code $($process.ExitCode)"
  }
  $doc = Get-Content -LiteralPath $tempPath -Raw | ConvertFrom-Json
} finally {
  Remove-Item -LiteralPath $tempPath -Force -ErrorAction SilentlyContinue
}

$packages = @()
foreach ($src in @($doc.Sources)) {
  foreach ($pkg in @($src.Packages)) {
    $packages += $pkg
  }
}
$match = $packages | Where-Object { $_.PackageIdentifier -eq $id } | Select-Object -First 1
$installed = $null -ne $match
if ($ensure -eq 'absent') {
  Write-Output $installed
  exit 0
}
if (-not $installed) {
  Write-Output 'true'
  exit 0
}
if ($version -and [string]$match.Version -ne $version) {
  Write-Output 'true'
  exit 0
}
Write-Output 'false'
`)
}

func (m *WingetPackageModule) Apply(ctx context.Context, params map[string]any) error {
	if _, err := paramStringRequired(params, "id"); err != nil {
		return err
	}
	if _, err := paramString(params, "ensure", "present"); err != nil {
		return err
	}

	_, err := runWindowsPowerShellWithParams(ctx, params, `
$id = [string]$params.id
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
$source = if ($params.source) { [string]$params.source } else { '' }
$version = if ($params.version) { [string]$params.version } else { '' }
$scope = if ($params.scope) { [string]$params.scope } else { 'machine' }
Get-Command winget.exe -ErrorAction Stop | Out-Null

$args = @()
if ($ensure -eq 'absent') {
  $args = @('uninstall', '--id', $id, '--exact', '--disable-interactivity', '--accept-source-agreements')
} else {
  $args = @('install', '--id', $id, '--exact', '--silent', '--disable-interactivity', '--accept-package-agreements', '--accept-source-agreements', '--scope', $scope)
}
if ($version) {
  $args += @('--version', $version)
}
if ($source) {
  $args += @('--source', $source)
}

$process = Start-Process -FilePath 'winget.exe' -ArgumentList $args -Wait -PassThru -NoNewWindow
if ($process.ExitCode -ne 0) {
  throw "winget command failed with exit code $($process.ExitCode)"
}
`)
	return err
}
