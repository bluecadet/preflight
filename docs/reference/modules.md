# Built-In Module Reference

This page describes the built-in modules registered by [`internal/module/`](/Users/clay/repos/preflight/internal/module) and exposed through the runtime module registry.

## Execution Contract

Every module implements the same two-method contract:

- `Check(ctx, params) -> (needsChange, error)`
- `Apply(ctx, params) -> error`

The runner always calls `Check()` first. If it returns `false`, the task is reported as already in the desired state. If it returns `true`, `Apply()` runs unless the command is in dry-run mode.

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
| `powershell` | Yes | Yes | No |
| `environment` | Yes | Yes | No |
| `wait` | Yes | Yes | No |
| `reboot` | Yes | Yes | No |
| `registry` | Windows only | Yes | No |
| `service` | Windows only | Yes | No |
| `package` | Windows only | Yes | No |
| `shortcut` | Windows only | Yes | No |
| `scheduled_task` | Windows only | Yes | No |
| `user` | Windows only | Yes | No |
| `windows_feature` | Windows only | Yes | No |
| `firewall_rule` | Windows only | Yes | No |

Notes:

- On non-Windows local runs, Windows-only built-ins are still registered but fail fast with a Windows-only error.
- SSH currently implements only `file`, `directory`, and `shell`.

## Module Fields

### `registry`

Manage Windows registry keys and values.

| Field | Type | Meaning |
| --- | --- | --- |
| `path` | string | Registry key path |
| `values` | object | Value-name to value-data map |
| `ensure` | `present` or `absent` | Desired state |

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
| `owner` | string | Schema field; ownership handling is platform-dependent |
| `permissions` | string | Schema field; permission handling is platform-dependent |

### `directory`

Manage directories.

| Field | Type | Meaning |
| --- | --- | --- |
| `path` | string | Directory path |
| `ensure` | `present` or `absent` | Desired state |
| `owner` | string | Schema field; ownership handling is platform-dependent |
| `permissions` | string | Schema field; permission handling is platform-dependent |

### `package`

Manage package installation on Windows.

| Field | Type | Meaning |
| --- | --- | --- |
| `source` | string | MSI or EXE installer path |
| `args` | string[] | Extra installer arguments |
| `product_id` | string | MSI product GUID used for idempotency |
| `ensure` | `present` or `absent` | Desired state |

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
| `name` | string | Scheduled task name |
| `command` | string | Command to run |
| `trigger` | string | Trigger expression |
| `user` | string | Run-as user |
| `ensure` | `present` or `absent` | Desired state |

### `user`

Manage Windows local users.

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string | User name |
| `password` | string | Plaintext password |
| `password_from` | string | Secret reference for the password |
| `groups` | string[] | Group memberships |
| `ensure` | `present` or `absent` | Desired state |

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
| `creates` | string | Skip when this path already exists |

Exactly one of `script` or `file` should be provided for meaningful behavior.

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

Wait for a condition.

| Field | Type | Meaning |
| --- | --- | --- |
| `condition` | `port_open`, `file_exists`, or `service_running` | Wait condition |
| `timeout` | integer | Timeout in seconds |

## Related Docs

- [Playbook and action YAML reference](./yaml.md)
- [Targets, transports, and plugins](../explanation/targets-and-transports.md)
