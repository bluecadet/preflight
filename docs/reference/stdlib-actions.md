# Stdlib action reference

Stdlib actions are embedded in the `preflight` binary under the `preflight/` namespace. A `preflight/<name>` ref resolves before project, cache, and Git refs. Each action has a `version` field, but the ref cannot pin it; upgrading the binary upgrades the stdlib.

```yaml
tasks:
  - name: Machine baseline
    uses: preflight/windows-machine
    with:
      timezone: Eastern Standard Time
```

An input with no default in the tables below is required.

User-scoped settings in actions with a `user` input apply to the transport account when `user` is empty. When `user` is set, they are written to that user's profile hive, which is loaded on demand if the user is signed out. See [Configure another user's settings](../how-to/configure-another-users-settings.md).

## `preflight/autologin`

Configures Windows automatic logon through the `Winlogon` registry key.

| Input | Type | Default | Description |
| --- | --- | --- | --- |
| `username` | string | | Account to sign in as |
| `password` | string | `""` | Account password; accepts `secret:<name>` |
| `domain` | string | `.` | Domain; `.` for local accounts |

## `preflight/git-sync`

Clones or updates a Git repository on the target. The checkout is owned by the transport account. HTTPS and SSH credentials reach Git through environment variables, not the script text.

| Input | Type | Default | Description |
| --- | --- | --- | --- |
| `repo` | string | | Git remote URL |
| `dest` | path | | Working tree directory |
| `ref` | string | `""` | Branch, tag, or commit to check out |
| `remote` | string | `origin` | Remote name |
| `local_branch` | string | `""` | Local branch to create or reset from `ref` |
| `detach` | bool | `false` | Check out `ref` as a detached HEAD |
| `fetch` | bool | `true` | Fetch when the repository exists |
| `prune` | bool | `true` | Prune deleted remote refs during fetch |
| `fetch_tags` | bool | `true` | Fetch tags |
| `reset` | bool | `true` | Hard-reset the working tree to the resolved ref |
| `clean` | bool | `false` | Remove untracked files after checkout |
| `clean_ignored` | bool | `false` | Also remove ignored files when `clean` is set |
| `submodules` | bool | `false` | Initialize and update submodules recursively |
| `lfs` | bool | `false` | Run `git lfs install` and `git lfs pull` after checkout |
| `depth` | int | `0` | Clone or fetch depth; `0` is full history |
| `git_path` | string | `git` | Git executable |
| `create_parent` | bool | `true` | Create the parent directory of `dest` |
| `set_remote_url` | bool | `true` | Set `remote` to `repo` before fetching |
| `safe_directory` | bool | `true` | Add `dest` to the global Git `safe.directory` list |
| `http_username` | string | `x-access-token` | HTTPS user name returned by askpass |
| `http_password` | string | `""` | HTTPS password or token returned by askpass |
| `ssh_private_key` | string | `""` | SSH private key content, used through a temporary `GIT_SSH_COMMAND` |
| `ssh_known_hosts` | string | `""` | Known hosts content used with `ssh_private_key` |
| `ssh_strict_host_key_checking` | bool | `true` | Require SSH host key verification |

## `preflight/windows-machine`

Configures machine-wide settings.

| Input | Type | Default | Description |
| --- | --- | --- | --- |
| `computer_name` | string | `""` | Computer name; empty leaves it unchanged |
| `timezone` | string | `""` | Windows time zone ID; empty leaves it unchanged |
| `enable_long_paths` | bool | `true` | Long path policy |
| `ps1_execution_policy` | string | `RemoteSigned` | `LocalMachine` PowerShell execution policy |
| `disable_new_network_prompt` | bool | `true` | Disable the new network prompt on first connection |

## `preflight/windows-shell`

Configures desktop, Explorer, taskbar, and Start settings for the selected user.

`clear_desktop_shortcuts` removes `.lnk`, `.url`, and `.website` files from the user's Desktop folders, OneDrive desktop folders, and the public desktop, and hides the This PC, Network, and Control Panel desktop icons. Some changes take effect only after sign-out, an Explorer restart, or a reboot.

