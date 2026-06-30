package pscript

const RegistryModuleCheckScript = `
$path = [string]$params.path
$user = if ($params.user) { [string]$params.user } else { '' }
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
function Resolve-RegistryPath([string]$path, [string]$user) {
  if (-not $user) { return $path }
  if (-not ($path -match '(?i)^(Registry::HKEY_CURRENT_USER|HKCU:|HKEY_CURRENT_USER\\)')) {
    throw 'registry: user can only be used with HKCU/HKEY_CURRENT_USER paths'
  }
  $account = New-Object System.Security.Principal.NTAccount($user)
  $sid = $account.Translate([System.Security.Principal.SecurityIdentifier]).Value
  $root = 'Registry::HKEY_USERS\' + $sid
  if (-not (Test-Path -LiteralPath $root)) {
    throw ("registry: profile hive is not loaded for user " + $user + "; sign in once before applying user-scoped registry settings")
  }
  $subPath = $path -replace '(?i)^(Registry::HKEY_CURRENT_USER\\?|HKCU:\\?|HKEY_CURRENT_USER\\?)', ''
  if ($subPath) { return (Join-Path $root $subPath) }
  return $root
}
$path = Resolve-RegistryPath $path $user
function Normalize-RegistryKind([string]$kind) {
  switch ($kind.ToLowerInvariant()) {
    'expandstring' { return 'expand_string' }
    'multistring' { return 'multi_string' }
    default { return $kind.ToLowerInvariant() }
  }
}
if ($ensure -eq 'absent') {
  Write-Output (Test-Path -LiteralPath $path)
  exit 0
}
if (-not (Test-Path -LiteralPath $path)) {
  if (-not $params.values) {
    Write-Output 'true'
    exit 0
  }
  $presentSpecs = @($params.values | Where-Object { -not $_.ensure -or $_.ensure -eq 'present' })
  Write-Output ([bool]($presentSpecs.Count -gt 0))
  exit 0
}
$needs = $false
if ($params.values) {
  $item = Get-Item -LiteralPath $path
  $props = Get-ItemProperty -LiteralPath $path
  foreach ($spec in $params.values) {
    $name = [string]$spec.name
    $ensureValue = if ($spec.ensure) { [string]$spec.ensure } else { 'present' }
    $prop = $props.PSObject.Properties[$name]
    if ($ensureValue -eq 'absent') {
      if ($null -ne $prop) {
        $needs = $true
        break
      }
      continue
    }

    if ($null -eq $prop) {
      $needs = $true
      break
    }

    $currentKind = Normalize-RegistryKind($item.GetValueKind($name).ToString())
    $desiredKind = [string]$spec.type
    if ($currentKind -ne $desiredKind) {
      $needs = $true
      break
    }

    if ($spec.patch) {
      if ($desiredKind -ne 'binary') {
        throw 'registry: patch is only supported for binary values'
      }
      $current = @($prop.Value | ForEach-Object { [int]$_ })
      foreach ($patch in @($spec.patch)) {
        $offset = [int]$patch.offset
        if ($current.Count -le $offset -or $current[$offset] -ne [int]$patch.data) {
          $needs = $true
          break
        }
      }
      if ($needs) {
        break
      }
      continue
    }

    switch ($desiredKind) {
      'string' { if ([string]$prop.Value -ne [string]$spec.data) { $needs = $true } }
      'expandstring' { if ([string]$prop.Value -ne [string]$spec.data) { $needs = $true } }
      'dword' { if ([int64]$prop.Value -ne [int64]$spec.data) { $needs = $true } }
      'qword' { if ([int64]$prop.Value -ne [int64]$spec.data) { $needs = $true } }
      'multistring' {
        $current = @($prop.Value | ForEach-Object { [string]$_ })
        $desired = @($spec.data | ForEach-Object { [string]$_ })
        if ($current.Count -ne $desired.Count) {
          $needs = $true
        } else {
          for ($i = 0; $i -lt $current.Count; $i++) {
            if ($current[$i] -ne $desired[$i]) {
              $needs = $true
              break
            }
          }
        }
      }
      'binary' {
        $current = @($prop.Value | ForEach-Object { [int]$_ })
        $desired = @($spec.data | ForEach-Object { [int]$_ })
        if ($current.Count -ne $desired.Count) {
          $needs = $true
        } else {
          for ($i = 0; $i -lt $current.Count; $i++) {
            if ($current[$i] -ne $desired[$i]) {
              $needs = $true
              break
            }
          }
        }
      }
      default { throw "registry: unsupported type $desiredKind" }
    }
    if ($needs) {
      break
    }
  }
}
Write-Output $needs
`

