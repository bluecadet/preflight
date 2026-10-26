# Configure another user's settings

Change registry settings or environment variables for a specific Windows account, such as a kiosk user, without signing in as that user.

Every task runs as the account the transport authenticates with. The `registry` and `environment` modules take a `user` parameter and edit that user's profile hive directly.

## Prerequisites

- A transport account in the local Administrators group on the target
- A profile for the target user. The user must have signed in at least once.

## Set registry values for a user

Add `user` to a `registry` task whose path starts with `HKCU:` or `HKEY_CURRENT_USER`. Other hives reject `user`.

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

## Set environment variables for a user

Set `scope: user` and `user`:

```yaml
  - name: Set exhibit mode
    environment:
      name: EXHIBIT_MODE
      value: kiosk
      scope: user
      user: exhibit
```

Without `user`, `scope: user` writes to the transport account's environment.

## Use stdlib actions

`preflight/windows-shell`, `preflight/windows-input`, `preflight/windows-power`, `preflight/windows-quiet-mode`, and `preflight/debloat` accept a `user` input for their per-user settings:

```yaml
  - name: Configure shell defaults for exhibit
    uses: preflight/windows-shell
    with:
      user: exhibit
      theme_mode: dark
      taskbar_auto_hide: true
```

Some shell settings take effect only after the user signs out and back in, Explorer restarts, or the machine reboots.

## Signed-in and signed-out users

If the user is signed in, Preflight edits the loaded hive under `HKEY_USERS\<SID>`. If not, Preflight loads the user's `NTUSER.DAT` as `HKEY_USERS\preflight-<SID>`, applies the change, and unloads it.

## Run a command as another user

To run a program as another user, register it with `scheduled_task` and `run_as`:

```yaml
  - name: Run the exhibit setup as exhibit
    scheduled_task:
      name: ExhibitSetup
      execute: C:\Exhibit\setup.exe
      trigger: onlogon
      run_as: exhibit
```

The task runs with an interactive logon, so it runs only while that user is signed in.

## Limitations

- `winget_package` with `scope: user` and `remove_appx_packages` with `scope: current_user` act on the transport account, not another user.
- `user` covers registry-backed settings only. It does not reach a user's files, `APPDATA`, or running session.

## Troubleshooting

| Error | Fix |
|---|---|
| `user <name> was not found on this machine` | Preflight cannot resolve the name to a SID. Use `DOMAIN\name` for a domain account. |
| `user <name> has no profile on this machine; sign in once to create it` | Sign in as the user once to create the profile. |
| `could not unload the profile hive` | Another process held the hive open. Retry the task, or sign the user in so Preflight edits the loaded hive instead. |

## Related docs

- [Built-in module reference](../reference/modules.md)
- [Stdlib actions reference](../reference/stdlib-actions.md)
- [Playbook reference](../reference/playbooks.md)
