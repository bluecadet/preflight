//go:build windows

package module

import "context"

type WindowsFeatureModule struct{}

func (m *WindowsFeatureModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	var p WindowsFeatureParams
	if err := Decode(params, &p); err != nil {
		return false, err
	}
	return runWindowsPowerShellBool(ctx, params, `
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
$feature = Get-WindowsOptionalFeature -Online -FeatureName $name -ErrorAction SilentlyContinue
if ($null -eq $feature) {
  throw "windows_feature not found: $name"
}
if ($ensure -eq 'absent') {
  Write-Output ($feature.State -eq 'Enabled')
  exit 0
}
Write-Output ($feature.State -ne 'Enabled')
`)
}

func (m *WindowsFeatureModule) Apply(ctx context.Context, params map[string]any) error {
	var p WindowsFeatureParams
	if err := Decode(params, &p); err != nil {
		return err
	}
	_, err := runWindowsPowerShellWithParams(ctx, params, `
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Disable-WindowsOptionalFeature -Online -FeatureName $name -NoRestart | Out-Null
  exit 0
}
Enable-WindowsOptionalFeature -Online -FeatureName $name -NoRestart | Out-Null
`)
	return err
}
