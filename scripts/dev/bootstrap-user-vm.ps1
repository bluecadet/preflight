<#
.SYNOPSIS
    Provision the local test account for a disposable Preflight integration VM.
.DESCRIPTION
    Creates the local (non-domain) pf-test admin user and writes a sacrificial
    sentinel to the registry so the integration suite can verify it is pointing
    at a safe target. This is the identity + safety step; run it once, then run
    bootstrap-winrm-vm.ps1 and/or bootstrap-ssh-vm.ps1 to enable the transports
    you want to test. Those transport scripts reuse this pf-test account.

    The password is read from PREFLIGHT_TEST_WINRM_PASS (falling back to
    PREFLIGHT_TEST_SSH_PASS); if neither is set, you will be prompted.
.LINK
    https://github.com/bluecadet/preflight/blob/main/docs/development/winrm-integration-testing.md
#>

$ErrorActionPreference = 'Stop'

# ---- Password ----
$pass = $env:PREFLIGHT_TEST_WINRM_PASS
if (-not $pass) { $pass = $env:PREFLIGHT_TEST_SSH_PASS }
if (-not $pass) {
    $sec = Read-Host -Prompt 'Enter password for pf-test user' -AsSecureString
    $ptr = [System.Runtime.InteropServices.Marshal]::SecureStringToBSTR($sec)
    $pass = [System.Runtime.InteropServices.Marshal]::PtrToStringBSTR($ptr)
    [System.Runtime.InteropServices.Marshal]::ZeroFreeBSTR($ptr)
}

Write-Host "==> 1. Creating pf-test user"
$pw = ConvertTo-SecureString $pass -AsPlainText -Force
if (Get-LocalUser -Name 'pf-test' -ErrorAction SilentlyContinue) {
    Write-Host "    pf-test user already exists, resetting password"
    Set-LocalUser -Name 'pf-test' -Password $pw
} else {
    New-LocalUser -Name 'pf-test' -Password $pw -PasswordNeverExpires -AccountNeverExpires
}
Add-LocalGroupMember -Group 'Administrators' -Member 'pf-test' -ErrorAction SilentlyContinue

Write-Host "==> 2. Writing sacrificial sentinel"
$sentinelPath = 'HKLM:\SOFTWARE\PreflightTest'
if (-not (Test-Path $sentinelPath)) {
    New-Item -Path $sentinelPath -Force | Out-Null
}
New-ItemProperty -LiteralPath $sentinelPath -Name 'IsSacrificial' -Value 1 -PropertyType DWord -Force | Out-Null

Write-Host ""
Write-Host "==============================================================="
Write-Host " pf-test provisioned.  Next, enable one or both transports:"
Write-Host "   .\bootstrap-winrm-vm.ps1   # WinRM on port 5985"
Write-Host "   .\bootstrap-ssh-vm.ps1     # SSH on port 22 (OpenSSH Server)"
Write-Host ""
Write-Host " Then add the matching vars to your .env.test file:"
Write-Host "==============================================================="
Write-Host ""
# Prefer the adapter that has a default gateway; fall back to any non-loopback,
# non-link-local IPv4 address (host-only networks often have no gateway).
$ip = (Get-NetIPConfiguration -ErrorAction SilentlyContinue |
    Where-Object { $_.IPv4DefaultGateway -and $_.NetAdapter.Status -eq 'Up' } |
    Select-Object -First 1).IPv4Address.IPAddress
if (-not $ip) {
    $ip = Get-NetIPAddress -AddressFamily IPv4 -ErrorAction SilentlyContinue |
        Where-Object { $_.IPAddress -notlike '127.*' -and $_.IPAddress -notlike '169.254.*' } |
        Select-Object -First 1 -ExpandProperty IPAddress
}
if (-not $ip) { $ip = '<vm-ip-address>' }
Write-Host "PREFLIGHT_TEST_WINRM_HOST=$ip"
Write-Host "PREFLIGHT_TEST_WINRM_PORT=5985"
Write-Host "PREFLIGHT_TEST_WINRM_USER=pf-test"
Write-Host "PREFLIGHT_TEST_WINRM_PASS=$pass"
Write-Host ""
Write-Host "PREFLIGHT_TEST_SSH_HOST=$ip"
Write-Host "PREFLIGHT_TEST_SSH_PORT=22"
Write-Host "PREFLIGHT_TEST_SSH_USER=pf-test"
Write-Host "PREFLIGHT_TEST_SSH_PASS=$pass"
