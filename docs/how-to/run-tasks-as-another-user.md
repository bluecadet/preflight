# Run Tasks As Another User

Use this guide when you need tasks to execute under a specific user identity — for example, configuring a kiosk account's profile, writing to a user's `APPDATA`, or running a command that checks user-scoped registry values.

## Prerequisites

- A working playbook and target connection
- The account used for your transport (WinRM or SSH) must have permission to create local users and to execute commands as other users
- For Windows `become`: the target user must have a password

## Overview

The typical pattern has two phases:

1. Create the user account using the `user` module, running as your transport account.
2. Add `become` to tasks that must run in that user's context.

## 1. Store The Password As A Secret

Avoid hardcoding passwords. Use a named secret reference instead:

```bash
preflight secret encrypt exhibit-password
```

See [Manage secrets](./manage-secrets.md) if you have not set up `age` secrets yet.

## 2. Provision The User Account

Run a task as your normal transport account to create the user:

```yaml
tasks:
  - name: Create exhibit user
    user:
      name: exhibit
      password: secret:exhibit-password
      groups:
        - Users
      ensure: present
```

This task runs as the account your WinRM or SSH transport authenticates with — it does not need `become`.

## 3. Configure Autologin (Optional)

If this is a kiosk that should boot directly into the exhibit account:

```yaml
  - name: Configure autologin
    uses: preflight/autologin
    with:
      username: exhibit
      password: secret:exhibit-password
```

## 4. Run Tasks As The New User

Add a `become` block to any task that must execute under the exhibit account's identity. On Windows, `become` uses `runas` by default:

```yaml
  - name: Set user environment variable
    become:
      user: exhibit
      password: secret:exhibit-password
    environment:
      name: EXHIBIT_MODE
      value: "kiosk"
      scope: user
      ensure: present

  - name: Write user-scoped registry value
    become:
      user: exhibit
      password: secret:exhibit-password
    registry:
      path: 'HKCU:\Software\ExhibitApp'
      values:
        StartFullscreen: "1"
      ensure: present
```

`become` is task execution metadata — it does not change the module's `params`, only the identity the task runs under.

This is also the recommended way to apply current-user stdlib actions to a kiosk or exhibit account when the action does not expose a `user` input, or when you need every task in the action to run under that Windows identity.

For `preflight/windows-shell`, `preflight/windows-input`, `preflight/windows-power`, and `preflight/debloat`, you can set `with.user` when you only need the action's supported user-scoped registry settings and do not want to switch the task process identity. The target user's profile hive must already be loaded, such as while that user is signed in or by running with `become.load_profile`.

```yaml
  - name: Configure supported shell defaults for exhibit user
    uses: preflight/windows-shell
    with:
      user: exhibit
      theme_mode: dark
      taskbar_auto_hide: true
```

Some shell-facing changes persist immediately in the user's profile but only become visible after sign-out, Explorer restart, or reboot.

## 5. Use Playbook Defaults To Avoid Repetition

When most tasks in a playbook should run as the exhibit user, set `defaults.become` at the playbook level:

```yaml
name: configure-exhibit-user

defaults:
  become:
    user: exhibit
    password: secret:exhibit-password

tasks:
  - name: Set environment variable
    environment:
      name: EXHIBIT_MODE
      value: "kiosk"
      scope: user
      ensure: present

  - name: Write registry value
    registry:
      path: 'HKCU:\Software\ExhibitApp'
      values:
        StartFullscreen: "1"
      ensure: present

  - name: Create exhibit user
    become:
      enabled: false
    user:
      name: exhibit
      password: secret:exhibit-password
      ensure: present
```

The last task uses `enabled: false` to run as the transport account instead of inheriting the default `become`.

## 6. Load The User Profile

By default, Windows `become` does not load the user's full profile (environment variables, `APPDATA`, `USERPROFILE`, etc.). Set `load_profile: true` when the task needs those values:

```yaml
  - name: Configure user shell settings
    become:
      user: exhibit
      password: secret:exhibit-password
      load_profile: true
    powershell:
      script: |
        $appData = $env:APPDATA
        New-Item -ItemType Directory -Path "$appData\ExhibitApp" -Force | Out-Null
```

## Troubleshooting

### `become: password is required for Windows user`

Windows `runas` requires a password. Make sure `password` is set and the
secret is configured in the project:

```bash
preflight secret list
```

If the secret exists but holds the wrong value, update it with
`preflight secret edit exhibit-password`.

### `become` is not taking effect

Check that the task does not have `enabled: false` and that it is not overriding an inherited default with an empty block. A `become:` key without `user:` is an error.

## POSIX: Become To Root From An Unprivileged SSH User

On POSIX targets the recommended posture is the reverse of Windows: connect over SSH as an **unprivileged** user and escalate per task with `become`. Allowing root SSH login is a stated working alternative (it passes the `requires_root` check with no `become`), but an unprivileged session plus `become` is the default recommendation.

A bare `become: {enabled: true}` means **root** on POSIX — you do not need to name `user: root`.

### Password-first: the host-var sudo password pattern

The documented primary path supplies `become.password` from a `secret:` reference. Put the become default in the playbook and the per-host sudo password in ordinary host/group vars — Preflight resolves `secret:` refs in `become.password` right before execution:

```yaml
# playbooks/manage-hosts.yml
name: manage-hosts

defaults:
  become:
    enabled: true            # bare become means root on POSIX
    password: "{{ sudo_password }}"

tasks:
  - name: ensure a package is present
    system_package:
      packages:
        - name: htop
          ensure: present
```

```yaml
# preflight.yml — the sudo password is a per-host secret
inventory:
  hosts:
    - name: web-01
      address: [IP_ADDRESS]
      transport: ssh
      username: deploy              # unprivileged SSH user
      password: secret:deploy-password
      vars:
        sudo_password: secret:deploy-sudo-password
```

The same shape works at the group level — put `sudo_password` in the group's `vars:` and every host in the group inherits it.

### NOPASSWD fallback

If a host's sudoers is configured with `NOPASSWD` for the SSH user, omit `become.password` entirely. Preflight runs `sudo -n`, which fails fast with a `sudo-password-required` reason code if a password turns out to be required, so a misconfigured `NOPASSWD` never hangs the run.

### `requires_root` modules

`service`, `user`, `system_package`, and `reboot` require an effective root user. Preflight probes `id -u` once per target and fails such a task **before `Check()`** with a `requires-root-violation` reason code when the effective user is not root. Setting `become: {enabled: true}` (root) or connecting as root satisfies the check; become-to-a-non-root-user is caught by the same check.

## Related Docs

- [Playbook and action YAML reference](../reference/playbooks.md) — full `become` field reference
- [How `become` works](../explanation/become.md) — mechanics, method selection, inheritance
- [Manage secrets](./manage-secrets.md) — store and resolve credentials
- [Built-in module reference](../reference/modules.md) — `user` module
