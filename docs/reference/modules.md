# Built-In Module Reference

This page is the authoritative reference for the built-in modules: what
each one does, which runtimes support it, its fields, and its known
limitations. The per-module `Supported runtimes` lines below are asserted
against the module catalog in code by a drift test, so this page cannot
silently disagree with the binary.

Modules on this page:
[`registry`](#registry) ·
[`service`](#service) ·
[`file`](#file) ·
[`directory`](#directory) ·
[`package`](#package) ·
[`winget_package`](#winget_package) ·
[`remove_appx_packages`](#remove_appx_packages) ·
[`shortcut`](#shortcut) ·
[`scheduled_task`](#scheduled_task) ·
[`user`](#user) ·
[`power_plan`](#power_plan) ·
[`windows_feature`](#windows_feature) ·
[`environment`](#environment) ·
[`firewall_rule`](#firewall_rule) ·
[`powershell`](#powershell) ·
[`shell`](#shell) ·
[`reboot`](#reboot) ·
[`wait`](#wait)

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

Every built-in runs on the local, WinRM, and SSH targets. All targets must be Windows.

Notes:

- \*`windows_feature` and `remove_appx_packages` are registered over WinRM but cannot complete their changes over a basic WinRM session. See [WinRM session limitations](#winrm-session-limitations).
- The local target is only supported when the controller itself runs Windows. SSH targets must provide Windows PowerShell; connection fails otherwise.
- Plugin modules run over every transport (local, SSH, and WinRM) — because the plugin process runs controller-side and its target effects flow through the transport's handle ops. See the [plugin reference](./plugins.md#execution-model).

Unsupported module usage is caught before the task runs and returns a
clear, typed error; there is no silent fallback. See the
[error reference](./errors.md) for when each layer catches a violation and
the reason codes involved.

## WinRM Session Limitations

The WinRM transport authenticates with NTLM/Negotiate and runs each
operation under a non-interactive network logon. Some Windows operations
require privileges or a user profile that this kind of session does not
provide, so they cannot be performed over WinRM regardless of the module
used:

- **`windows_feature` (DISM online servicing)** — enabling or disabling an
  optional feature fails with *"The symbolic link cannot be followed
  because its type is disabled."* DISM follows symlinks in the component
  store (WinSxS), and a network-logon token is not permitted to follow
  them. Reading feature state works; changing it does not.
- **`remove_appx_packages` with all-users scope** —
  `Remove-AppxPackage -AllUsers` fails with HRESULT `0x80073D19` (*"An
  error occurred because a user was logged off."*). All-users AppX removal
  needs an interactive session context.
- **Incremental output streaming** — over WinRM, the WS-Man channel
  buffers a command's stdout and delivers it in a single batch when the
  command completes. Output from the `powershell` module is still
  delivered correctly and in order; it just does not arrive line-by-line
  as it is produced.

These are properties of the WinRM session, not defects in the modules, so
Preflight cannot work around them in PowerShell. There is no CredSSP
option in the WinRM transport (see
[why CredSSP would not lift most of these](../explanation/targets-and-transports.md#would-credssp-help)).
When you need these operations:

- run them with the **local target** or a **staged bundle** executed on
  the box, or
- use an **interactive/elevated context** (for example a scheduled task),
  or
- for live streaming specifically, use **Windows-over-SSH**, where output
  is delivered incrementally.

## Module Fields

### `registry`

**Supported runtimes:** `windows-powershell`

Manage Windows registry keys and values.

| Field | Type | Meaning |
| --- | --- | --- |
| `path` | string | Registry key path |
| `user` | string | Optional Windows user for `HKCU`/`HKEY_CURRENT_USER` paths |
| `values` | list | Typed value spec list |
| `ensure` | `present` or `absent` | Desired state |

Typed value specs inside `values` support these fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string | Registry value name |
| `type` | `string`, `expand_string`, `dword`, `qword`, `binary`, or `multi_string` | Registry value type |
| `data` | any | Registry value data |
| `patch` | list | Byte patches for an existing `binary` value |
| `ensure` | `present` or `absent` | Desired value state |

Use `patch` when a Windows setting is stored inside an existing binary registry value and the rest of the value should be preserved:

```yaml
- name: Enable taskbar auto-hide
  registry:
    path: 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Explorer\StuckRects3'
    values:
      - name: Settings
        type: binary
        patch:
          - offset: 8
            data: 3
```

### `service`

**Supported runtimes:** `windows-powershell`

Manage Windows services.

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string | Service name |
| `state` | `running`, `stopped`, or `disabled` | Desired service state |
| `startup_type` | `automatic`, `manual`, or `disabled` | Startup behavior |

### `file`

**Supported runtimes:** `windows-powershell`

Manage files.

| Field | Type | Meaning |
| --- | --- | --- |
| `src` | string | Local source path to copy from |
| `content` | string | Inline file content to write; may be a `secret:<name>` reference |
| `content_template` | string | Inline file content template to render before writing; supports `secret("name")` placeholders |
| `dest` | string | Destination path |
| `ensure` | `present` or `absent` | Desired state |

Use `src`, `content`, or `content_template`; do not combine them. `content` is useful for writing
secret-backed files without creating a temporary plaintext source file:

```yaml
- name: Write license file
  file:
    dest: "C:\\Exhibits\\license.txt"
    content: secret:license-file
```

Use `content_template` when only part of the file is secret:

```yaml
- name: Write app config
  file:
    dest: "C:\\Exhibits\\app.ini"
    content_template: |
      username={{ vars.app_user }}
      password={{ secret("app-password") }}
```

`secret:<name>` is still the syntax for whole-field secret references. Inside
`content_template`, use `secret("name")` so the secret can be interpolated into
the rendered file body.

### `directory`

**Supported runtimes:** `windows-powershell`

Manage directories.

| Field | Type | Meaning |
| --- | --- | --- |
| `path` | string | Directory path |
| `ensure` | `present` or `absent` | Desired state |

### `package`

**Supported runtimes:** `windows-powershell`

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

Use `package` when you already have a staged or local installer path. Use `winget_package` for package-manager-driven installs.

### `winget_package`

**Supported runtimes:** `windows-powershell`

Manage packages through `winget`.

```yaml
- name: Install packages
  winget_package:
    packages:
      - id: Microsoft.VisualStudioCode
        version: "1.85.0"
      - id: Git.Git
        scope: machine
      - id: Microsoft.VisualStudio.2022.Community
        args:
          - --override
          - "--quiet --wait --norestart"
      - id: OldApp
        ensure: absent
```

The `packages` list is the primary interface. Each entry supports:

| Field | Type | Meaning |
| --- | --- | --- |
| `id` | string (required) | `winget` package identifier |
| `version` | string | Pin to an exact version |
| `source` | string | `winget` source name |
| `args` | string[] | Extra `winget` command arguments |
| `scope` | `machine` or `user` | Install scope (default: `machine`) |
| `ensure` | `present` or `absent` | Desired state (default: `present`) |

Put package-specific `winget` flags under `args` on that package entry. Do not add flags as additional `packages` list items.

### `remove_appx_packages`

**Supported runtimes:** `windows-powershell`

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

Installed Appx packages that Windows marks `NonRemovable` are ignored so checks do not report changes that Windows will not allow Preflight to apply.

### `shortcut`

**Supported runtimes:** `windows-powershell`

Manage Windows `.lnk` shortcuts.

| Field | Type | Meaning |
| --- | --- | --- |
| `target` | string | Shortcut target path |
| `destination` | string | `.lnk` path to manage |
| `args` | string | Optional arguments |
| `icon` | string | Optional icon path |

### `scheduled_task`

**Supported runtimes:** `windows-powershell`

Manage Windows scheduled tasks.

| Field | Type | Meaning |
| --- | --- | --- |
| `path` | string | Scheduled task folder path, such as `\Preflight\` |
| `name` | string | Scheduled task name |
| `execute` | string | Executable path |
| `arguments` | string | Optional command arguments |
| `working_dir` | string | Optional working directory |
| `trigger` | `startup`, `onlogon`, `daily`, or `once` | Trigger type |
| `start_at` | string | Start time for `daily` and `once` triggers |
| `delay` | string | Delay for `startup` and `onlogon` triggers |
| `run_as` | string | Run-as user |
| `run_level` | `least` or `highest` | Privilege level |
| `enabled` | bool | Enabled state |
| `ensure` | `present` or `absent` | Desired state |

`delay` accepts ISO-8601 duration strings such as `PT30S`.

### `user`

**Supported runtimes:** `windows-powershell`

Manage local users.

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string | User name |
| `password` | string | Plaintext password or a secret reference |
| `groups` | string[] | Group memberships |
| `ensure` | `present` or `absent` | Desired state |

**Windows.** When `ensure: present` is used without a `password`, Preflight creates the
user without a password if the account does not already exist. If the user
already exists, omitting `password` leaves the current password unchanged.
Requested `groups` are additive and ensure membership in those groups without
removing other existing memberships.

### `power_plan`

**Supported runtimes:** `windows-powershell`

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

**Supported runtimes:** `windows-powershell`

Manage Windows optional features.

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string | Feature name |
| `ensure` | `present` or `absent` | Desired state |

### `environment`

**Supported runtimes:** `windows-powershell`

Manage environment variables.

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string | Variable name |
| `value` | string | Variable value |
| `scope` | `machine` or `user` | Target scope |
| `ensure` | `present` or `absent` | Desired state |

### `firewall_rule`

**Supported runtimes:** `windows-powershell`

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

**Supported runtimes:** `windows-powershell`

Run PowerShell.

| Field | Type | Meaning |
| --- | --- | --- |
| `script` | string | Inline PowerShell script |
| `file` | string | Path to a PowerShell script file |
| `args` | string[] | Arguments passed to the script file path |
| `check_script` | string | Inline non-mutating PowerShell check script |
| `creates` | string | Skip when this path already exists |
| `working_dir` | string | Working directory |
| `env` | object | Environment variables visible to the PowerShell process |

Exactly one of `script` or `file` should be provided for meaningful behavior.

When `working_dir` is set, relative `creates` paths are checked from that directory.

`check_script` takes precedence over `creates`. It must return either:

- a boolean, where `true` means change is needed
- an object with `needs_change` and optional `message`

### `shell`

**Supported runtimes:** `windows-powershell`

Run a shell command.

| Field | Type | Meaning |
| --- | --- | --- |
| `cmd` | string | Command to execute |
| `args` | string[] | Command arguments |
| `creates` | string | Skip when this path already exists |
| `working_dir` | string | Working directory |
| `env` | object | Environment variables visible to the command process |

When `working_dir` is set, relative `creates` paths are checked from that directory.

### `reboot`

**Supported runtimes:** `windows-powershell`

Request a reboot.

| Field | Type | Meaning |
| --- | --- | --- |
| `condition` | `always` or `if_needed` | Reboot policy (default: `if_needed`) |
| `timeout` | integer | Reconnect-wait timeout in seconds (default: `300`) |

### `wait`

**Supported runtimes:** `windows-powershell`

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

- [Playbook and action YAML reference](./playbooks.md)
- [Error reference](./errors.md)
- [Targets, transports, and plugins](../explanation/targets-and-transports.md)
