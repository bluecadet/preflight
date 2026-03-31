//go:build windows

package module

import "context"

type RegistryModule struct{}

func (m *RegistryModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	if _, err := paramStringRequired(params, "path"); err != nil {
		return false, err
	}
	if _, err := paramString(params, "ensure", "present"); err != nil {
		return false, err
	}

	return runWindowsPowerShellBool(ctx, params, `
$path = [string]$params.path
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Write-Output (Test-Path -LiteralPath $path)
  exit 0
}
if (-not (Test-Path -LiteralPath $path)) {
  Write-Output 'true'
  exit 0
}
$needs = $false
if ($params.values) {
  $item = Get-ItemProperty -LiteralPath $path
  foreach ($prop in $params.values.PSObject.Properties) {
    $currentProp = $item.PSObject.Properties[$prop.Name]
    if ($null -eq $currentProp) {
      $needs = $true
      break
    }
    if ([string]$currentProp.Value -ne [string]$prop.Value) {
      $needs = $true
      break
    }
  }
}
Write-Output $needs
`)
}

func (m *RegistryModule) Apply(ctx context.Context, params map[string]any) error {
	if _, err := paramStringRequired(params, "path"); err != nil {
		return err
	}
	if _, err := paramString(params, "ensure", "present"); err != nil {
		return err
	}

	_, err := runWindowsPowerShellWithParams(ctx, params, `
$path = [string]$params.path
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Remove-Item -LiteralPath $path -Recurse -Force -ErrorAction SilentlyContinue
  exit 0
}
New-Item -Path $path -Force | Out-Null
if ($params.values) {
  foreach ($prop in $params.values.PSObject.Properties) {
    New-ItemProperty -LiteralPath $path -Name $prop.Name -Value $prop.Value -Force | Out-Null
  }
}
`)
	return err
}
