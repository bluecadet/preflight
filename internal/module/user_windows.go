//go:build windows

package module

import "context"

type UserModule struct{}

func (m *UserModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	if _, err := paramStringRequired(params, "name"); err != nil {
		return false, err
	}
	if _, err := paramString(params, "ensure", "present"); err != nil {
		return false, err
	}

	return runWindowsPowerShellBool(ctx, params, `
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
$user = Get-LocalUser -Name $name -ErrorAction SilentlyContinue
if ($ensure -eq 'absent') {
  Write-Output ($null -ne $user)
  exit 0
}
if ($null -eq $user) {
  Write-Output 'true'
  exit 0
}
$needs = $false
if ($params.password) {
  $needs = $true
}
if ($params.groups) {
  foreach ($group in $params.groups) {
    $members = Get-LocalGroupMember -Group ([string]$group) -ErrorAction SilentlyContinue
    if (-not ($members | Where-Object { $_.Name -match ("(^|\\\\)" + [regex]::Escape($name) + "$") })) {
      $needs = $true
      break
    }
  }
}
Write-Output $needs
`)
}

func (m *UserModule) Apply(ctx context.Context, params map[string]any) error {
	if _, err := paramStringRequired(params, "name"); err != nil {
		return err
	}
	if _, err := paramString(params, "ensure", "present"); err != nil {
		return err
	}

	_, err := runWindowsPowerShellWithParams(ctx, params, `
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Remove-LocalUser -Name $name -ErrorAction SilentlyContinue
  exit 0
}
$passwordValue = if ($params.password) { [string]$params.password } else { '' }
$securePassword = $null
if ($passwordValue) {
  $securePassword = ConvertTo-SecureString $passwordValue -AsPlainText -Force
}
$user = Get-LocalUser -Name $name -ErrorAction SilentlyContinue
if ($null -eq $user) {
  if ($securePassword) {
    New-LocalUser -Name $name -Password $securePassword | Out-Null
  } else {
    New-LocalUser -Name $name -NoPassword | Out-Null
  }
} elseif ($securePassword) {
  $user | Set-LocalUser -Password $securePassword
}
if ($params.groups) {
  foreach ($group in $params.groups) {
    Add-LocalGroupMember -Group ([string]$group) -Member $name -ErrorAction SilentlyContinue
  }
}
`)
	return err
}
