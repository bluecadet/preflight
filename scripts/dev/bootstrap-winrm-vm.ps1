<#
.SYNOPSIS
    Enable the WinRM transport on a Preflight integration test VM.
.DESCRIPTION
    Enables WinRM over HTTP with Basic authentication on port 5985, adds the
    pf-test account to Remote Management Users, and opens the firewall.

    This script only enables the WinRM transport. It does NOT create the
    pf-test user or write the sacrificial sentinel - bootstrap-user-vm.ps1 owns
    those. Run that first (it also prints the .env.test connection vars).
.LINK
    https://github.com/bluecadet/preflight/blob/main/docs/development/winrm-integration-testing.md
#>

$ErrorActionPreference = 'Stop'

Write-Host "==> 1. Enabling WinRM over HTTP with Basic auth on port 5985"
winrm quickconfig -q -force
winrm set winrm/config/service/Auth "@{Basic=`"true`"}"
winrm set winrm/config/service "@{AllowUnencrypted=`"true`"}"

Write-Host "==> 2. Adding pf-test to Remote Management Users"
if (Get-LocalUser -Name 'pf-test' -ErrorAction SilentlyContinue) {
    Add-LocalGroupMember -Group 'Remote Management Users' -Member 'pf-test' -ErrorAction SilentlyContinue
} else {
    Write-Host ""
    Write-Host "WARNING: the pf-test user does not exist on this VM. Run"
    Write-Host "bootstrap-user-vm.ps1 first - the WinRM tests reuse that account."
}

Write-Host "==> 3. Opening firewall rule for WinRM HTTP"
New-NetFirewallRule -DisplayName 'Preflight WinRM HTTP' `
    -Direction Inbound -Protocol TCP -LocalPort 5985 -Action Allow `
    -Profile Any -ErrorAction SilentlyContinue | Out-Null

Write-Host ""
Write-Host "WinRM enabled on port 5985. Connection vars were printed by"
Write-Host "bootstrap-user-vm.ps1; reuse the pf-test password."
