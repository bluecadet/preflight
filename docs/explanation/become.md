# How `become` Works

`become` lets a task execute under a different user identity than the one used to connect to the target. This page explains when to reach for it, how the runtime handles identity switching on each platform, and how `become` settings propagate through the task hierarchy.

## When To Use `become`

Use `become` when a task produces results that belong to a specific user:

- Writing to `HKCU:\` registry keys for a named account
- Setting user-scoped environment variables
- Creating files in `%APPDATA%` or `%USERPROFILE%`
- Running an application's first-launch setup in the context of a kiosk account
- Any task where the user identity changes the outcome

Do not use `become` when the task only needs elevated privilege on **Windows** — on Windows that is handled by the transport account's privilege level, not by `become`. On **POSIX**, the opposite holds: `become: {enabled: true}` (which means root) is the documented way to run a `requires_root` module (`service`, `user`, `system_package`, `reboot`) from an unprivileged SSH session.

## Runtime Methods

### Windows — `runas`

On Windows, `become` defaults to `method: runas`. How the identity switch happens depends on the target type:

- **Local target** — Preflight relaunches itself as a child process under the target user's credentials, with stdio redirected so module input and output flow through normally.
- **Remote targets (WinRM and Windows-over-SSH)** — a remote session is non-interactive and has no window station, so starting a process directly under alternate credentials fails inside it. Preflight instead stages the task script on the target, grants the become user the batch-logon right if needed, registers and runs a one-shot scheduled task as that user, waits for it to finish, and replays its output. A non-zero exit code from the task is surfaced as the task's failure.

Staged files and the scheduled task are cleaned up after the task regardless of outcome.

A password is required for all Windows `become` users. Preflight rejects a task with `become.user` set but no `password`.

### POSIX — `sudo`

On POSIX targets (SSH with a POSIX shell runtime, and the local target on a POSIX machine), `become` defaults to `method: sudo`. The recommended posture is **unprivileged SSH user + become**: connect as an ordinary user and escalate per task, rather than allowing root SSH login. Root login over SSH is a stated working alternative — it passes the `requires_root` check with no `become`.

A bare `become: {enabled: true}` with no `user` means **root** on POSIX. This fixes the former empty-user sudo wrap. Windows keeps requiring an explicit `become.user`.

Preflight wraps the command as:

```bash
sudo -n -u <user> /bin/sh -lc <command>
```

The `-n` flag makes a password-requiring `sudo` fail deterministically instead of hanging the run on a prompt. When a `password` is provided, it is fed via `sudo -S -p ''` on stdin.

**Password-first posture.** The documented primary path is `become.password` backed by a `secret:` reference; relying on `NOPASSWD` sudoers is the fallback, not the headline. See [Run tasks as another user](../how-to/run-tasks-as-another-user.md) for the `defaults.become` + host-var `sudo_password` pattern.

**`requires_root` modules.** Some modules (`service`, `user`, `system_package`, `reboot`) require an effective root user. Preflight probes `id -u` once per target (cached with runtime detection) and fails the task **before `Check()`** with a `requires-root-violation` reason code when the effective user is not root. The effective user is `become.user` when become is enabled, otherwise the session user — so become-to-a-non-root-user is caught by the same check. The wording names the module and offers both fixes: run as root, or set `become: {enabled: true}`.

**sudo availability.** `sudo` is required only when `become` is used. A POSIX target with `become` enabled but no `sudo` binary fails fast with a `sudo-missing` reason code. A no-password `sudo -n` run that needs a password fails with `sudo-password-required`; a rejected password fails with `sudo-auth-failed`.

When no password is provided, `sudo` must already be configured to allow the transport user to switch to the target user without a password prompt (for example, via a `NOPASSWD` rule in `/etc/sudoers`).

## `load_profile`

On the local Windows target, `runas` does not automatically load the target user's profile. When `load_profile: true` is set, the child process is started with the user's profile loaded, which populates the user's environment variables (`APPDATA`, `USERPROFILE`, `HOME`, etc.) before the task runs. Use `load_profile: true` any time a local task depends on user profile paths or user-specific environment variables.

On remote Windows targets (WinRM and Windows-over-SSH), the scheduled task's password logon always loads the target user's profile, so `load_profile` has no additional effect there.

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
