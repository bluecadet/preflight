# Enable remote access on a Windows target

Turn on SSH or WinRM on a Windows machine so Preflight can manage it from a controller. This guide does not create user accounts. To set up a disposable VM for the integration tests, see [Run the integration test suite](../development/winrm-integration-testing.md) instead.

## Prerequisites

- Administrator access on the target
- An existing local or domain account for Preflight to log in as
- Network access from the controller to the target on port 22 (SSH) or 5985 (WinRM)

## 1. Choose a transport

Use SSH unless you have a reason not to. SSH encrypts traffic by default. The setup script configures WinRM over HTTP with Basic auth, which sends credentials and output unencrypted; use it only on a trusted network.

## 2. Run the setup script

On the target, open PowerShell as Administrator and run:

```powershell
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass -Force
iex ((New-Object System.Net.WebClient).DownloadString(
  'https://raw.githubusercontent.com/bluecadet/preflight/main/scripts/setup-preflight-access.ps1'
))
```

The script enables SSH only. Pass `-Transport WinRM` or `-Transport Both` to change that:

```powershell
iex "& { $((New-Object System.Net.WebClient).DownloadString('https://raw.githubusercontent.com/bluecadet/preflight/main/scripts/setup-preflight-access.ps1')) } -Transport Both"
```

With a local copy of the script, run `.\setup-preflight-access.ps1 -Transport Both`.

For SSH, the script installs the OpenSSH Server capability if missing, sets `sshd` to start automatically, starts it, and opens TCP port 22. For WinRM, it runs `winrm quickconfig`, enables Basic auth and unencrypted traffic, and opens TCP port 5985.

To encrypt WinRM, configure an HTTPS listener with a certificate the controller trusts, then set `https: true` on the host. The port defaults to 5986.

## 3. Add the host to `preflight.yml`

The script prints an inventory snippet for each transport it enabled. Fill in the address and an existing account:

```yaml
inventory:
  hosts:
    - name: my-workstation
      address: 192.168.1.50
      transport: ssh          # or winrm
      username: preflight-svc
      password: secret:my-workstation-password
```

SSH hosts can use `private_key` instead of `password`. See the [Inventory reference](../reference/inventory.md) for every field.

## 4. Verify

From the controller:

```bash
preflight facts my-workstation
```

Gathered facts confirm that authentication and remote execution work. For connection errors or `401` responses, see [Troubleshoot remote connections](./troubleshoot-remote-connections.md).

## Related docs

- [Manage your first Windows target](../tutorials/first-windows-target.md)
- [Run a playbook against remote hosts](./remote-execution.md)
- [Targets, transports, and plugins](../explanation/targets-and-transports.md)
