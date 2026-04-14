package pscript

const FirewallRuleCheckScript = `
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
$rule = Get-NetFirewallRule -DisplayName $name -ErrorAction SilentlyContinue | Select-Object -First 1
if ($ensure -eq 'absent') {
  Write-Output ($null -ne $rule)
  exit 0
}
if ($null -eq $rule) {
  Write-Output 'true'
  exit 0
}
$directionMap = @{ inbound = 'Inbound'; outbound = 'Outbound' }
$actionMap = @{ allow = 'Allow'; block = 'Block' }
$protocolMap = @{ tcp = 'TCP'; udp = 'UDP'; any = 'Any' }
$portFilter = $rule | Get-NetFirewallPortFilter
$needs = $rule.Direction -ne $directionMap[[string]$params.direction] -or $rule.Action -ne $actionMap[[string]$params.action]
if ([string]$params.protocol) {
  $needs = $needs -or $portFilter.Protocol -ne $protocolMap[[string]$params.protocol]
}
if ([string]$params.ports) {
  $needs = $needs -or [string]$portFilter.LocalPort -ne [string]$params.ports
}
Write-Output $needs
`

const FirewallRuleApplyScript = `
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Remove-NetFirewallRule -DisplayName $name -ErrorAction SilentlyContinue
  exit 0
}
$directionMap = @{ inbound = 'Inbound'; outbound = 'Outbound' }
$actionMap = @{ allow = 'Allow'; block = 'Block' }
$protocolMap = @{ tcp = 'TCP'; udp = 'UDP'; any = 'Any' }
$existing = Get-NetFirewallRule -DisplayName $name -ErrorAction SilentlyContinue | Select-Object -First 1
if ($null -eq $existing) {
  $newParams = @{
    DisplayName = $name
    Direction = $directionMap[[string]$params.direction]
    Action = $actionMap[[string]$params.action]
    Protocol = $protocolMap[[string]$params.protocol]
  }
  if ([string]$params.ports) {
    $newParams['LocalPort'] = [string]$params.ports
  }
  New-NetFirewallRule @newParams | Out-Null
  exit 0
}
Set-NetFirewallRule -DisplayName $name -Direction $directionMap[[string]$params.direction] -Action $actionMap[[string]$params.action] | Out-Null
if ([string]$params.protocol -or [string]$params.ports) {
  Set-NetFirewallRule -DisplayName $name -Protocol $protocolMap[[string]$params.protocol] -LocalPort ([string]$params.ports) | Out-Null
}
`
