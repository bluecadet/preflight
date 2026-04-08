# Built-In Module Reference

This page describes the built-in modules registered by [`internal/module/`](/Users/clay/repos/preflight/internal/module) and exposed through the runtime module registry.

## Execution Contract

The built-in modules exposed through the local registry implement the same two-method contract:

- `Check(ctx, params) -> (needsChange, error)`
- `Apply(ctx, params) -> error`

The runner always calls `Check()` first. If it returns `false`, the task is reported as already in the desired state. If it returns `true`, `Apply()` runs unless the command is in dry-run mode.

Remote transports adapt that contract into the shared runtime dispatcher:

- `Check(ctx, params) -> (needsChange, message, error)`
- `Apply(ctx, params) -> (output, error)`

That allows remote runtimes to return a no-op message from `Check()` and captured command output from `Apply()` while preserving the same dry-run and idempotency flow.

## Task Forms

Built-ins can be used either as inline modules:

```yaml
- name: Ensure a directory exists
  directory:
    path: "C:\\Exhibits\\Content"
```

or as explicit modules:

```yaml
- name: Ensure a directory exists
  module: directory
  params:
    path: "C:\\Exhibits\\Content"
```

## Platform And Transport Support

| Module | Local target | WinRM target | SSH target |
| --- | --- | --- | --- |
| `file` | Yes | Yes | Yes |
| `directory` | Yes | Yes | Yes |
| `shell` | Yes | Yes | Yes |
| `powershell` | Yes | Yes | Yes on Windows-over-SSH; on POSIX-over-SSH when `pwsh` or `powershell` is installed |
| `environment` | Yes | Yes | Windows-over-SSH only |
| `wait` | Yes | Yes | Yes on Windows-over-SSH; partial on POSIX-over-SSH (`file_exists`, `port_open`) |
| `reboot` | Yes | Yes | Windows-over-SSH only |
| `registry` | Windows only | Yes | Windows-over-SSH only |
| `service` | Windows only | Yes | Windows-over-SSH only |
| `package` | Windows only | Yes | Windows-over-SSH only |
| `winget_package` | Windows only | Yes | Windows-over-SSH only |
| `remove_appx_packages` | Windows only | Yes | Windows-over-SSH only |
| `shortcut` | Windows only | Yes | Windows-over-SSH only |
| `scheduled_task` | Windows only | Yes | Windows-over-SSH only |
| `user` | Windows only | Yes | Windows-over-SSH only |
| `power_plan` | Windows only | Yes | Windows-over-SSH only |
| `windows_feature` | Windows only | Yes | Windows-over-SSH only |
| `firewall_rule` | Windows only | Yes | Windows-over-SSH only |

Notes:

- On non-Windows local runs, Windows-only built-ins are still registered but fail fast with a Windows-only error.
- SSH auto-detects `windows-powershell` or `posix-shell` at connection time.
- Windows-over-SSH shares the built-in Windows module surface with WinRM.
- POSIX-over-SSH currently supports `file`, `directory`, `shell`, `wait` (`file_exists`, `port_open`), and `powershell` when a remote PowerShell binary is available.
- Plugin modules are not yet supported over SSH.

## Module Fields

### `registry`

Manage Windows registry keys and values.

| Field | Type | Meaning |
| --- | --- | --- |
| `path` | string | Registry key path |
| `values` | object or list | Legacy value-name map or typed value spec list |
| `ensure` | `present` or `absent` | Desired state |

Typed value specs inside `values` support these fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string | Registry value name |
| `type` | `string`, `expand_string`, `dword`, `qword`, `binary`, or `multi_string` | Registry value type |
| `data` | any | Registry value data |
| `ensure` | `present` or `absent` | Desired value state |

### `service`

Manage Windows services.

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string | Service name |
| `state` | `running`, `stopped`, or `disabled` | Desired service state |
| `startup_type` | `automatic`, `manual`, or `disabled` | Startup behavior |

### `file`

