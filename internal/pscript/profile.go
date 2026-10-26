package pscript

// UserProfileHelpers defines PowerShell helpers that run a script block against
// another user's registry hive.
//
//	Invoke-WithUserHive <name> { param($root) ... }
//
// $root is a Registry:: path to the user's HKEY_USERS hive. When the user is
// signed in the loaded hive is used as-is. Otherwise NTUSER.DAT is loaded under
// HKU\preflight-<SID> for the duration of the block and unloaded afterwards.
// A failed unload is retried; if it still fails it is reported as an error
// only when the block itself succeeded, so it never masks the block's error.
//
// Convert-HkcuPath rewrites an HKCU path to live under $root.
const UserProfileHelpers = `
function Invoke-WithUserHive([string]$user, [scriptblock]$body) {
  try {
    $sid = (New-Object System.Security.Principal.NTAccount($user)).Translate([System.Security.Principal.SecurityIdentifier]).Value
  } catch {
    throw ("user " + $user + " was not found on this machine")
  }
  $loaded = 'Registry::HKEY_USERS\' + $sid
  if (Test-Path -LiteralPath $loaded) { return (& $body $loaded) }
  $missing = "user " + $user + " has no profile on this machine; sign in once to create it"
  $listKey = 'Registry::HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList\' + $sid
  $profilePath = (Get-ItemProperty -LiteralPath $listKey -Name ProfileImagePath -ErrorAction SilentlyContinue).ProfileImagePath
  if (-not $profilePath) { throw $missing }
  $ntuser = Join-Path ([Environment]::ExpandEnvironmentVariables($profilePath)) 'NTUSER.DAT'
  if (-not (Test-Path -LiteralPath $ntuser)) { throw $missing }
  $mount = 'preflight-' + $sid
  $loadOut = & reg.exe load ('HKU\' + $mount) $ntuser 2>&1
  if ($LASTEXITCODE -ne 0) {
    throw ("could not load the profile hive for user " + $user + ": " + (($loadOut | Out-String).Trim()))
  }
  $bodyFailed = $true
  try {
    $result = & $body ('Registry::HKEY_USERS\' + $mount)
    $bodyFailed = $false
    return $result
  } finally {
    for ($attempt = 0; $attempt -lt 3; $attempt++) {
      [gc]::Collect()
      [gc]::WaitForPendingFinalizers()
      $unloadOut = & reg.exe unload ('HKU\' + $mount) 2>&1
      if ($LASTEXITCODE -eq 0) { break }
      Start-Sleep -Milliseconds 500
    }
    if ($LASTEXITCODE -ne 0) {
      $unloadErr = "could not unload the profile hive for user " + $user + ": " + (($unloadOut | Out-String).Trim())
      # Never mask the body's own error; it is the root cause.
      if ($bodyFailed) { Write-Warning $unloadErr } else { throw ($unloadErr + "; the hive stays loaded until it is unloaded or the machine restarts") }
    }
  }
}
function Convert-HkcuPath([string]$path, [string]$root) {
  if (-not ($path -match '(?i)^(Registry::HKEY_CURRENT_USER|HKCU:|HKEY_CURRENT_USER\\)')) {
    throw 'registry: user can only be used with HKCU/HKEY_CURRENT_USER paths'
  }
  $subPath = $path -replace '(?i)^(Registry::HKEY_CURRENT_USER\\?|HKCU:\\?|HKEY_CURRENT_USER\\?)', ''
  if ($subPath) { return ($root + '\' + $subPath) }
  return $root
}
`
