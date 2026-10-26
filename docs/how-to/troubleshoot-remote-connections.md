# Troubleshoot remote connections

Diagnose a host that will not connect over SSH or WinRM, or test a new host before adding it to your inventory.

## Prerequisites

- `preflight` installed on the controller
- The host's address and credentials: an SSH key or password, or a WinRM username and password

## 1. Probe the port

Check that something listens on the expected port: `22` for SSH, `5985` for WinRM over HTTP, `5986` for WinRM over HTTPS.

```bash
nc -vz 192.168.1.50 22
```

```powershell
Test-NetConnection -ComputerName 192.168.1.50 -Port 22
```

An open port proves only that a listener exists. For WinRM, confirm the listener is a WS-Management endpoint:

```bash
curl -i --max-time 5 http://192.168.1.50:5985/wsman
```

A WinRM listener rejects the `GET` with:

```text
HTTP/1.1 405
Allow: POST
Server: Microsoft-HTTPAPI/2.0
```

Any other response means a different service owns the port.

## 2. Create a scratch project

Create an empty directory with a `preflight.yml` that holds only the host under test, so you can iterate without touching your real inventory:

```yaml
inventory:
  hosts:
    - name: exhibit-pc
      address: 192.168.1.50
      transport: ssh          # or winrm
      username: preflight-test
      password: "TempPreflight123!"
```

For WinRM over HTTPS, add `https: true`. The port then defaults to `5986`.

## 3. Run facts

From the scratch directory:

```bash
preflight facts exhibit-pc
```

`facts` is the first command that authenticates and runs code on the host. `inventory list` and `plan` never connect, so they succeed even when the host is unreachable.

When `facts` succeeds, copy the working values into your project's `preflight.yml` and replace the inline password with a secret reference ([Manage secrets](./manage-secrets.md)).

## WinRM symptoms

| Symptom | Fix |
|---|---|
| The port answers, but `facts` returns `401` | Preflight's WinRM transport uses Basic auth. In an elevated PowerShell on the target, run `winrm get winrm/config/service/auth` and check that `Basic` is `true`. For an HTTP listener, also check that `AllowUnencrypted` is `true` under `winrm get winrm/config/service`. Then check the username and password. |
| A domain or Microsoft account fails while a local account works | WinRM Basic auth accepts local accounts only. Create a local account in the Administrators group (`New-LocalUser`, then `Add-LocalGroupMember -Group Administrators`). |
| A task fails with a symbolic-link error or HRESULT `0x80073D19` | The WinRM session cannot run that operation. See [WinRM session limitations](../reference/modules.md#winrm-session-limitations). |

## SSH symptoms

| Symptom | Fix |
|---|---|
| Host key verification fails after the target was reimaged | The default `accept-new` policy rejects a changed key. If you expect the change, delete the host's line from the known_hosts file and reconnect. See [SSH host-key verification](../explanation/targets-and-transports.md#ssh-host-key-verification). |
| A `strict` host fails on first connect | `strict` never trusts an unknown host. Add the key with `ssh-keyscan -H <host> >> <known_hosts_file>`, or connect once with `accept-new` before switching to `strict`. |
| `Too many authentication failures` | An SSH agent is offering other keys first. Set `private_key` on the host, or unset `SSH_AUTH_SOCK` for the run. |
| `private key is encrypted: set private_key_passphrase` | Set `private_key_passphrase` next to `private_key`. |
| An SSH agent connection error, with no other credentials configured | The `SSH_AUTH_SOCK` socket is dead. Check `ssh-add -l`, or set `private_key` or `password` on the host. |
| The connection succeeds, but decrypting a secret reference fails | The controller cannot decrypt the secret. Check the project's `age` identity ([Manage secrets](./manage-secrets.md)). |

For failure reason codes, see the [Error reference](../reference/errors.md).

## Related docs

- [Enable remote access on a Windows target](./enable-remote-access.md)
- [Run a playbook against remote hosts](./remote-execution.md)
- [Inventory reference](../reference/inventory.md)
- [Targets, transports, and plugins](../explanation/targets-and-transports.md)
