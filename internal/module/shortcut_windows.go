//go:build windows

package module

import "context"

type ShortcutModule struct{}

func (m *ShortcutModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	var p ShortcutParams
	if err := Decode(params, &p); err != nil {
		return false, err
	}
	return runWindowsPowerShellBool(ctx, params, `
$destination = [string]$params.destination
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Write-Output (Test-Path -LiteralPath $destination)
  exit 0
}
if (-not (Test-Path -LiteralPath $destination)) {
  Write-Output 'true'
  exit 0
}
$shell = New-Object -ComObject WScript.Shell
$shortcut = $shell.CreateShortcut($destination)
$args = if ($params.args) { [string]$params.args } else { '' }
$icon = if ($params.icon) { [string]$params.icon } else { '' }
$needs = $shortcut.TargetPath -ne [string]$params.target -or $shortcut.Arguments -ne $args -or $shortcut.IconLocation -ne $icon
Write-Output $needs
`)
}

func (m *ShortcutModule) Apply(ctx context.Context, params map[string]any) error {
	var p ShortcutParams
	if err := Decode(params, &p); err != nil {
		return err
	}
	_, err := runWindowsPowerShellWithParams(ctx, params, `
$destination = [string]$params.destination
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Remove-Item -LiteralPath $destination -Force -ErrorAction SilentlyContinue
  exit 0
}
$parent = Split-Path -Parent $destination
if ($parent) {
  New-Item -ItemType Directory -Path $parent -Force | Out-Null
}
$shell = New-Object -ComObject WScript.Shell
$shortcut = $shell.CreateShortcut($destination)
$shortcut.TargetPath = [string]$params.target
$shortcut.Arguments = if ($params.args) { [string]$params.args } else { '' }
$shortcut.IconLocation = if ($params.icon) { [string]$params.icon } else { '' }
$shortcut.Save()
`)
	return err
}