const RegistryCheckScript = `
$path = [string]$params.path
$user = if ($params.user) { [string]$params.user } else { '' }
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
function Resolve-RegistryPath([string]$path, [string]$user) {
  if (-not $user) { return $path }
  if (-not ($path -match '(?i)^(Registry::HKEY_CURRENT_USER|HKCU:|HKEY_CURRENT_USER\\)')) {
    throw 'registry: user can only be used with HKCU/HKEY_CURRENT_USER paths'
  }
  $account = New-Object System.Security.Principal.NTAccount($user)
  $sid = $account.Translate([System.Security.Principal.SecurityIdentifier]).Value
  $root = 'Registry::HKEY_USERS\' + $sid
  if (-not (Test-Path -LiteralPath $root)) {
    throw ("registry: profile hive is not loaded for user " + $user + "; sign in once before applying user-scoped registry settings")
  }
  $subPath = $path -replace '(?i)^(Registry::HKEY_CURRENT_USER\\?|HKCU:\\?|HKEY_CURRENT_USER\\?)', ''
  if ($subPath) { return (Join-Path $root $subPath) }
  return $root
}
$path = Resolve-RegistryPath $path $user
function Normalize-RegistryKind([string]$kind) {
  switch ($kind.ToLowerInvariant()) {
    'expandstring' { return 'expand_string' }
    'multistring' { return 'multi_string' }
    default { return $kind.ToLowerInvariant() }
  }
}
if ($ensure -eq 'absent') {
  Write-Output (Test-Path -LiteralPath $path)
  exit 0
}
if (-not (Test-Path -LiteralPath $path)) {
  if (-not $params.values) {
    Write-Output 'true'
    exit 0
  }
  $presentSpecs = @($params.values | Where-Object { -not $_.ensure -or $_.ensure -eq 'present' })
  Write-Output ([bool]($presentSpecs.Count -gt 0))
  exit 0
}
$needs = $false
if ($params.values) {
  $item = Get-Item -LiteralPath $path
  $props = Get-ItemProperty -LiteralPath $path
  foreach ($spec in $params.values) {
    $name = [string]$spec.name
    $ensureValue = if ($spec.ensure) { [string]$spec.ensure } else { 'present' }
    $prop = $props.PSObject.Properties[$name]
    if ($ensureValue -eq 'absent') {
      if ($null -ne $prop) {
        $needs = $true
        break
      }
      continue
    }
    if ($null -eq $prop) {
      $needs = $true
      break
    }
    $currentKind = Normalize-RegistryKind($item.GetValueKind($name).ToString())
    $desiredKind = [string]$spec.type
    if ($currentKind -ne $desiredKind) {
      $needs = $true
      break
    }
    if ($spec.patch) {
      if ($desiredKind -ne 'binary') {
        throw 'registry: patch is only supported for binary values'
      }
      $current = @($prop.Value | ForEach-Object { [int]$_ })
      foreach ($patch in @($spec.patch)) {
        $offset = [int]$patch.offset
        if ($current.Count -le $offset -or $current[$offset] -ne [int]$patch.data) {
          $needs = $true
          break
        }
      }
      if ($needs) {
        break
      }
      continue
    }
    switch ($desiredKind) {
      'string' { if ([string]$prop.Value -ne [string]$spec.data) { $needs = $true } }
      'expand_string' { if ([string]$prop.Value -ne [string]$spec.data) { $needs = $true } }
      'dword' { if ([int64]$prop.Value -ne [int64]$spec.data) { $needs = $true } }
      'qword' { if ([int64]$prop.Value -ne [int64]$spec.data) { $needs = $true } }
      'multi_string' {
        $current = @($prop.Value | ForEach-Object { [string]$_ })
        $desired = @($spec.data | ForEach-Object { [string]$_ })
        if ($current.Count -ne $desired.Count) {
          $needs = $true
        } else {
          for ($i = 0; $i -lt $current.Count; $i++) {
            if ($current[$i] -ne $desired[$i]) {
              $needs = $true
              break
            }
          }
        }
      }
      'binary' {
        $current = @($prop.Value | ForEach-Object { [int]$_ })
        $desired = @($spec.data | ForEach-Object { [int]$_ })
        if ($current.Count -ne $desired.Count) {
          $needs = $true
        } else {
          for ($i = 0; $i -lt $current.Count; $i++) {
            if ($current[$i] -ne $desired[$i]) {
              $needs = $true
              break
            }
          }
        }
      }
      default { throw "registry: unsupported type $desiredKind" }
    }
    if ($needs) {
      break
    }
  }
}
Write-Output $needs
`

