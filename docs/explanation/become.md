# How `become` Works

`become` lets a task execute under a different user identity than the one used to connect to the target. This page explains when to reach for it, how the runtime handles identity switching on each platform, and how `become` settings propagate through the task hierarchy.

## When To Use `become`

Use `become` when a task produces results that belong to a specific user:

- Writing to `HKCU:\` registry keys for a named account
- Setting user-scoped environment variables
- Creating files in `%APPDATA%` or `%USERPROFILE%`
- Running an application's first-launch setup in the context of a kiosk account
- Any task where the user identity changes the outcome

Do not use `become` when the task only needs elevated privilege — on Windows that is handled by the transport account's privilege level, not by `become`.

## Runtime Methods

### Windows — `runas`

On Windows, `become` defaults to `method: runas`. Preflight writes the task script to a temporary file and uses `Start-Process` with a `PSCredential` to execute it under the target user's token.

The temporary file is cleaned up after the task regardless of outcome.

A password is required for all Windows `become` users. Preflight rejects a task with `become.user` set but no `password`.

### POSIX — `sudo`

On POSIX targets (SSH with a POSIX shell runtime), `become` defaults to `method: sudo`. Preflight wraps the command as:

```bash
sudo -u <user> /bin/sh -lc <command>
```

When a `password` is provided, it is fed via `sudo -S`. When no password is provided, `sudo` must already be configured to allow the transport user to switch to the target user without a password prompt (for example, via a `NOPASSWD` rule in `/etc/sudoers`).

## `load_profile`

On Windows, `runas` does not automatically load the target user's profile. When `load_profile: true` is set, Preflight sets `LoadUserProfile = $true` in the `Start-Process` call, which populates the user's environment variables (`APPDATA`, `USERPROFILE`, `HOME`, etc.) before the task runs.

Use `load_profile: true` any time the task depends on user profile paths or user-specific environment variables.

```yaml
become:
  user: exhibit
  password: secret:exhibit-password
  load_profile: true
```

On POSIX, `sudo -u <user> /bin/sh -lc` already loads a login shell, so `load_profile` has no additional effect.

## Inheritance Model

`become` flows down through three levels, with more-specific settings winning:

```text
playbook defaults.become
  -> action defaults.become
    -> task become
```

Each level performs a shallow merge — keys present in the override layer replace keys from the parent. This means you can set a default user and password at the playbook level and override only `load_profile` on a single task:

```yaml
defaults:
  become:
    user: exhibit
    password: secret:exhibit-password

tasks:
  - name: Write profile-dependent config
    become:
      load_profile: true
    powershell:
      script: New-Item -ItemType Directory "$env:APPDATA\ExhibitApp" -Force | Out-Null
```

Setting `enabled: false` on a task disables the inherited `become` for that task regardless of any parent defaults:

```yaml
  - name: Create exhibit user (runs as transport account)
    become:
      enabled: false
    user:
      name: exhibit
      password: secret:exhibit-password
      ensure: present
```

## Secret Resolution

`become.password` supports the same `secret:<name>` reference syntax as module params. Preflight resolves the secret immediately before task execution. Secret values are never written into staged bundles — playbooks that use `become` with secret-backed passwords cannot be staged.

## State And Redaction

`become` metadata is recorded alongside task params in the state file. The `password` value is redacted in all state output and diffs.

## Related Docs

- [Run tasks as another user](../how-to/run-tasks-as-another-user.md) — step-by-step how-to
- [Playbook and action YAML reference](../reference/yaml.md) — `become` field reference
- [Manage secrets](../how-to/manage-secrets.md)
