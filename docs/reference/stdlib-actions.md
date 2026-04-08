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
| `password` | string | Plaintext password |
| `password_from` | string | Secret reference for the password |
| `domain` | string | Domain or `.` for local accounts |

## Grouped Windows Baseline Actions

### `preflight/windows-machine`

Configure machine-level baseline settings.

| Input | Type | Meaning |
| --- | --- | --- |
| `computer_name` | string | Desired computer name |
| `timezone` | string | Windows time zone ID |
| `enable_long_paths` | bool | Enable or disable the long path policy |
| `ps1_execution_policy` | string | LocalMachine PowerShell execution policy |

### `preflight/windows-shell`

Configure desktop and Explorer defaults.

| Input | Type | Meaning |
| --- | --- | --- |
| `scope_bias` | string | `machine` or `user` for supported per-user settings |
| `clear_desktop_background` | bool | Clear the wallpaper |
| `clear_desktop_shortcuts` | bool | Remove `.lnk` and `.url` files from desktop locations |
| `hide_recycle_bin` | bool | Hide the Recycle Bin icon |
| `show_hidden_files` | bool | Show hidden files in Explorer |
| `show_file_extensions` | bool | Show file extensions in Explorer |
| `clear_start_pins` | bool | Apply an empty Start pin set |
| `start_pins_json` | string | Explicit `ConfigureStartPins` JSON payload |

### `preflight/windows-input`

Configure input, gesture, and text-scale preferences.

| Input | Type | Meaning |
| --- | --- | --- |
| `scope_bias` | string | `machine` or `user` for supported per-user settings |
| `disable_edge_gestures` | bool | Disable edge swipe gestures |
| `disable_touch_feedback` | bool | Disable touch contact visualization |
| `disable_touch_gestures` | bool | Disable gesture visualization |
| `text_scale_percent` | int | Text scale percentage, typically `100` |

### `preflight/windows-quiet-mode`

Reduce notifications, prompts, and recovery UI noise.

| Input | Type | Meaning |
| --- | --- | --- |
| `scope_bias` | string | `machine` or `user` for supported per-user settings |
| `disable_notifications` | bool | Disable toast and cloud notifications |
| `disable_error_reporting` | bool | Disable Windows Error Reporting |
| `disable_windows_setup_prompt` | bool | Disable consumer and cloud-optimized Windows prompts |
| `disable_app_restore_on_boot` | bool | Disable automatic restart sign-on after update reboots |

### `preflight/windows-update-lockdown`

Reduce background system changes driven by Windows Update and Microsoft Store policy.

| Input | Type | Meaning |
| --- | --- | --- |
| `disable_windows_update` | bool | Disable automatic Windows Update policy checks |
| `disable_windows_update_service` | bool | Stop and disable the Windows Update service |
| `disable_store_auto_download` | bool | Disable Store app auto-download and update behavior |

### `preflight/windows-power`

Manage named power plans, screensaver defaults, and optional scheduled reboot tasks.

| Input | Type | Meaning |
| --- | --- | --- |
| `scope_bias` | string | `machine` or `user` for supported per-user settings |
| `plan_name` | string | Friendly name for the managed power plan |
| `plan_base` | string | Base plan alias or GUID to clone |
| `activate_plan` | bool | Activate the managed plan after applying it |
| `display_timeout_ac` | int | AC display timeout in minutes |
| `display_timeout_dc` | int | DC display timeout in minutes |
| `sleep_timeout_ac` | int | AC sleep timeout in minutes |
| `sleep_timeout_dc` | int | DC sleep timeout in minutes |
| `disable_screensaver` | bool | Disable the screen saver |
| `scheduled_reboot_state` | string | `present` or `absent` for the reboot task |
| `scheduled_reboot_time` | string | Daily scheduled reboot time |
| `scheduled_reboot_name` | string | Scheduled reboot task name |

### `preflight/debloat`

Remove common built-in Windows apps (Xbox, Cortana, News, Weather, Teams, Skype). No inputs — use `remove_appx_packages` directly if you need a custom list.

## `scope_bias`

Several grouped Windows actions expose `scope_bias`.

| Value | Meaning |
| --- | --- |
| `machine` | Apply the current-user setting and, where supported by the action, also seed the default user profile for future accounts |
| `user` | Apply only the current-user setting |

## Related Docs

- [Built-in module reference](./modules.md)
- [Playbook and action YAML reference](./yaml.md)
- [Actions, stdlib, and lockfiles](../explanation/actions-and-lockfiles.md)
