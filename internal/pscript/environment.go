package pscript

// EnvironmentUserCheckScript reports whether an environment variable in
// another user's profile (HKU\<SID>\Environment) needs to change. It expects
// $params.name, $params.user and optionally $params.value and $params.ensure.
const EnvironmentUserCheckScript = UserProfileHelpers + `
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
Invoke-WithUserHive ([string]$params.user) {
  param($root)
  $key = Get-Item -LiteralPath ($root + '\Environment') -ErrorAction SilentlyContinue
  $current = $null
  if ($key) { $current = $key.GetValue($name, $null, 'DoNotExpandEnvironmentNames') }
  if ($ensure -eq 'absent') {
    Write-Output ($null -ne $current)
  } else {
    Write-Output ($current -ne [string]$params.value)
  }
}
`

// EnvironmentUserApplyScript sets or removes an environment variable in
// another user's profile. Parameters match EnvironmentUserCheckScript.
const EnvironmentUserApplyScript = UserProfileHelpers + `
$name = [string]$params.name
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
Invoke-WithUserHive ([string]$params.user) {
  param($root)
  $keyPath = $root + '\Environment'
  if (-not (Test-Path -LiteralPath $keyPath)) {
    New-Item -Path $keyPath -Force -ErrorAction Stop | Out-Null
  }
  if ($ensure -eq 'absent') {
    Remove-ItemProperty -LiteralPath $keyPath -Name $name -Force -ErrorAction SilentlyContinue
    return
  }
  $value = [string]$params.value
  $kind = if ($value -match '%') { 'ExpandString' } else { 'String' }
  New-ItemProperty -LiteralPath $keyPath -Name $name -Value $value -PropertyType $kind -Force -ErrorAction Stop | Out-Null
}
`
