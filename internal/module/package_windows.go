//go:build windows

package module

import "context"

type PackageModule struct{}

func (m *PackageModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	if _, err := paramStringRequired(params, "product_id"); err != nil {
		return false, err
	}
	if _, err := paramString(params, "ensure", "present"); err != nil {
		return false, err
	}

	return runWindowsPowerShellBool(ctx, params, `
$productId = [string]$params.product_id
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
$entries = Get-ItemProperty -Path @(
  'HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*',
  'HKLM:\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*',
  'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*'
) -ErrorAction SilentlyContinue
$installed = $null -ne ($entries | Where-Object {
  $_.PSChildName -eq $productId -or $_.ProductID -eq $productId
} | Select-Object -First 1)
if ($ensure -eq 'absent') {
  Write-Output $installed
  exit 0
}
Write-Output (-not $installed)
`)
}

func (m *PackageModule) Apply(ctx context.Context, params map[string]any) error {
	productID, err := paramStringRequired(params, "product_id")
	if err != nil {
		return err
	}
	ensure, err := paramString(params, "ensure", "present")
	if err != nil {
		return err
	}
	if ensure == "present" {
		if _, err := paramStringRequired(params, "source"); err != nil {
			return err
		}
	}

	_ = productID
	_, err = runWindowsPowerShellWithParams(ctx, params, `
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
$argsList = @()
if ($params.args) {
  foreach ($arg in $params.args) {
    $argsList += [string]$arg
  }
}
if ($ensure -eq 'absent') {
  $process = Start-Process -FilePath 'msiexec.exe' -ArgumentList @('/x', [string]$params.product_id, '/qn', '/norestart') -Wait -PassThru
  if ($process.ExitCode -ne 0) {
    throw "package uninstall failed with exit code $($process.ExitCode)"
  }
  exit 0
}
$source = [string]$params.source
if ($source.ToLower().EndsWith('.msi')) {
  $process = Start-Process -FilePath 'msiexec.exe' -ArgumentList (@('/i', $source, '/qn', '/norestart') + $argsList) -Wait -PassThru
} else {
  $process = Start-Process -FilePath $source -ArgumentList $argsList -Wait -PassThru
}
if ($process.ExitCode -ne 0) {
  throw "package install failed with exit code $($process.ExitCode)"
}
`)
	return err
}
