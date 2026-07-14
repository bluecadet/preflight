# Embedded Stdlib Action Reference

This page describes the embedded actions shipped under the `preflight/` namespace. These actions are versioned with the binary and are resolved before project-local or remote actions.

## Resolution And Versioning

- Embedded stdlib actions use refs such as `preflight/windows-machine`.
- They are bundled into the binary with `//go:embed`.
- They do not have independent versions. Upgrading the binary upgrades the embedded stdlib.

## Leaf Actions

### `preflight/autologin`

Configure Windows automatic logon.

| Input | Type | Meaning |
| --- | --- | --- |
| `username` | string | User name for automatic logon |
| `password` | string | Password or secret reference |
| `domain` | string | Domain or `.` for local accounts |

### `preflight/git-sync`

Clone or update a Git repository on a Windows target.

Use `become` when the checkout should be owned by a kiosk, exhibit, or service account. Authentication values should usually be passed as `secret:<name>` values. The action passes HTTPS and SSH credentials through environment variables to the PowerShell process instead of putting them in the script text.

By default, the action adds `dest` to Git's global `safe.directory` list before running sync checks. This prevents Git for Windows from rejecting repositories whose files are owned by another account, which is common when setup runs as an administrator but the exhibit app runs as a kiosk user.

| Input | Type | Meaning |
| --- | --- | --- |
| `repo` | string | Git remote URL |
| `dest` | path | Destination working tree directory |
| `ref` | string | Branch, tag, or commit to check out |
| `remote` | string | Remote name, default `origin` |
| `local_branch` | string | Local branch to create or reset from `ref` |
| `detach` | bool | Check out `ref` detached |
| `fetch` | bool | Fetch remote updates when the repo already exists |
| `prune` | bool | Prune deleted remote refs during fetch |
| `fetch_tags` | bool | Fetch tags during sync |
| `reset` | bool | Hard-reset the working tree to the resolved ref |
| `clean` | bool | Remove untracked files after checkout |
| `clean_ignored` | bool | Include ignored files when cleaning |
| `submodules` | bool | Sync and update submodules recursively |
| `lfs` | bool | Run Git LFS install and pull |
| `depth` | int | Shallow clone or fetch depth; `0` means full history |
| `git_path` | string | Git executable path |
| `create_parent` | bool | Create the parent directory for `dest` |
| `set_remote_url` | bool | Ensure `remote` points at `repo` |
| `safe_directory` | bool | Add `dest` to Git `safe.directory`, default `true` |
| `http_username` | string | HTTPS askpass username |
| `http_password` | string | HTTPS askpass password or token |
| `ssh_private_key` | string | SSH private key content |
| `ssh_known_hosts` | string | SSH known hosts content |
| `ssh_strict_host_key_checking` | bool | Require SSH host key verification |

## Grouped Windows Baseline Actions

### `preflight/windows-machine`

Configure machine-level baseline settings.

| Input | Type | Meaning |
| --- | --- | --- |
| `computer_name` | string | Desired computer name |
| `timezone` | string | Windows time zone ID |
| `enable_long_paths` | bool | Enable or disable the long path policy |
| `ps1_execution_policy` | string | LocalMachine PowerShell execution policy |
| `disable_new_network_prompt` | bool | Disable the first-connection new network prompt |

### `preflight/windows-shell`

Configure desktop and Explorer defaults for the current execution identity, or set `user` to write supported settings for a named Windows account without switching process identity.

When `user` is set, registry-backed settings, including taskbar auto-hide, are written through the target user's profile hive. The target hive must already be loaded, such as while that user is signed in or by running with `become.load_profile`. Desktop shortcut cleanup resolves the target user's configured Desktop folders, the profile's normal Desktop folder, OneDrive desktop folders, and the public desktop, then removes `.lnk`, `.url`, and `.website` files. It also hides common shell desktop icons such as This PC, Network, and Control Panel.

Some shell visual changes do not update an already-running Explorer session immediately. Expect them to apply after sign-out, Explorer restart, or reboot.

| Input | Type | Meaning |
| --- | --- | --- |
| `user` | string | Optional Windows user for user-scoped shell settings |
| `clear_desktop_background` | bool | Clear the wallpaper |
| `clear_desktop_shortcuts` | bool | Remove shortcut files from desktop locations and hide common shell desktop icons |
| `taskbar_auto_hide` | bool | Enable or disable taskbar auto-hide (default: `false`) |
| `theme_mode` | string | Keep the current theme or set both app and system surfaces to `light` or `dark` |
| `transparency_effects` | bool | Enable or disable transparency effects (default: `true`) |
| `hide_recycle_bin` | bool | Hide the Recycle Bin icon |
| `show_hidden_files` | bool | Show hidden files in Explorer |
| `show_file_extensions` | bool | Show file extensions in Explorer |
| `show_empty_drives` | bool | Show empty removable drives in Explorer |
| `show_sync_provider_notifications` | bool | Show sync provider notifications in Explorer |
| `aero_shake` | bool | Enable Aero Shake window minimization |
| `launch_to_this_pc` | bool | Open Explorer to This PC instead of Quick Access |
| `show_item_check_boxes` | bool | Show item check boxes in Explorer |
| `taskbar_allow_resize` | bool | Allow taskbar resizing |
| `taskbar_on_all_displays` | bool | Show taskbar on all displays |
| `taskbar_allow_multimon` | bool | Allow multi-monitor taskbar behavior |
| `hide_this_pc_folders` | bool | Hide common library folders from This PC |
| `disable_network_folder_thumbnails` | bool | Prevent Explorer thumbnail caches on network folders |
| `show_store_apps_on_taskbar` | bool | Show Microsoft Store apps on the taskbar |
| `clear_start_pins` | bool | Apply an empty Start pin set |
| `start_pins_json` | string | Explicit `ConfigureStartPins` JSON payload |