Manage files.

| Field | Type | Meaning |
| --- | --- | --- |
| `src` | string | Local source path to copy from |
| `dest` | string | Destination path |
| `ensure` | `present` or `absent` | Desired state |

### `directory`

Manage directories.

| Field | Type | Meaning |
| --- | --- | --- |
| `path` | string | Directory path |
| `ensure` | `present` or `absent` | Desired state |

### `package`

Manage local MSI or EXE installations on Windows.

```yaml
- name: Install packages
  package:
    packages:
      - product_id: "{D5E71B88-9A6C-4B6B-89C0-123456789ABC}"
        source: "C:\\Installers\\app.msi"
      - product_id: "{AAAA-...}"
        source: "C:\\Installers\\tool.exe"
        args: ["/silent", "/norestart"]
      - product_id: "{OLD-GUID}"
        ensure: absent
```

| Field | Type | Meaning |
| --- | --- | --- |
| `product_id` | string (required) | MSI product GUID used for idempotency |
| `source` | string | MSI or EXE installer path (required when `ensure=present`) |
| `args` | string[] | Extra installer arguments |
| `ensure` | `present` or `absent` | Desired state (default: `present`) |

The legacy single-package form (`product_id` at the top level) is still accepted.

Use `package` when you already have a staged or local installer path. Use `winget_package` for package-manager-driven installs.

### `winget_package`

Manage packages through `winget`.

```yaml
- name: Install packages
  winget_package:
    packages:
      - id: Microsoft.VisualStudioCode
        version: "1.85.0"
      - id: Git.Git
        scope: machine
      - id: OldApp
        ensure: absent
```

The `packages` list is the primary interface. Each entry supports:

| Field | Type | Meaning |
| --- | --- | --- |
| `id` | string (required) | `winget` package identifier |
| `version` | string | Pin to an exact version |
| `source` | string | `winget` source name |
| `scope` | `machine` or `user` | Install scope (default: `machine`) |
| `ensure` | `present` or `absent` | Desired state (default: `present`) |

**Legacy single-package form** — `id` at the top level is still accepted for backward compatibility and behaves identically to a one-entry `packages` list:

```yaml
winget_package:
  id: Microsoft.VisualStudioCode
  version: "1.85.0"
```

### `remove_appx_packages`

Remove built-in Windows Store-style packages.

```yaml
- name: Remove bloatware
  remove_appx_packages:
    packages:
      - name: Microsoft.Xbox*
        scope: both
      - name: Microsoft.BingNews
      - name: Microsoft.549981C3F5F10
        scope: provisioned
```

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string (required) | Package name or wildcard pattern |
| `scope` | `current_user`, `all_users`, `provisioned`, or `both` | Removal scope (default: `both`) |
| `ensure` | `absent` | Desired state |

`remove_appx_packages` supports removal only. The legacy single-package form (`name` at the top level) is still accepted.

### `shortcut`

Manage Windows `.lnk` shortcuts.

| Field | Type | Meaning |
| --- | --- | --- |
| `target` | string | Shortcut target path |
| `destination` | string | `.lnk` path to manage |
| `args` | string | Optional arguments |
| `icon` | string | Optional icon path |

### `scheduled_task`

Manage Windows scheduled tasks.

