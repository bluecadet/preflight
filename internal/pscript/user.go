package pscript

const UserCheckScript = `
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
    $members = net localgroup ([string]$group) 2>$null
    $inMembers = $false
    $isMember = $false
    foreach ($_ in $members) {
      if ($_ -match '^-{5,}') { $inMembers = $true; continue }
      if ($inMembers) {
        $trimmed = $_.Trim()
        if ($trimmed -match 'completed successfully') { break }
        if ($trimmed -match "^\*?$([regex]::Escape($name))$") { $isMember = $true; break }
      }
    }
    if (-not $isMember) { $needs = $true; break }
  }
}
Write-Output $needs
`

const UserApplyScript = `
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
    net localgroup ([string]$group) $name /add 2>$null
  }
}
`
