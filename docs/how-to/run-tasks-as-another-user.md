# Run Tasks As Another User

Use this guide when you need tasks to execute under a specific user identity — for example, configuring a kiosk account's profile, writing to a user's `APPDATA`, or running a command that checks user-scoped registry values.

## Prerequisites

- A working playbook and target connection
- The account used for your transport (WinRM or SSH) must have permission to create local users and to execute commands as other users
- For Windows `become`: the target user must have a password (except when using the `SYSTEM` account)

## Overview

The typical pattern has two phases:

1. Create the user account using the `user` module, running as your transport account.
2. Add `become` to tasks that must run in that user's context.

## 1. Store The Password As A Secret

Avoid hardcoding passwords. Use a named secret reference instead:

```bash
preflight secrets add exhibit-password
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

Windows `runas` requires a password for non-`SYSTEM` accounts. Make sure `password` is set and the secret resolves correctly:

```bash
preflight secrets show exhibit-password
```

### `become` is not taking effect

Check that the task does not have `enabled: false` and that it is not overriding an inherited default with an empty block. A `become:` key without `user:` is an error.

### Running as `SYSTEM`

To run a task as the Windows `SYSTEM` account, set `user: SYSTEM`. No password is needed. Preflight uses a temporary scheduled task to execute under the `SYSTEM` service account:

```yaml
  - name: Run as SYSTEM
    become:
      user: SYSTEM
    powershell:
      script: Write-Output $env:USERNAME
```

## Related Docs

- [Playbook and action YAML reference](../reference/yaml.md) — full `become` field reference
- [How `become` works](../explanation/become.md) — mechanics, method selection, inheritance
- [Manage secrets](./manage-secrets.md) — store and resolve credentials
- [Built-in module reference](../reference/modules.md) — `user` module