const RegistryApplyScript = `
$path = [string]$params.path
$user = if ($params.user) { [string]$params.user } else { '' }
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
function Resolve-RegistryPath([string]$path, [string]$user) {
  if (-not $user) { return $path }
  if (-not ($path -match '(?i)^(Registry::HKEY_CURRENT_USER|HKCU:|HKEY_CURRENT_USER\\)')) {
    throw 'registry: user can only be used with HKCU/HKEY_CURRENT_USER paths'
  }
  $account = New-Object System.Security.Principal.NTAccount($user)
  $sid = $account.Translate([System.Security.Principal.SecurityIdentifier]).Value
  $root = 'Registry::HKEY_USERS\' + $sid
  if (-not (Test-Path -LiteralPath $root)) {
    throw ("registry: profile hive is not loaded for user " + $user + "; sign in once before applying user-scoped registry settings")
  }
  $subPath = $path -replace '(?i)^(Registry::HKEY_CURRENT_USER\\?|HKCU:\\?|HKEY_CURRENT_USER\\?)', ''
  if ($subPath) { return (Join-Path $root $subPath) }
  return $root
}
$path = Resolve-RegistryPath $path $user
if ($ensure -eq 'absent') {
  Remove-Item -LiteralPath $path -Recurse -Force -ErrorAction SilentlyContinue
  exit 0
}
if (-not (Test-Path -LiteralPath $path)) {
  New-Item -Path $path -Force -ErrorAction Stop | Out-Null
}
if ($params.values) {
  foreach ($spec in $params.values) {
    $name = [string]$spec.name
    $ensureValue = if ($spec.ensure) { [string]$spec.ensure } else { 'present' }
    if ($ensureValue -eq 'absent') {
      Remove-ItemProperty -LiteralPath $path -Name $name -Force -ErrorAction SilentlyContinue
      continue
    }
    if ($spec.patch) {
      $item = Get-Item -LiteralPath $path
      $props = Get-ItemProperty -LiteralPath $path
      $prop = $props.PSObject.Properties[$name]
      if ($null -eq $prop) {
        throw ("registry: cannot patch missing binary value " + $name)
      }
      if ($item.GetValueKind($name).ToString().ToLowerInvariant() -ne 'binary') {
        throw ("registry: cannot patch non-binary value " + $name)
      }
      $value = [byte[]]@($prop.Value | ForEach-Object { [byte][int]$_ })
      foreach ($patch in @($spec.patch)) {
        $offset = [int]$patch.offset
        if ($value.Length -le $offset) {
          throw ("registry: binary patch offset " + $offset + " is outside value " + $name)
        }
        $value[$offset] = [byte][int]$patch.data
      }
      Remove-ItemProperty -LiteralPath $path -Name $name -Force -ErrorAction SilentlyContinue
      New-ItemProperty -LiteralPath $path -Name $name -Value $value -PropertyType Binary -Force -ErrorAction Stop | Out-Null
      continue
    }
    $kindMap = @{
      string = 'String'
      expand_string = 'ExpandString'
      dword = 'DWord'
      qword = 'QWord'
      multi_string = 'MultiString'
      binary = 'Binary'
    }
    $value = switch ([string]$spec.type) {
      'multi_string' { @($spec.data | ForEach-Object { [string]$_ }) }
      'binary' { [byte[]]@($spec.data | ForEach-Object { [byte][int]$_ }) }
      'dword' { [int]$spec.data }
      'qword' { [int64]$spec.data }
      default { $spec.data }
    }
    Remove-ItemProperty -LiteralPath $path -Name $name -Force -ErrorAction SilentlyContinue
    New-ItemProperty -LiteralPath $path -Name $name -Value $value -PropertyType $kindMap[[string]$spec.type] -Force -ErrorAction Stop | Out-Null
  }
}
`