| Input | Type | Default | Description |
| --- | --- | --- | --- |
| `user` | string | `""` | Windows user to configure |
| `clear_desktop_background` | bool | `true` | Clear the wallpaper |
| `clear_desktop_shortcuts` | bool | `true` | Remove desktop shortcuts and hide common desktop icons |
| `taskbar_auto_hide` | bool | `false` | Taskbar auto-hide |
| `theme_mode` | string | `unchanged` | `unchanged`, `light`, or `dark`, applied to app and system surfaces |
| `transparency_effects` | bool | `true` | Transparency effects |
| `hide_recycle_bin` | bool | `true` | Hide the Recycle Bin desktop icon |
| `show_hidden_files` | bool | `true` | Show hidden files in Explorer |
| `show_file_extensions` | bool | `true` | Show file extensions in Explorer |
| `show_empty_drives` | bool | `true` | Show empty removable drives in Explorer |
| `show_sync_provider_notifications` | bool | `false` | Show sync provider notifications in Explorer |
| `aero_shake` | bool | `false` | Aero Shake window minimization |
| `launch_to_this_pc` | bool | `true` | Open Explorer to This PC instead of Quick Access |
| `show_item_check_boxes` | bool | `false` | Show item check boxes in Explorer |
| `taskbar_allow_resize` | bool | `true` | Allow taskbar resizing |
| `taskbar_on_all_displays` | bool | `false` | Show the taskbar on all displays |
| `taskbar_allow_multimon` | bool | `true` | Allow the multi-monitor taskbar |
| `hide_this_pc_folders` | bool | `true` | Hide Music, Pictures, Videos, and 3D Objects from This PC |
| `disable_network_folder_thumbnails` | bool | `true` | Stop thumbnail caches on network folders |
| `show_store_apps_on_taskbar` | bool | `false` | Show Microsoft Store apps on the taskbar |
| `hide_spotlight_desktop_icon` | bool | `true` | Hide the Windows Spotlight "Learn about this picture" desktop icon |
| `clear_start_pins` | bool | `false` | Replace Start pins with `start_pins_json` |
| `start_pins_json` | string | `{"pinnedList":[]}` | `ConfigureStartPins` JSON payload |

## `preflight/windows-input`

Configures touch, gesture, accessibility shortcut, and text scale settings. Edge swipe and touch edge-targeting policies are machine-scoped; the other settings apply to the selected user. Visual changes may take effect only after sign-out or an Explorer restart.

| Input | Type | Default | Description |
| --- | --- | --- | --- |
| `user` | string | `""` | Windows user to configure |
| `disable_edge_gestures` | bool | `true` | Disable screen-edge swipe gestures |
| `disable_touch_feedback` | bool | `true` | Disable touch contact visualization |
| `disable_touch_gestures` | bool | `true` | Disable gesture visualization |
| `text_scale_percent` | int | `100` | Text size percentage |
| `disable_accessibility_shortcuts` | bool | `true` | Disable the Sticky Keys, Filter Keys, and Toggle Keys shortcuts |

## `preflight/windows-quiet-mode`

Suppresses notifications, prompts, and error reporting UI. Notification policy, news and interests, error reporting, consumer features, and restart sign-on are machine-scoped. Notification center, toast, setup prompt, and app suggestion settings apply to the selected user.

| Input | Type | Default | Description |
| --- | --- | --- | --- |
| `user` | string | `""` | Windows user for user-scoped settings |
| `disable_notifications` | bool | `true` | Disable toast and cloud notifications |
| `disable_news_and_interests` | bool | `true` | Disable the news and interests feed |
| `disable_error_reporting` | bool | `true` | Disable Windows Error Reporting UI and submission |
| `disable_windows_setup_prompt` | bool | `true` | Disable consumer and cloud-optimized Windows prompts |
| `disable_app_restore_on_boot` | bool | `true` | Disable automatic restart sign-on after Windows Update restarts |
| `disable_app_suggestions` | bool | `true` | Disable consumer app suggestions and silent app installs |

## `preflight/windows-update-lockdown`

Limits Windows Update and Microsoft Store background changes. All settings are machine-scoped.

| Input | Type | Default | Description |
| --- | --- | --- | --- |
| `disable_windows_update` | bool | `true` | Disable automatic Windows Update checks by policy |
| `disable_windows_update_service` | bool | `true` | Stop and disable the Windows Update, Update Orchestrator, and Windows Update Medic services |
| `disable_update_auto_reboot` | bool | `true` | Prevent automatic restarts while users are signed in |
| `disable_store_auto_download` | bool | `true` | Disable Store app automatic downloads |

## `preflight/windows-power`

Manages a named power plan, hibernation, and the selected user's screen saver. Screen saver changes may take effect only after sign-out.

| Input | Type | Default | Description |
| --- | --- | --- | --- |
| `user` | string | `""` | Windows user whose screen saver to configure |
| `plan_name` | string | `Preflight Exhibit` | Managed power plan name |
| `plan_base` | string | `balanced` | Plan alias or GUID to clone |
| `activate_plan` | bool | `true` | Activate the managed plan |
| `display_timeout_ac` | int | `0` | Display timeout on AC power, in minutes; `0` is never |
| `display_timeout_dc` | int | `0` | Display timeout on battery, in minutes; `0` is never |
| `sleep_timeout_ac` | int | `0` | Sleep timeout on AC power, in minutes; `0` is never |
| `sleep_timeout_dc` | int | `0` | Sleep timeout on battery, in minutes; `0` is never |
| `disable_screensaver` | bool | `true` | Disable the screen saver |
| `disable_hibernation` | bool | `false` | Disable hibernation and remove `hiberfil.sys` |

