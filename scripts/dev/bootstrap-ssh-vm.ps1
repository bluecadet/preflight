<#
.SYNOPSIS
    Enable SSH-to-Windows on a Preflight integration test VM.
.DESCRIPTION
    Installs the OpenSSH Server feature, starts sshd, and opens the firewall
    for port 22.

    This script only enables the SSH transport. It does NOT create the pf-test
    user or write the sacrificial sentinel - bootstrap-user-vm.ps1 owns those.
    Run that first (it also prints the .env.test connection vars); the SSH
    tests reuse the same pf-test account and password.

    Preflight authenticates over SSH with password auth by default, so no key
    generation is required. Run this once inside the VM.
.LINK
    https://github.com/bluecadet/preflight/blob/main/docs/development/winrm-integration-testing.md
#>

$ErrorActionPreference = 'Stop'

Write-Host "==> 1. Installing OpenSSH Server feature"
$cap = Get-WindowsCapability -Online -Name 'OpenSSH.Server*'
if ($cap.State -ne 'Installed') {
    Add-WindowsCapability -Online -Name $cap.Name | Out-Null
} else {
    Write-Host "    OpenSSH Server already installed"
}

Write-Host "==> 2. Starting sshd and setting it to start automatically"
Set-Service -Name sshd -StartupType Automatic
Start-Service sshd

Write-Host "==> 3. Opening firewall rule for SSH"
# The OpenSSH Server install usually creates 'OpenSSH-Server-In-TCP'; add our
# own rule idempotently so the port is open regardless of the install path.
New-NetFirewallRule -DisplayName 'Preflight SSH' `
    -Direction Inbound -Protocol TCP -LocalPort 22 -Action Allow `
    -Profile Any -ErrorAction SilentlyContinue | Out-Null

if (-not (Get-LocalUser -Name 'pf-test' -ErrorAction SilentlyContinue)) {
    Write-Host ""
    Write-Host "WARNING: the pf-test user does not exist on this VM. Run"
    Write-Host "bootstrap-user-vm.ps1 first - the SSH tests reuse that account."
}

Write-Host ""
Write-Host "SSH enabled on port 22. Connection vars were printed by"
Write-Host "bootstrap-user-vm.ps1; reuse the pf-test password."