// RegistryEnsureScript combines check and apply in one PowerShell invocation.
// It outputs "ok", "changed", or "would-change" (dry-run). $__pf_dry_run must
// be injected by the caller before $params.
const RegistryEnsureScript = `
$path = [string]$params.path
$user = if ($params.user) { [string]$params.user } else { '' }
$ensure = if ($params.ensure) { [string]$params.ensure } else { 'present' }
function Resolve-RegistryPath([string]$path, [string]$user) {
  if (-not $user) { return $path }
  if (-not ($path -match '(?i)^(Registry::HKEY_CURRENT_USER|HKCU:|HKEY_CURRENT_USER\\)')) {
    throw 'registry: user can only be used with HKCU/HKEY_CURRENT_USER paths'
  }
  $account = New-Object System.Security.Principal.NTAccount($user)
  $sid = $account.Translate([System.Security.Principal.SecurityIdentifier]).Value
  $root = 'Registry::HKEY_USERS\' + $sid
  if (-not (Test-Path -LiteralPath $root)) {
    throw ("registry: profile hive is not loaded for user " + $user + "; sign in once before applying user-scoped registry settings")
  }
  $subPath = $path -replace '(?i)^(Registry::HKEY_CURRENT_USER\\?|HKCU:\\?|HKEY_CURRENT_USER\\?)', ''
  if ($subPath) { return (Join-Path $root $subPath) }
  return $root
}
$path = Resolve-RegistryPath $path $user
function Normalize-RegistryKind([string]$kind) {
  switch ($kind.ToLowerInvariant()) {
    'expandstring' { return 'expand_string' }
    'multistring'  { return 'multi_string' }
    default        { return $kind.ToLowerInvariant() }
  }
}
$needs = $false
if ($ensure -eq 'absent') {
  $needs = Test-Path -LiteralPath $path
} elseif (-not (Test-Path -LiteralPath $path)) {
  if ($params.values) {
    $presentSpecs = @($params.values | Where-Object { -not $_.ensure -or $_.ensure -eq 'present' })
    $needs = $presentSpecs.Count -gt 0
  }
} elseif ($params.values) {
  $item = Get-Item -LiteralPath $path
  $props = Get-ItemProperty -LiteralPath $path
  foreach ($spec in $params.values) {
    $name = [string]$spec.name
    $ensureValue = if ($spec.ensure) { [string]$spec.ensure } else { 'present' }
    $prop = $props.PSObject.Properties[$name]
    if ($ensureValue -eq 'absent') {
      if ($null -ne $prop) { $needs = $true; break }
      continue
    }
    if ($null -eq $prop) { $needs = $true; break }
    $currentKind = Normalize-RegistryKind($item.GetValueKind($name).ToString())
    $desiredKind = [string]$spec.type
    if ($currentKind -ne $desiredKind) { $needs = $true; break }
    if ($spec.patch) {
      if ($desiredKind -ne 'binary') {
        throw 'registry: patch is only supported for binary values'
      }
      $current = @($prop.Value | ForEach-Object { [int]$_ })
      foreach ($patch in @($spec.patch)) {
        $offset = [int]$patch.offset
        if ($current.Count -le $offset -or $current[$offset] -ne [int]$patch.data) {
          $needs = $true
          break
        }
      }
      if ($needs) { break }
      continue
    }
    switch ($desiredKind) {
      'string'        { if ([string]$prop.Value -ne [string]$spec.data) { $needs = $true } }
      'expand_string' { if ([string]$prop.Value -ne [string]$spec.data) { $needs = $true } }
      'dword'         { if ([int64]$prop.Value -ne [int64]$spec.data) { $needs = $true } }
      'qword'         { if ([int64]$prop.Value -ne [int64]$spec.data) { $needs = $true } }
      'multi_string' {
        $current = @($prop.Value | ForEach-Object { [string]$_ })
        $desired = @($spec.data | ForEach-Object { [string]$_ })
        if ($current.Count -ne $desired.Count) { $needs = $true }
        else { for ($i = 0; $i -lt $current.Count; $i++) { if ($current[$i] -ne $desired[$i]) { $needs = $true; break } } }
      }
      'binary' {
        $current = @($prop.Value | ForEach-Object { [int]$_ })
        $desired = @($spec.data | ForEach-Object { [int]$_ })
        if ($current.Count -ne $desired.Count) { $needs = $true }
        else { for ($i = 0; $i -lt $current.Count; $i++) { if ($current[$i] -ne $desired[$i]) { $needs = $true; break } } }
      }
      default { throw "registry: unsupported type $desiredKind" }
    }
    if ($needs) { break }
  }
}
if (-not $needs) { Write-Output 'ok'; exit 0 }
if ($__pf_dry_run) { Write-Output 'would-change'; exit 0 }
if ($ensure -eq 'absent') {
  Remove-Item -LiteralPath $path -Recurse -Force -ErrorAction SilentlyContinue
} else {
  if (-not (Test-Path -LiteralPath $path)) {
    New-Item -Path $path -Force -ErrorAction Stop | Out-Null
  }
  if ($params.values) {
    foreach ($spec in $params.values) {
      $name = [string]$spec.name
      $ensureValue = if ($spec.ensure) { [string]$spec.ensure } else { 'present' }
      if ($ensureValue -eq 'absent') {
        Remove-ItemProperty -LiteralPath $path -Name $name -Force -ErrorAction SilentlyContinue
        continue
      }
      if ($spec.patch) {
        $item = Get-Item -LiteralPath $path
        $props = Get-ItemProperty -LiteralPath $path
        $prop = $props.PSObject.Properties[$name]
        if ($null -eq $prop) {
          throw ("registry: cannot patch missing binary value " + $name)
        }
        if ((Normalize-RegistryKind($item.GetValueKind($name).ToString())) -ne 'binary') {
          throw ("registry: cannot patch non-binary value " + $name)
        }
        $value = [byte[]]@($prop.Value | ForEach-Object { [byte][int]$_ })
        foreach ($patch in @($spec.patch)) {
          $offset = [int]$patch.offset
          if ($value.Length -le $offset) {
            throw ("registry: binary patch offset " + $offset + " is outside value " + $name)
          }
          $value[$offset] = [byte][int]$patch.data
        }
        Remove-ItemProperty -LiteralPath $path -Name $name -Force -ErrorAction SilentlyContinue
        New-ItemProperty -LiteralPath $path -Name $name -Value $value -PropertyType Binary -Force -ErrorAction Stop | Out-Null
        continue
      }
      $kindMap = @{
        string = 'String'; expand_string = 'ExpandString'; dword = 'DWord'
        qword = 'QWord'; multi_string = 'MultiString'; binary = 'Binary'
      }
      $value = switch ([string]$spec.type) {
        'multi_string' { @($spec.data | ForEach-Object { [string]$_ }) }
        'binary'       { [byte[]]@($spec.data | ForEach-Object { [byte][int]$_ }) }
        'dword'        { [int]$spec.data }
        'qword'        { [int64]$spec.data }
        default        { $spec.data }
      }
      Remove-ItemProperty -LiteralPath $path -Name $name -Force -ErrorAction SilentlyContinue
      New-ItemProperty -LiteralPath $path -Name $name -Value $value -PropertyType $kindMap[[string]$spec.type] -Force -ErrorAction Stop | Out-Null
    }
  }
}
Write-Output 'changed'
`
