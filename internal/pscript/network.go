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
$ports = @($params.ports | Where-Object { "$_" -ne '' })
$portFilter = $rule | Get-NetFirewallPortFilter
$needs = $rule.Direction -ne $directionMap[[string]$params.direction] -or $rule.Action -ne $actionMap[[string]$params.action]
if ([string]$params.protocol) {
  $needs = $needs -or $portFilter.Protocol -ne $protocolMap[[string]$params.protocol]
}
if ($ports.Count -gt 0) {
  $needs = $needs -or (@($portFilter.LocalPort | Sort-Object) -join ',') -ne (@($ports | Sort-Object) -join ',')
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
$ports = @($params.ports | Where-Object { "$_" -ne '' })
$existing = Get-NetFirewallRule -DisplayName $name -ErrorAction SilentlyContinue | Select-Object -First 1
if ($null -eq $existing) {
  $newParams = @{
    DisplayName = $name
    Direction = $directionMap[[string]$params.direction]
    Action = $actionMap[[string]$params.action]
    Protocol = $protocolMap[[string]$params.protocol]
  }
  if ($ports.Count -gt 0) {
    $newParams['LocalPort'] = [string[]]$ports
  }
  New-NetFirewallRule @newParams | Out-Null
  exit 0
}
$setParams = @{
  DisplayName = $name
  Direction = $directionMap[[string]$params.direction]
  Action = $actionMap[[string]$params.action]
  Protocol = $protocolMap[[string]$params.protocol]
}
if ($ports.Count -gt 0) {
  $setParams['LocalPort'] = [string[]]$ports
}
Set-NetFirewallRule @setParams | Out-Null
`
