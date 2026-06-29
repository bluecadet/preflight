<#
.SYNOPSIS
    Bootstrap a disposable Windows VM for Preflight WinRM integration testing.
.DESCRIPTION
    Provisions WinRM (HTTP, Basic auth), creates a dedicated pf-test user,
    opens the firewall, and writes a sacrificial sentinel to the registry
    so the integration test can verify it is pointing at a safe target.

    The WinRM password is read from the env var PREFLIGHT_TEST_WINRM_PASS.
    If unset, you will be prompted for a password.

    Run this once inside a fresh Windows VM.
.LINK
    https://github.com/bluecadet/preflight/docs/how-to/winrm-integration-testing.md
#>

$ErrorActionPreference = 'Stop'

# ---- Password ----
$winrmPass = $env:PREFLIGHT_TEST_WINRM_PASS
if (-not $winrmPass) {
    $sec = Read-Host -Prompt 'Enter password for pf-test user' -AsSecureString
    $ptr = [System.Runtime.InteropServices.Marshal]::SecureStringToBSTR($sec)
    $winrmPass = [System.Runtime.InteropServices.Marshal]::PtrToStringBSTR($ptr)
    [System.Runtime.InteropServices.Marshal]::ZeroFreeBSTR($ptr)
}

Write-Host "==> 1. Enabling WinRM over HTTP with Basic auth on port 5985"
winrm quickconfig -q -force
winrm set winrm/config/service/Auth "@{Basic=`"true`"}"
winrm set winrm/config/service "@{AllowUnencrypted=`"true`"}"

Write-Host "==> 2. Creating pf-test user"
$pw = ConvertTo-SecureString $winrmPass -AsPlainText -Force
$existing = Get-LocalUser -Name 'pf-test' -ErrorAction SilentlyContinue
if ($existing) {
    Write-Host "    pf-test user already exists, resetting password"
    Set-LocalUser -Name 'pf-test' -Password $pw
} else {
    New-LocalUser -Name 'pf-test' -Password $pw -PasswordNeverExpires -AccountNeverExpires
}
Add-LocalGroupMember -Group 'Administrators' -Member 'pf-test' -ErrorAction SilentlyContinue
Add-LocalGroupMember -Group 'Remote Management Users' -Member 'pf-test' -ErrorAction SilentlyContinue

Write-Host "==> 3. Opening firewall rule for WinRM HTTP"
New-NetFirewallRule -DisplayName 'Preflight WinRM HTTP' `
    -Direction Inbound -Protocol TCP -LocalPort 5985 -Action Allow `
    -Profile Any -ErrorAction SilentlyContinue | Out-Null

Write-Host "==> 4. Writing sacrificial sentinel"
$sentinelPath = 'HKLM:\SOFTWARE\PreflightTest'
if (-not (Test-Path $sentinelPath)) {
    New-Item -Path $sentinelPath -Force | Out-Null
}
New-ItemProperty -LiteralPath $sentinelPath -Name 'IsSacrificial' -Value 1 -PropertyType DWord -Force | Out-Null

Write-Host ""
Write-Host "================================================================"
Write-Host " Bootstrap complete.  Set this environment variable on the"
Write-Host " controller machine to run the integration suite:"
Write-Host "================================================================"
Write-Host ""
$ip = (Get-NetIPConfiguration | Where-Object { $_.IPv4DefaultGateway -and $_.NetAdapter.Status -eq 'Up' }).IPAddress[0].IPAddress
$json = @{
    host = $ip
    port = 5985
    user = 'pf-test'
    pass = $winrmPass
} | ConvertTo-Json -Compress
Write-Host 'export PREFLIGHT_TEST_WINRM=''' + $json + ''''
Write-Host ""
Write-Host "(PowerShell: `$env:PREFLIGHT_TEST_WINRM = '$json')"
Write-Host ""
Write-Host "Then run: cd preflight && go test -v -run TestWinRMIntegration ./internal/target/"
