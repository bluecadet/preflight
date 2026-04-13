//go:build windows

package module

import "context"

type ServiceModule struct{}

func (m *ServiceModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	var p ServiceParams
	if err := Decode(params, &p); err != nil {
		return false, err
	}
	return runWindowsPowerShellBool(ctx, params, `
$name = [string]$params.name
$desiredState = if ($params.state) { [string]$params.state } else { '' }
$desiredStartup = if ($params.startup_type) { [string]$params.startup_type } else { '' }
$filterName = $name.Replace("'", "''")
$service = Get-CimInstance Win32_Service -Filter ("Name='" + $filterName + "'")
if ($null -eq $service) {
  throw "service not found: $name"
}
$needs = $false
if ($desiredState -eq 'disabled') {
  if ($service.State -ne 'Stopped' -or $service.StartMode -ne 'Disabled') {
    $needs = $true
  }
} else {
  if ($desiredState -eq 'running' -and $service.State -ne 'Running') {
    $needs = $true
  }
  if ($desiredState -eq 'stopped' -and $service.State -ne 'Stopped') {
    $needs = $true
  }
  if ($desiredStartup) {
    $startupMap = @{ automatic = 'Auto'; manual = 'Manual'; disabled = 'Disabled' }
    if ($startupMap[$desiredStartup] -ne $service.StartMode) {
      $needs = $true
    }
  }
}
Write-Output $needs
`)
}

func (m *ServiceModule) Apply(ctx context.Context, params map[string]any) error {
	var p ServiceParams
	if err := Decode(params, &p); err != nil {
		return err
	}
	_, err := runWindowsPowerShellWithParams(ctx, params, `
$name = [string]$params.name
$desiredState = if ($params.state) { [string]$params.state } else { '' }
$desiredStartup = if ($params.startup_type) { [string]$params.startup_type } else { '' }
if ($desiredState -eq 'disabled') {
  Stop-Service -Name $name -Force -ErrorAction SilentlyContinue
  Set-Service -Name $name -StartupType Disabled
  exit 0
}
if ($desiredStartup) {
  $startupMap = @{ automatic = 'Automatic'; manual = 'Manual'; disabled = 'Disabled' }
  Set-Service -Name $name -StartupType $startupMap[$desiredStartup]
}
if ($desiredState -eq 'running') {
  Start-Service -Name $name
}
if ($desiredState -eq 'stopped') {
  Stop-Service -Name $name -Force
}
`)
	return err
}