| Field | Type | Meaning |
| --- | --- | --- |
| `path` | string | Scheduled task folder path, such as `\Preflight\` |
| `name` | string | Scheduled task name |
| `execute` | string | Executable path |
| `command` | string | Alias for `execute` |
| `arguments` | string | Optional command arguments |
| `working_dir` | string | Optional working directory |
| `trigger` | `startup`, `onlogon`, `daily`, or `once` | Trigger type |
| `start_at` | string | Start time for `daily` and `once` triggers |
| `delay` | string | Delay for `startup` and `onlogon` triggers |
| `run_as` | string | Run-as user |
| `user` | string | Alias for `run_as` |
| `run_level` | `least` or `highest` | Privilege level |
| `enabled` | bool | Enabled state |
| `ensure` | `present` or `absent` | Desired state |

`delay` accepts ISO-8601 duration strings such as `PT30S`. `command` and `user` remain supported as compatibility aliases.

### `user`

Manage Windows local users.

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string | User name |
| `password` | string | Plaintext password or a secret reference |
| `groups` | string[] | Group memberships |
| `ensure` | `present` or `absent` | Desired state |

When `ensure: present` is used without a `password`, Preflight creates the user
without a password if the account does not already exist. If the user already
exists, omitting `password` leaves the current password unchanged. Requested
`groups` are additive and ensure membership in those groups without removing
other existing memberships.

### `power_plan`

Manage named Windows power plans.

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string | Friendly scheme name |
| `base` | string | Base alias or GUID to clone when creating the scheme |
| `activate` | bool | Whether to activate the scheme after applying it |
| `settings` | list | AC and DC setting overrides |
| `ensure` | `present` or `absent` | Desired state |

Each entry in `settings` supports:

| Field | Type | Meaning |
| --- | --- | --- |
| `subgroup` | string | Power setting subgroup alias or GUID |
| `setting` | string | Power setting alias or GUID |
| `ac_value` | integer | AC value override |
| `dc_value` | integer | DC value override |

### `windows_feature`

Manage Windows optional features.

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string | Feature name |
| `ensure` | `present` or `absent` | Desired state |

### `environment`

Manage environment variables.

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string | Variable name |
| `value` | string | Variable value |
| `scope` | `machine` or `user` | Target scope |
| `ensure` | `present` or `absent` | Desired state |

### `firewall_rule`

Manage Windows firewall rules.

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string | Rule name |
| `direction` | `inbound` or `outbound` | Traffic direction |
| `action` | `allow` or `block` | Rule behavior |
| `protocol` | `tcp`, `udp`, or `any` | Protocol |
| `ports` | int, string, or array | Port or port list |
| `ensure` | `present` or `absent` | Desired state |

### `powershell`

Run PowerShell.

| Field | Type | Meaning |
| --- | --- | --- |
| `script` | string | Inline PowerShell script |
| `file` | string | Path to a PowerShell script file |
| `args` | string[] | Arguments passed to the script file path |
| `check_script` | string | Inline non-mutating PowerShell check script |
| `creates` | string | Skip when this path already exists |

Exactly one of `script` or `file` should be provided for meaningful behavior.

`check_script` takes precedence over `creates`. It must return either:

- a boolean, where `true` means change is needed
- an object with `needs_change` and optional `message`

### `shell`

Run a shell command.

| Field | Type | Meaning |
| --- | --- | --- |
| `cmd` | string | Command to execute |
| `args` | string[] | Command arguments |
| `creates` | string | Skip when this path already exists |
| `working_dir` | string | Working directory |

### `reboot`

Request a reboot.

| Field | Type | Meaning |
| --- | --- | --- |
| `condition` | `always` or `if_needed` | Reboot policy |
| `timeout` | integer | Timeout in seconds |

### `wait`

Wait for a condition to be met before continuing.

| Field | Type | Meaning |
| --- | --- | --- |
| `condition` | `port_open`, `file_exists`, or `service_running` | Wait condition |
| `target` | string | What to wait on — interpretation depends on `condition` (see below) |
| `timeout` | duration string | Maximum time to wait, e.g. `"5m"`, `"30s"` (default: `"5m"`) |

The `target` field is required and interpreted per condition:

| `condition` | `target` format | Example |
| --- | --- | --- |
| `port_open` | `address:port` TCP endpoint | `"localhost:8080"` |
| `file_exists` | File system path | `"C:\\Exhibits\\ready.txt"` |
| `service_running` | Windows service name | `"W32Time"` |

## Related Docs

- [Playbook and action YAML reference](./yaml.md)
- [Targets, transports, and plugins](../explanation/targets-and-transports.md)
