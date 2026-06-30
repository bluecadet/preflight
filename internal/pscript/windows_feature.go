package pscript

const WindowsFeatureCheckScript = `
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
`

const WindowsFeatureApplyScript = `
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
if ($ensure -eq 'absent') {
  Disable-WindowsOptionalFeature -Online -FeatureName $name -NoRestart | Out-Null
  exit 0
}
Enable-WindowsOptionalFeature -Online -FeatureName $name -LimitAccess -NoRestart | Out-Null
`
