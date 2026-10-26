# Manage your first Windows target

This tutorial builds its own project, so you do not need to finish the
[Quickstart](./quickstart.md) first. You add a Windows host to an inventory, store its password as an encrypted secret, test
the connection, and apply a small playbook to the host. The password never
appears in plaintext YAML.

## Before you start

You need:

- A controller with `preflight` installed. The controller can run Windows,
  macOS, or Linux.
- A Windows host that the controller can reach over SSH
- A local or domain account that can sign in to that host
- Administrator access on the Windows host

If SSH is not enabled yet, follow
[Enable remote access on a Windows target](../how-to/enable-remote-access.md)
with the default SSH setup, then return here.

The tutorial uses password authentication so you can see the secrets workflow.
You can switch the host to an SSH key later.

## 1. Create the project

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

The command prints a public recipient beginning with `age1`. Copy it. Then add
`.age/` to the project's `.gitignore` so the private identity stays out of Git:

```text
.age/
```

The recipient is public. The identity in `.age/keys.txt` is private: give it
only to people and machines that should decrypt this project's secrets.

## 2. Configure the host and secret

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
to `secrets/windows-password.age`. The plaintext never reaches `preflight.yml`
or your shell history.

## 3. Test the connection

Gather facts from the host:

```bash
preflight facts kiosk-01
```

Facts come back only when the address, username, and password work and the
host provides Windows PowerShell over SSH.

If the connection fails, stop and work through
[Troubleshoot remote connections](../how-to/troubleshoot-remote-connections.md)
before continuing.

## 4. Write a Windows playbook

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

## 5. Inspect and check the run

Validate the YAML and inspect the expanded task list:

```bash
preflight validate playbooks/hello-windows.yml
preflight plan playbooks/hello-windows.yml --target kiosk-01
```

Neither command changes the host. Now run the module checks against it:

```bash
preflight check playbooks/hello-windows.yml --target kiosk-01
```

`check` connects to the host and reports the directory and file as pending
changes without creating them.

## 6. Apply the playbook

```bash
preflight apply playbooks/hello-windows.yml --target kiosk-01
```

On the Windows host, open PowerShell and read the file:

```powershell
Get-Content C:\PreflightTutorial\hello.txt
```

You should see:

```text
Hello from Preflight.
```

## 7. Confirm idempotency

Run the check again:

```bash
preflight check playbooks/hello-windows.yml --target kiosk-01
```

Preflight reports no changes. The directory and file already match the
playbook.

## Next steps

- [Run a playbook against remote hosts](../how-to/remote-execution.md) covers
  groups, selectors, and concurrency.
- [Manage secrets](../how-to/manage-secrets.md) covers more recipients,
  editing, and rekeying.
- The [built-in module reference](../reference/modules.md) lists modules for
  services, packages, users, and other Windows state.
