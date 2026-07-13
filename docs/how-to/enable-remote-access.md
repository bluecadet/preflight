# Enable Remote Access On A Windows Target

Use this guide when you have a real Windows machine — a workstation, kiosk,
or server you manage day to day — and want Preflight to be able to reach it
remotely. It walks through the transport switches only; it does not create or
manage user accounts.

> [!NOTE]
> Setting up a disposable test VM for Preflight's own integration test suite
> is a different task with its own scripts and guide — see
> [Run the integration test suite](./winrm-integration-testing.md). This
> guide is for a target you actually intend to manage.

## Prerequisites

- Administrator access on the target Windows machine
- An existing local or domain account you intend to use in `preflight.yml` —
  this guide does not create one for you
- Network connectivity from wherever you run `preflight` to the target, on
  port 22 (SSH) and/or 5985 (WinRM)

## 1. Choose A Transport

Preflight can reach Windows hosts over SSH or WinRM. If you are unsure,
prefer SSH:

- SSH is the default transport and is encrypted out of the box.
- WinRM, as configured by this script, uses HTTP with Basic auth, which
  sends credentials unencrypted. It is only appropriate on a trusted or
  internal network (see the security note in step 3).

## 2. Run The Setup Script

On the target machine, open **PowerShell as Administrator** and run:

```powershell
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass -Force
iex ((New-Object System.Net.WebClient).DownloadString(
  'https://raw.githubusercontent.com/bluecadet/preflight/main/scripts/setup-preflight-access.ps1'
))
```

By default this enables **SSH only**. Pass `-Transport` to change that:

```powershell
# Enable WinRM only
iex "& { $((New-Object System.Net.WebClient).DownloadString('https://raw.githubusercontent.com/bluecadet/preflight/main/scripts/setup-preflight-access.ps1')) } -Transport WinRM"

# Enable both
iex "& { $((New-Object System.Net.WebClient).DownloadString('https://raw.githubusercontent.com/bluecadet/preflight/main/scripts/setup-preflight-access.ps1')) } -Transport Both"
```

If you already have the repository checked out, or copy the script to the
target first, you can call it directly instead:

```powershell
.\setup-preflight-access.ps1 -Transport Both
```

What the script does, per transport:

- **SSH**: installs the OpenSSH Server capability if missing, starts and
  enables `sshd`, and opens the firewall for port 22.
- **WinRM**: runs `winrm quickconfig`, enables Basic auth over HTTP, and
  opens the firewall for port 5985.

It does not touch user accounts, groups, or any application state.

## 3. Read The Security Note

If you enabled WinRM, the script prints a warning: HTTP with Basic auth
sends credentials and command output unencrypted. Only run WinRM this way on
a trusted or internal network. For an encrypted setup, configure a WinRM
HTTPS listener yourself, then set `https: true` and `port: 5986` on the host
in `preflight.yml` — see
[Validate a WinRM connection from macOS](./validate-winrm-from-macos.md) for
the listener commands. Where possible, prefer SSH instead.

## 4. Add The Host To `preflight.yml`

The script prints a ready-to-edit inventory snippet for the transport(s) you
enabled. For SSH:

```yaml
inventory:
  hosts:
    - name: my-workstation
      address: 192.168.1.50
      transport: ssh
      username: preflight-svc
      password: secret:my-workstation-password
```

For WinRM:

```yaml
inventory:
  hosts:
    - name: my-workstation
      address: 192.168.1.50
      transport: winrm
      username: preflight-svc
      password: secret:my-workstation-password
```

Replace `username` and the secret reference with the account you already
have on the target. See the [inventory reference](../reference/inventory.md)
for every available field.

## 5. Confirm It Works

From wherever you run `preflight`:

```bash
preflight facts my-workstation --output json
```

A successful response confirms both authentication and remote execution. If
you get a connection error or `401`, see
[Validate a WinRM connection from macOS](./validate-winrm-from-macos.md) —
its troubleshooting steps apply to both transports.

## Troubleshooting

| Symptom | Likely cause |
|---------|--------------|
| `connection refused` | The transport was never enabled, or the wrong port/address |
| `401 Unauthorized` (WinRM) | Basic auth not enabled, or the wrong username/password |
| SSH prompts for a host key you don't recognize | Expected on first connect; see [SSH host-key verification](../explanation/targets-and-transports.md#ssh-host-key-verification) |
| Script requires elevation | Re-open PowerShell with "Run as Administrator" |

## Related Docs

- [Validate a WinRM connection from macOS](./validate-winrm-from-macos.md)
- [Install Preflight](./install-preflight.md)
- [Run a playbook against remote hosts](./remote-execution.md)
- [Targets, transports, and plugins](../explanation/targets-and-transports.md)
- [Inventory reference](../reference/inventory.md)
