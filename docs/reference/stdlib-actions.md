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

## Related docs

- [Built-in module reference](./modules.md)
- [Playbook and action YAML reference](./playbooks.md)
- [Execution model](../explanation/execution-model.md)
