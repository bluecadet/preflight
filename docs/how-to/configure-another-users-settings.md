# Configure Another User's Settings

Use this guide when a task must change settings that belong to a specific user account, such as a kiosk account's registry preferences or environment variables, without signing in as that user.

Preflight does not switch identity for a task. Every task runs as the account your transport (local, WinRM, or SSH) authenticates with. The `registry` and `environment` modules instead edit the target user's profile hive directly, and the `preflight/windows-*` stdlib actions pass the same `user` input through to `registry`.

## Prerequisites

- A working playbook and target connection
- A transport account that can read and write other users' profiles (a local administrator)
- The target user has signed in at least once, so the profile exists

## Set Registry Values For A User

Add `user` to a `registry` task whose path starts with `HKCU:` or `HKEY_CURRENT_USER`:

```yaml
tasks:
  - name: Disable the screen saver for exhibit
    registry:
      path: 'HKCU:\Control Panel\Desktop'
      user: exhibit
      values:
        - name: ScreenSaveActive
          type: string
          data: "0"
```

`user` is rejected for any other hive.

## Set Environment Variables For A User

`environment.user` requires `scope: user`:

```yaml
  - name: Set exhibit mode
    environment:
      name: EXHIBIT_MODE
      value: kiosk
      scope: user
      user: exhibit
```

The variable is written to `HKEY_USERS\<SID>\Environment`. Without `user`, `scope: user` applies to the transport account.

## Configure Settings Through Stdlib Actions

`preflight/windows-shell`, `preflight/windows-input`, `preflight/windows-power`, and `preflight/debloat` accept `with.user` for their user-scoped registry settings:

```yaml
  - name: Configure shell defaults for exhibit user
    uses: preflight/windows-shell
    with:
      user: exhibit
      theme_mode: dark
      taskbar_auto_hide: true
```

Some shell-facing changes are saved to the profile immediately but only appear after the user signs out and back in, Explorer restarts, or the machine reboots.

## Signed-In Versus Signed-Out Users

Preflight resolves the user's SID and checks whether the profile hive is loaded:

- **Signed in.** The hive is already loaded under `HKEY_USERS\<SID>`. Preflight edits it in place and leaves it loaded.
- **Signed out.** Preflight loads `NTUSER.DAT` from the user's profile directory as `HKEY_USERS\preflight-<SID>`, applies the change, and unloads it again.

If the user has no profile, the task fails with `user <name> has no profile on this machine; sign in once to create it`.

## Limitations

- `winget_package` with `scope: user` and `remove_appx_packages` with `scope: current_user` act on the transport account. They cannot target another user.
- Only registry-backed settings are covered. Tasks that need a user's files, `APPDATA`, or a running session are not.

## Run A Command As Another User

When a command must run as that user, schedule it with `scheduled_task`:

```yaml
  - name: Run the exhibit setup as exhibit
    scheduled_task:
      name: ExhibitSetup
      execute: C:\Exhibit\setup.exe
      trigger: onlogon
      run_as: exhibit
```

## Troubleshooting

### `user <name> was not found on this machine`

The name could not be resolved to a SID. Use `DOMAIN\name` or `MACHINE\name` for accounts outside the local machine.

### `could not unload the profile hive`

Another process kept a handle open on the hive. Retry the task; if it persists, sign the user in so the hive stays loaded.

## Related Docs

- [Playbook and action YAML reference](../reference/playbooks.md)
- [Built-in module reference](../reference/modules.md) for `registry`, `environment`, and `scheduled_task`
- [Stdlib actions reference](../reference/stdlib-actions.md)
