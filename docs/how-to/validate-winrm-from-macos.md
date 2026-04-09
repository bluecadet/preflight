# Validate A WinRM Connection From macOS

Use this guide when you are on a Mac and want to confirm that a Windows host is reachable over WinRM before you commit it to your real `inventory.yml`.

This flow answers three separate questions:

1. Is there a listener on the network path you expect?
2. Is that listener actually a WinRM endpoint?
3. Can Preflight authenticate and run PowerShell on the target?

## Prerequisites

- A Windows host on the same network, or otherwise reachable from your Mac
- A username and password for the Windows host
- A local `preflight` binary on your Mac

If you already know you will store the password as a secret reference, you can still use a temporary inline password in a scratch inventory file for this validation flow.

## 1. Check That The Port Is Reachable

WinRM usually listens on one of these ports:

- `5985` for HTTP
- `5986` for HTTPS

From your Mac, test both if you are not sure which one the target uses:

```bash
nc -vz 192.168.1.50 5985
nc -vz 192.168.1.50 5986
```

This only proves that something is listening on the port. It does not prove that the service is WinRM or that authentication will work.

## 2. Confirm That The Listener Is Really WinRM

If `5985` is open, check the WSMan endpoint directly:

```bash
curl -i --max-time 5 http://192.168.1.50:5985/wsman
```

A real WinRM HTTP listener commonly responds like this:

```text
HTTP/1.1 405
Allow: POST
Server: Microsoft-HTTPAPI/2.0
```

That response is a good sign. It means:

- `/wsman` exists
- the host is speaking the expected Windows HTTP API
- the endpoint is rejecting `GET` because WinRM expects `POST`

If you get an HTML page, a proxy page, or some other non-WSMan response, you have reached the wrong service even if the port is open.

## 3. Prove That Preflight Can Authenticate

Network reachability is not enough. The first Preflight command that proves both authentication and remote PowerShell execution is `preflight facts`.

Create a temporary inventory file such as `/tmp/winrm-test.yml`:

```yaml
groups:
  test:
    hosts:
      - name: exhibit-pc
        address: 192.168.1.50
        transport: winrm
        username: preflight-test
        password: "TempPreflight123!"
```

Then run:

```bash
preflight facts exhibit-pc --inventory /tmp/winrm-test.yml --output json
```

If the target uses WinRM over HTTPS, add these fields:

```yaml
        https: true
        port: 5986
```

Why use `facts` here:

- `inventory list` only validates inventory parsing and selector resolution
- `plan` does not contact the target
- `facts` proves that Preflight can authenticate and execute remote PowerShell

## 4. If You Get `401`, Check WinRM Auth On The Windows Host

On the Windows machine, in an elevated PowerShell session, inspect the WinRM service:

```powershell
winrm get winrm/config/service
winrm get winrm/config/service/auth
winrm enumerate winrm/config/listener
```

For the current Preflight WinRM implementation, the simplest successful path is:

- a local Windows account
- username and password authentication
- an HTTP listener on `5985` or an HTTPS listener on `5986`

For an HTTP listener on `5985`, the remote host usually needs:

```powershell
winrm set winrm/config/service/Auth '@{Basic="true"}'
winrm set winrm/config/service '@{AllowUnencrypted="true"}'
```

On trusted internal networks, that is often the fastest way to validate a new setup. On less trusted networks, prefer WinRM over HTTPS.

## 5. Prefer A Dedicated Local Account For Validation

If a Microsoft-backed sign-in, domain-backed sign-in, or ambiguous administrator name keeps failing, create a dedicated local account for WinRM validation.

Example in elevated PowerShell:

```powershell
$pw = ConvertTo-SecureString 'TempPreflight123!' -AsPlainText -Force
New-LocalUser -Name 'preflight-test' -Password $pw -PasswordNeverExpires
Add-LocalGroupMember -Group 'Administrators' -Member 'preflight-test'
```

This avoids several common sources of confusion:

- Microsoft-backed consumer accounts may not map cleanly to the username format expected by Basic-auth WinRM
- domain accounts usually imply Kerberos or NTLM expectations rather than simple local username and password auth
- built-in administrator accounts may have been renamed or disabled

If you want to inspect the currently signed-in identity, these commands are useful:

```powershell
whoami
$env:USERNAME
$env:COMPUTERNAME
Get-LocalUser | Select-Object Name, Enabled
```

## 6. Move The Working Settings Into Your Real Inventory

Once the scratch inventory succeeds, copy the same working values into your project `inventory.yml` and replace the inline password with a secret reference if appropriate.

Example:

```yaml
groups:
  lobby:
    hosts:
      - name: lobby-pc-01
        address: 192.168.1.50
        transport: winrm
        username: preflight-test
        password: secret:winrm-password
```

## Troubleshooting

### `nc` succeeds but `preflight facts` returns `401`

That usually means the endpoint is reachable but the WinRM auth settings and the credentials do not match. Check:

- `Basic`
- `AllowUnencrypted`
- whether the account is local or domain-backed
- whether the username is the exact account name you intend to use

### `curl` returns `405 Allow: POST`

That is a positive signal. You have reached a real WSMan endpoint. Move on to `preflight facts`.

### `plan` works but `facts` fails

That is expected for auth problems. `plan` is a pure planning step and does not contact the target.

## Related Docs

- [Run a playbook against remote hosts](./remote-execution.md)
- [Inventory reference](../reference/inventory.md)
- [Targets, transports, and plugins](../explanation/targets-and-transports.md)
