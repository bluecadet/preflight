# Manage Your First Windows Target

This tutorial picks up where the [Quickstart](./quickstart.md) ends. You will
add one Windows host to an inventory, store its password as an encrypted
secret, test the connection, and apply a small playbook to it.

By the end, you will have managed a real Windows target without putting its
password in plaintext YAML.

## Before You Start

You need:

- A controller with `preflight` installed
- A Windows host that the controller can reach over SSH
- A local or domain account that can sign in to that host
- Administrator access on the Windows host

If SSH is not enabled yet, follow
[Enable remote access on a Windows target](../how-to/enable-remote-access.md)
and choose the default SSH setup. Return here once the setup script finishes.

This tutorial uses password authentication to make the full secrets workflow
visible. For long-lived environments, you can switch the inventory entry to
an SSH key later.

## 1. Create The Project

Create a project directory on the controller:

```bash
mkdir preflight-windows-tutorial
cd preflight-windows-tutorial
mkdir playbooks
```

Generate an `age` identity for the project:

```bash
preflight secret identity generate --out .age/keys.txt
```

The command prints a public recipient beginning with `age1`. Copy that value.
Add `.age/` to the project's `.gitignore` so the private identity cannot be
committed:

```text
.age/
```

The recipient is public. The identity in `.age/keys.txt` is private and should
only be available to people or systems allowed to decrypt this project's
secrets.

## 2. Configure The Host And Secret

Create `preflight.yml`. Replace the recipient, address, and username with your
own values:

```yaml
project: windows-tutorial
environment: development

secrets:
  identity: ".age/keys.txt"
  recipients:
    - "age1replace-with-your-recipient"
  entries:
    windows-password:
      file: "secrets/windows-password.age"

inventory:
  hosts:
    - name: kiosk-01
      address: 192.168.1.50
      transport: ssh
      username: exhibit-admin
      password: secret:windows-password
```

Encrypt the Windows account password:

```bash
preflight secret encrypt windows-password
```

Enter the password twice when prompted. Preflight writes the encrypted value
to `secrets/windows-password.age`; the plaintext does not go into
`preflight.yml` or your shell history.

## 3. Test The Connection

Ask Preflight to gather facts from the host:

```bash
preflight facts kiosk-01
```

A successful response confirms that the address, SSH transport, username, and
password all work together. It also confirms that Preflight detected the
Windows PowerShell runtime on the host.

If the connection fails, stop here and use
[Troubleshoot remote connections](../how-to/troubleshoot-remote-connections.md).
A playbook cannot fix a transport that cannot connect.

## 4. Write A Windows Playbook

Create `playbooks/hello-windows.yml`:

```yaml
name: hello-windows
description: Write a managed file on a Windows target

tasks:
  - name: Create the tutorial directory
    directory:
      path: 'C:\PreflightTutorial'
      ensure: present

  - name: Write the tutorial file
    file:
      dest: 'C:\PreflightTutorial\hello.txt'
      content: |
        Hello from Preflight.
      ensure: present
```

Both modules work on Windows and report whether their desired state already
exists before making a change.

## 5. Inspect And Check The Run

Validate the YAML and inspect the expanded task list:

```bash
preflight validate playbooks/hello-windows.yml
preflight plan playbooks/hello-windows.yml --target kiosk-01
```

Neither command changes the host. Now run the real module checks against it:

```bash
preflight check playbooks/hello-windows.yml --target kiosk-01
```

`check` connects to the host and reports the directory and file as changes it
would make, but it does not create them.

## 6. Apply The Playbook

Apply the desired state:

```bash
preflight apply playbooks/hello-windows.yml --target kiosk-01
```

On the Windows host, open PowerShell and confirm the file content:

```powershell
Get-Content C:\PreflightTutorial\hello.txt
```

You should see:

```text
Hello from Preflight.
```

## 7. Confirm Idempotency

Run the check again:

```bash
preflight check playbooks/hello-windows.yml --target kiosk-01
```

This time Preflight should report no changes. The directory and file already
match the playbook, so there is nothing to apply.

## What You Learned

You now know the normal path for managing a Windows host:

1. Define the host and its transport in inventory.
2. Keep credentials in an encrypted secret instead of plaintext YAML.
3. Use `facts` as the cheapest connection test.
4. Validate, plan, and check before applying a playbook.
5. Run the playbook again to confirm the result is stable.

## Next Steps

- [Run a playbook against remote hosts](../how-to/remote-execution.md) when you
  need groups, selectors, or concurrency.
- [Manage secrets](../how-to/manage-secrets.md) when you need more recipients,
  secret files, editing, or rekeying.
- [Built-in module reference](../reference/modules.md) when you are ready to
  configure services, packages, users, or other Windows state.
