//go:build windows

package module

import "context"

type AppxPackageModule struct{}

func (m *AppxPackageModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	if _, err := paramStringRequired(params, "name"); err != nil {
		return false, err
	}

	return runWindowsPowerShellBool(ctx, params, `
$name = [string]$params.name
$scope = if ($params.scope) { [string]$params.scope } else { 'both' }
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'absent' }
if ($ensure -ne 'absent') {
  throw "appx_package: only ensure=absent is supported"
}
$hasWildcard = [WildcardPattern]::ContainsWildcardCharacters($name)
$installed = @()
switch ($scope) {
  'current_user' { $installed = @(Get-AppxPackage -Name $name -ErrorAction SilentlyContinue) }
  'all_users' { $installed = @(Get-AppxPackage -AllUsers -Name $name -ErrorAction SilentlyContinue) }
  'provisioned' { $installed = @() }
  'both' { $installed = @(Get-AppxPackage -AllUsers -Name $name -ErrorAction SilentlyContinue) }
  default { throw "appx_package: unsupported scope $scope" }
}
$provisioned = @()
if ($scope -eq 'provisioned' -or $scope -eq 'both') {
  $provisioned = @(Get-AppxProvisionedPackage -Online | Where-Object {
    if ($hasWildcard) { $_.DisplayName -like $name } else { $_.DisplayName -eq $name }
  })
}
Write-Output ([bool](($installed.Count + $provisioned.Count) -gt 0))
`)
}

func (m *AppxPackageModule) Apply(ctx context.Context, params map[string]any) error {
	if _, err := paramStringRequired(params, "name"); err != nil {
		return err
	}

	_, err := runWindowsPowerShellWithParams(ctx, params, `
$name = [string]$params.name
$scope = if ($params.scope) { [string]$params.scope } else { 'both' }
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'absent' }
if ($ensure -ne 'absent') {
  throw "appx_package: only ensure=absent is supported"
}

if ($scope -eq 'current_user') {
  Get-AppxPackage -Name $name -ErrorAction SilentlyContinue | ForEach-Object {
    Remove-AppxPackage -Package $_.PackageFullName -ErrorAction SilentlyContinue
  }
} elseif ($scope -eq 'all_users' -or $scope -eq 'both') {
  Get-AppxPackage -AllUsers -Name $name -ErrorAction SilentlyContinue | ForEach-Object {
    try {
      Remove-AppxPackage -Package $_.PackageFullName -AllUsers -ErrorAction Stop
    } catch {
      Remove-AppxPackage -Package $_.PackageFullName -ErrorAction SilentlyContinue
    }
  }
}

if ($scope -eq 'provisioned' -or $scope -eq 'both') {
  $hasWildcard = [WildcardPattern]::ContainsWildcardCharacters($name)
  Get-AppxProvisionedPackage -Online | Where-Object {
    if ($hasWildcard) { $_.DisplayName -like $name } else { $_.DisplayName -eq $name }
  } | ForEach-Object {
    Remove-AppxProvisionedPackage -Online -PackageName $_.PackageName -ErrorAction SilentlyContinue | Out-Null
  }
}
`)
	return err
}