For a recurring reboot, use the `scheduled_task` module. See [Schedule a Windows reboot](../how-to/schedule-a-windows-reboot.md).

## `preflight/debloat`

Removes built-in Microsoft and third-party Store apps, uninstalls the OneDrive desktop client, and disables telemetry, activity history, advertising ID, Copilot, Recall, Bing and Cortana search, delivery optimization, and Game DVR. App removal and machine policies are not user-scoped. For a custom app list, use the `remove_appx_packages` module.

| Input | Type | Default | Description |
| --- | --- | --- | --- |
| `user` | string | `""` | Windows user for user-scoped privacy and taskbar settings |

## `preflight/winget`

Installs WinGet machine-wide and registers it for the transport account, so `winget_package` works over SSH and WinRM. Use it on editions that ship without WinGet, such as Windows IoT Enterprise LTSC, and for accounts that have never signed in interactively. The transport account must be an administrator.

| Input | Type | Default | Description |
| --- | --- | --- | --- |
| `source_max_age` | string | `24h` | Re-provision the `winget` source once its build is older than this, in `s`, `m`, `h` or `d`. `0` only provisions a missing source |

The action runs three tasks:

1. Provision App Installer: unless it is already provisioned, downloads the latest App Installer bundle and its dependencies for the target's architecture from the `winget-cli` GitHub releases, then provisions them for all users.
2. Provision the `winget` source: downloads `source2.msix` from the WinGet CDN and provisions it when it is missing or older than `source_max_age`.
3. Register WinGet for the transport account: registers both packages by family name when App Installer is missing for the account, or its source is older than the provisioned one.

WinGet can't install these packages for a remote session itself; per-user installs fail with `0x80073D19`. Provisioning machine-wide, then registering, avoids that path.

- The target needs internet access to GitHub and `cdn.winget.microsoft.com`.
- The source is refreshed from the CDN. If the CDN build is older than `source_max_age`, the task reports `changed` on every run.
- `preflight check` on a target without WinGet reports later `winget_package` tasks as failed, because the bootstrap has not run yet.

Known limitations:

- App Installer comes from the `winget-cli` `releases/latest` download, unpinned and without a hash check. Each fresh provisioning takes whatever release is current.
- The source guard only detects a missing `Microsoft.Winget.Source` package. A present but broken source isn't detected or repaired.

```yaml
tasks:
  - name: WinGet
    uses: preflight/winget
  - name: Tools
    winget_package:
      packages:
        - id: 7zip.7zip
```

## `preflight/chocolatey`

Installs Chocolatey and configures its package sources, so `choco_package` works. The transport account must be an administrator.

| Input | Type | Default | Description |
| --- | --- | --- | --- |
| `version` | string | `""` | Chocolatey version to install, 2.0 or later because `choco_package` rejects 1.x; empty installs the latest |
| `sources` | list | `[]` | Sources to add or update. Each entry has `name` and `url` |
| `remove_default_source` | bool | `false` | Remove the community source named `chocolatey` |

The action runs two tasks:

1. Install Chocolatey: when `choco.exe` is missing, downloads `install.ps1` from `community.chocolatey.org` and runs it.
2. Configure Chocolatey sources: adds each source in `sources`, or updates its URL and enables it, then removes the `chocolatey` source when `remove_default_source` is set.

- The target needs internet access to `community.chocolatey.org` for the install.
- `version` applies only to the install. An existing Chocolatey is not upgraded or downgraded.
- Sources that are not listed are left alone, except `chocolatey` with `remove_default_source`.
- Sources with credentials, priorities, or certificates aren't supported. Add them with a `powershell` task.
- `preflight check` on a target without Chocolatey reports later `choco_package` tasks as failed, because the bootstrap has not run yet.

Known limitations:

- A partial `ProgramData\chocolatey` directory without `bin\choco.exe` makes the Chocolatey installer refuse to run, which blocks the bootstrap. Remove the directory first.
- Pinning `version` below 2.0 installs a Chocolatey that `choco_package` then rejects. The input has no schema check for this.
- Updating a source relies on `choco source add` replacing an existing source of the same name in place.

```yaml
tasks:
  - name: Chocolatey
    uses: preflight/chocolatey
    with:
      sources:
        - name: internal
          url: https://nuget.example.com/chocolatey/
      remove_default_source: true
  - name: Tools
    choco_package:
      packages:
        - id: 7zip
```

## Related docs

- [Built-in module reference](./modules.md)
- [Playbook and action YAML reference](./playbooks.md)
- [Execution model](../explanation/execution-model.md)
