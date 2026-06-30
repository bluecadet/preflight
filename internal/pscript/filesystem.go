package pscript

const ShortcutCheckScript = `
$destination = [System.Environment]::ExpandEnvironmentVariables([string]$params.destination)
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
$target = [System.Environment]::ExpandEnvironmentVariables([string]$params.target)
$args = if ($params.args) { [string]$params.args } else { '' }
$needs = $shortcut.TargetPath -ne $target -or $shortcut.Arguments -ne $args
if ($params.icon) {
  $icon = [System.Environment]::ExpandEnvironmentVariables([string]$params.icon)
  if ($shortcut.IconLocation -ne $icon) { $needs = $true }
}
Write-Output $needs
`

const ShortcutApplyScript = `
$destination = [System.Environment]::ExpandEnvironmentVariables([string]$params.destination)
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
$shortcut.TargetPath = [System.Environment]::ExpandEnvironmentVariables([string]$params.target)
$shortcut.Arguments = if ($params.args) { [string]$params.args } else { '' }
if ($params.icon) { $shortcut.IconLocation = [System.Environment]::ExpandEnvironmentVariables([string]$params.icon) }
$shortcut.Save()
`
