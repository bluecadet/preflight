# Troubleshoot Remote Connections

Use this guide when a host you added to `preflight.yml` won't connect, or
when you want to validate a new host over SSH or WinRM before you commit it
to your inventory. The controller running `preflight` can be macOS, Linux,
or Windows; the walkthrough below shows the equivalent command for each.

## Prerequisites

- A host you want to validate, reachable from the controller
- Credentials for the host: an SSH key or password, or a WinRM username and
  password
- A local `preflight` binary on the controller

If you already know you will store the password or key as a secret
reference, you can still use a temporary inline value in a scratch
`preflight.yml` for this validation flow.

## 1. Probe Reachability

Before involving `preflight`, confirm that something is listening on the
port you expect.

For SSH (default port `22`):

```bash
nc -vz 192.168.1.50 22
```

```powershell
Test-NetConnection -ComputerName 192.168.1.50 -Port 22
```

For WinRM, test both the plain and TLS ports if you are not sure which one
the host uses:

```bash
nc -vz 192.168.1.50 5985
nc -vz 192.168.1.50 5986
```

```powershell
Test-NetConnection -ComputerName 192.168.1.50 -Port 5985
Test-NetConnection -ComputerName 192.168.1.50 -Port 5986
```

A successful result only proves that a listener exists on the port. It does
not prove the service is SSH or WinRM, or that authentication will succeed.

For WinRM specifically, confirm the listener is really a WSMan endpoint
before going further:

```bash
curl -i --max-time 5 http://192.168.1.50:5985/wsman
```

A real WinRM HTTP listener commonly responds like this:

```text
HTTP/1.1 405
Allow: POST
Server: Microsoft-HTTPAPI/2.0
```

That response is a good sign: `/wsman` exists, the host is speaking the
expected Windows HTTP API, and it rejects `GET` because WinRM expects
`POST`. An HTML page, a proxy page, or any other non-WSMan response means
you reached the wrong service even though the port is open.

## 2. Validate With A Temporary Project

Network reachability is not enough to prove that `preflight` can
authenticate and run tasks. Create a scratch directory with its own
`preflight.yml` so you can iterate without touching your real inventory.

For SSH:

```yaml
inventory:
  hosts:
    - name: exhibit-pc
      address: 192.168.1.50
      transport: ssh
      username: preflight-test
      password: "TempPreflight123!"
```

For WinRM:

```yaml
inventory:
  hosts:
    - name: exhibit-pc
      address: 192.168.1.50
      transport: winrm
      username: preflight-test
      password: "TempPreflight123!"
```

If the WinRM host uses HTTPS, add these fields:

```yaml
      https: true
      port: 5986
```

## 3. Confirm With preflight facts

From the scratch directory, run:

```bash
preflight facts exhibit-pc --output json
```

Use `facts` here rather than another command:

- `preflight inventory list` only validates inventory parsing and selector
  resolution; it never contacts the host.
- `preflight plan` resolves the playbook but does not contact the host
  either.
- `preflight facts` is the first command that proves both authentication
  and remote execution against the host.

Once the scratch inventory succeeds, copy the same working values into your
project `preflight.yml` and replace the inline password or key with a
secret reference. See [Manage secrets](./manage-secrets.md) for the secret
CLI procedures.

## WinRM Symptoms

| Symptom | Likely cause / fix |
|---|---|
| The port answers, but `preflight facts` returns `401` | The host is reachable but the WinRM auth settings and credentials don't match. On the target, in an elevated PowerShell session, inspect `winrm get winrm/config/service/auth` and confirm Basic auth is enabled (`winrm set winrm/config/service/Auth '@{Basic="true"}'`) and, for a plain HTTP listener, that unencrypted traffic is allowed (`winrm set winrm/config/service '@{AllowUnencrypted="true"}'`). Also confirm whether the account is local or domain-backed, and that the username matches exactly. |
| `curl`/`Invoke-WebRequest` against `/wsman` returns `405` with `Allow: POST` | Positive signal — you reached a real WSMan endpoint. Move on to `preflight facts`. |
| `preflight plan` succeeds but `preflight facts` fails | Expected for auth problems: `plan` is a pure planning step and never contacts the target. |
| A Microsoft-backed, domain-backed, or renamed administrator account keeps failing | Create a dedicated local account for validation instead (`New-LocalUser`, then `Add-LocalGroupMember -Group Administrators`). Local username/password auth over Basic is the simplest path the current WinRM transport supports. |
| A WinRM task fails with a symbolic-link error or HRESULT `0x80073D19` | This is a WinRM session limitation, not a module bug. See [WinRM session limitations](../reference/modules.md#winrm-session-limitations). |

## SSH Symptoms

| Symptom | Likely cause / fix |
|---|---|
| Host key verification fails after the target was reimaged | The default `accept-new` policy refuses to silently trust a changed key, since that can indicate a MITM attack. If the change is expected, remove the stale line for that host from `known_hosts_file` and reconnect. See [SSH host-key verification](../explanation/targets-and-transports.md#ssh-host-key-verification) for the full policy semantics. |
| `Too many authentication failures` | The SSH server closed the connection after too many offered credentials, often because an agent is offering keys ahead of the one you intend to use. Set `private_key` explicitly on the host entry, or unset `SSH_AUTH_SOCK` for the run so Preflight doesn't fall through to the agent. |
| An encrypted `private_key` fails immediately instead of falling back to another auth method | Expected behavior: an encrypted key without `private_key_passphrase` set fails with a clear error rather than silently trying other methods. Set `private_key_passphrase` alongside `private_key`. |
| Auth fails with an agent connection error, even though another auth method is configured | The `SSH_AUTH_SOCK` agent socket is dead but was the only usable candidate. Check `ssh-add -l` on the controller, or set `private_key`/`password` explicitly so Preflight doesn't depend on the agent. |
| A `strict` `host_key_policy` host fails on first connect | `strict` never trusts a host on first use. Pre-seed the file with `ssh-keyscan -H <host> >> <known_hosts_file>`, or connect once with `accept-new` to pin the key, before switching the host to `strict`. See [SSH host-key verification](../explanation/targets-and-transports.md#ssh-host-key-verification). |
| The connection succeeds, but a task fails during execution | If `password` or `private_key` is a secret reference, confirm the controller can decrypt it through the project's `age` identity. For a privilege-escalation failure, see the reason codes in the [error reference](../reference/errors.md#reason-codes). For a module or runtime support failure, see the [built-in module reference](../reference/modules.md). |

## Related Docs

- [Enable remote access on a Windows target](./enable-remote-access.md)
- [Run a playbook against remote hosts](./remote-execution.md)
- [Inventory reference](../reference/inventory.md)
- [Targets, transports, and plugins](../explanation/targets-and-transports.md)
- [Error reference](../reference/errors.md)
