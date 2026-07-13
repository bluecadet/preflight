<#
.SYNOPSIS
    Enable Preflight remote access (SSH and/or WinRM) on this Windows host.
.DESCRIPTION
    Flips the OS-level transport switches Preflight needs to manage this
    machine remotely. It does not create or manage a user account - point
    your preflight.yml at an existing local or domain account.

    Defaults to enabling SSH only. WinRM requires explicit opt-in via
    -Transport WinRM or -Transport Both: Preflight's WinRM setup here uses
    HTTP with Basic auth, which sends credentials unencrypted, so SSH is the
    safer default. See the security warning printed after the script runs.

    Run this from an elevated ("Run as Administrator") PowerShell session.
.PARAMETER Transport
    Which transport(s) to enable: SSH (default), WinRM, or Both.
.EXAMPLE
    .\setup-preflight-access.ps1
    Enables SSH only.
.EXAMPLE
    .\setup-preflight-access.ps1 -Transport Both
    Enables both SSH and WinRM.
.LINK
    https://github.com/bluecadet/preflight/docs/how-to/enable-remote-access.md
#>

[CmdletBinding()]
param(
    [ValidateSet('SSH', 'WinRM', 'Both')]
    [string]$Transport = 'SSH'
)

$ErrorActionPreference = 'Stop'

$enableSSH = $Transport -eq 'SSH' -or $Transport -eq 'Both'
$enableWinRM = $Transport -eq 'WinRM' -or $Transport -eq 'Both'

if ($enableSSH) {
    Write-Host "==> Enabling SSH (OpenSSH Server) on port 22"
    $cap = Get-WindowsCapability -Online -Name 'OpenSSH.Server*'
    if ($cap.State -ne 'Installed') {
        Add-WindowsCapability -Online -Name $cap.Name | Out-Null
    } else {
        Write-Host "    OpenSSH Server already installed"
    }
    Set-Service -Name sshd -StartupType Automatic
    Start-Service sshd
    New-NetFirewallRule -DisplayName 'Preflight SSH' `
        -Direction Inbound -Protocol TCP -LocalPort 22 -Action Allow `
        -Profile Any -ErrorAction SilentlyContinue | Out-Null
    Write-Host "    SSH enabled"
}

if ($enableWinRM) {
    Write-Host "==> Enabling WinRM over HTTP with Basic auth on port 5985"
    winrm quickconfig -q -force
    winrm set winrm/config/service/Auth "@{Basic=`"true`"}"
    winrm set winrm/config/service "@{AllowUnencrypted=`"true`"}"
    New-NetFirewallRule -DisplayName 'Preflight WinRM HTTP' `
        -Direction Inbound -Protocol TCP -LocalPort 5985 -Action Allow `
        -Profile Any -ErrorAction SilentlyContinue | Out-Null
    Write-Host "    WinRM enabled"
}

Write-Host ""
Write-Host "==============================================================="
Write-Host " Preflight remote access enabled."
Write-Host "==============================================================="
Write-Host ""
Write-Host "This script did not create or change any user account. Point"
Write-Host "preflight.yml at an existing local or domain account able to"
Write-Host "log in on this host, for example:"
Write-Host ""
if ($enableSSH) {
    Write-Host "    inventory:"
    Write-Host "      hosts:"
    Write-Host "        - name: <host-name>"
    Write-Host "          address: <this-host-ip-or-hostname>"
    Write-Host "          transport: ssh"
    Write-Host "          username: <existing-username>"
    Write-Host "          password: secret:<name>   # or private_key"
    Write-Host ""
}
if ($enableWinRM) {
    Write-Host "    inventory:"
    Write-Host "      hosts:"
    Write-Host "        - name: <host-name>"
    Write-Host "          address: <this-host-ip-or-hostname>"
    Write-Host "          transport: winrm"
    Write-Host "          username: <existing-username>"
    Write-Host "          password: secret:<name>"
    Write-Host ""
}

if ($enableWinRM) {
    Write-Host "==============================================================="
    Write-Host " SECURITY WARNING"
    Write-Host " WinRM is now listening over HTTP with Basic auth: credentials"
    Write-Host " and traffic are NOT encrypted. Only expose this on a trusted"
    Write-Host " or internal network. For an encrypted setup, configure WinRM"
    Write-Host " over HTTPS (see the link below) and set https: true on the"
    Write-Host " host in preflight.yml. Prefer SSH where possible; it is the"
    Write-Host " default transport and encrypted out of the box."
    Write-Host "==============================================================="
    Write-Host ""
}

Write-Host "Full guide: docs/how-to/enable-remote-access.md"