### `preflight/windows-input`

Configure input, gesture, and text-scale preferences. User-facing preferences apply to the current execution identity, or to `user` when set. Policy-backed edge-swipe settings apply at machine scope.

When `user` is set, user-scoped registry settings are written through the target user's profile hive. The target hive must already be loaded, such as while that user is signed in or by running with `become.load_profile`.

User-scoped visual input changes may require sign-out or a new Explorer session before the desktop reflects them.

| Input | Type | Meaning |
| --- | --- | --- |
| `user` | string | Optional Windows user for user-scoped input preferences |
| `disable_edge_gestures` | bool | Disable edge swipe gestures |
| `disable_touch_feedback` | bool | Disable touch contact visualization |
| `disable_touch_gestures` | bool | Disable gesture visualization |
| `text_scale_percent` | int | Text scale percentage, typically `100` |
| `disable_accessibility_shortcuts` | bool | Disable Sticky Keys, Filter Keys, and Toggle Keys shortcuts |

### `preflight/windows-quiet-mode`

Reduce notifications, prompts, and recovery UI noise with machine-scoped Windows policy settings.

| Input | Type | Meaning |
| --- | --- | --- |
| `user` | string | Optional Windows user for user-scoped notification and prompt settings |
| `disable_notifications` | bool | Disable toast and cloud notifications |
| `disable_news_and_interests` | bool | Hide the Windows news and interests feed |
| `disable_error_reporting` | bool | Disable Windows Error Reporting |
| `disable_windows_setup_prompt` | bool | Disable consumer and cloud-optimized Windows prompts |
| `disable_app_restore_on_boot` | bool | Disable automatic restart sign-on after update reboots |
| `disable_app_suggestions` | bool | Disable user-scoped consumer app suggestions and silent app installs |

### `preflight/windows-update-lockdown`

Reduce background system changes driven by Windows Update and Microsoft Store policy.

| Input | Type | Meaning |
| --- | --- | --- |
| `disable_windows_update` | bool | Disable automatic Windows Update policy checks |
| `disable_windows_update_service` | bool | Stop and disable Windows Update, Update Orchestrator, and Windows Update Medic services |
| `disable_update_auto_reboot` | bool | Prevent Windows Update from rebooting while users are signed in |
| `disable_store_auto_download` | bool | Disable Store app auto-download and update behavior |

### `preflight/windows-power`

Manage named power plans and user-scoped screensaver defaults.

When `user` is set, screen saver registry settings are written through the target user's profile hive. The target hive must already be loaded, such as while that user is signed in or by running with `become.load_profile`.

Screen saver changes are persisted immediately but may require sign-out or a new Explorer session before the shell reflects them.

| Input | Type | Meaning |
| --- | --- | --- |
| `user` | string | Optional Windows user for user-scoped screen saver preferences |
| `plan_name` | string | Friendly name for the managed power plan |
| `plan_base` | string | Base plan alias or GUID to clone |
| `activate_plan` | bool | Activate the managed plan after applying it |
| `display_timeout_ac` | int | AC display timeout in minutes |
| `display_timeout_dc` | int | DC display timeout in minutes |
| `sleep_timeout_ac` | int | AC sleep timeout in minutes |
| `sleep_timeout_dc` | int | DC sleep timeout in minutes |
| `disable_screensaver` | bool | Disable the screen saver |

Use the `scheduled_task` module directly when a machine needs a recurring reboot. See [Schedule a Windows reboot](../how-to/schedule-a-windows-reboot.md).

### `preflight/debloat`

Remove common built-in Windows apps (Xbox, Cortana, News, Weather, Teams, Skype, OneDrive) and set privacy, Copilot, search, taskbar, and Game DVR policies.

When `user` is set, user-scoped registry settings are written through the target user's profile hive. The target hive must already be loaded, such as while that user is signed in or by running with `become.load_profile`. App removal and machine policy settings are not profile-scoped. Use `remove_appx_packages` directly if you need a custom app list.

| Input | Type | Meaning |
| --- | --- | --- |
| `user` | string | Optional Windows user for user-scoped privacy and taskbar settings |

## Related Docs

- [Built-in module reference](./modules.md)
- [Playbook and action YAML reference](./playbooks.md)
- [Actions, stdlib, and lockfiles](../explanation/actions-and-lockfiles.md)
