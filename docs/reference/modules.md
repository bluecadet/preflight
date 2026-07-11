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
| `wait` | Yes | Yes | Yes on Windows-over-SSH; on POSIX-over-SSH (`file_exists`, `port_open`, `service_running`) |
| `reboot` | Yes | Yes | Windows-over-SSH; POSIX-over-SSH (systemd) |
| `registry` | Windows only | Yes | Windows-over-SSH only |
| `service` | Windows only | Yes | Yes on Windows-over-SSH; on POSIX-over-SSH over systemd (**requires root**) |
| `package` | Windows only | Yes | Windows-over-SSH only |
| `winget_package` | Windows only | Yes | Windows-over-SSH only |
| `remove_appx_packages` | Windows only | Yes* | Windows-over-SSH only |
| `shortcut` | Windows only | Yes | Windows-over-SSH only |
| `scheduled_task` | Windows only | Yes | Windows-over-SSH only |
| `user` | Windows only | Yes | Yes (Windows-over-SSH; POSIX-over-SSH, requires root) |
| `power_plan` | Windows only | Yes | Windows-over-SSH only |
| `windows_feature` | Windows only | Yes* | Windows-over-SSH only |
| `firewall_rule` | Windows only | Yes | Windows-over-SSH only |
| `system_package` | POSIX only | No | POSIX-over-SSH only (apt or dnf) |

Notes:

- \*`windows_feature` and `remove_appx_packages` are registered over WinRM but cannot complete their changes over a basic WinRM session (DISM symlink restriction; AppX all-users removal returns `0x80073D19`). See [WinRM Session Limitations](../explanation/targets-and-transports.md#winrm-session-limitations). Use the local target, a staged bundle, or Windows-over-SSH for these.
- On non-Windows local runs, Windows-only built-ins are still registered but fail fast with a Windows-only error.
- SSH auto-detects `windows-powershell` or `posix-shell` at connection time.
- Windows-over-SSH shares the built-in Windows module surface with WinRM.
- POSIX-over-SSH currently supports `file`, `directory`, `shell`, `wait` (`file_exists`, `port_open`, `service_running`), `reboot`, `powershell` when a remote PowerShell binary is available, `user` (requires root), `system_package` on targets with apt or dnf, and `service` over systemd (requires root).
- Plugin modules run over every transport — local, SSH (POSIX and Windows), and WinRM — because the plugin process runs controller-side and its target effects flow through the transport's handle ops. See [Plugins](../explanation/targets-and-transports.md#why-plugin-modules-fit-cleanly).

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

**Supported runtimes:** `windows-powershell`, `posix-shell` · **requires root**

Manage services. Same schema on Windows and POSIX; the runtime implementation
is per-platform (Windows SCM on Windows, `systemctl` on POSIX-over-SSH).

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string | Service name |
| `state` | `running`, `stopped`, or `disabled` | Desired service state |
| `startup_type` | `automatic`, `manual`, or `disabled` | Startup behavior |

**POSIX-over-SSH (systemd).** The target must run systemd (probed via
`/run/systemd/system`); a host without systemd fails the task with a typed
`missing_prerequisite` error naming what was probed. The module is
`requires root`: run the session as root or set `become: {enabled: true}` to
escalate (see [Become](../explanation/become.md)).

`state` maps to `systemctl` active state:

- `running` → the unit is active (`systemctl start`)
- `stopped` → the unit is inactive (`systemctl stop`)
- `disabled` → stop **and** mask in one step (`systemctl stop` + `systemctl
  mask`), short-circuiting `startup_type` just like the Windows apply. Windows'
  *disabled* service state corresponds to systemd *masked* (a unit that cannot
  be started).

`startup_type` maps to `systemctl` enable state:

- `automatic` → `enabled` (`systemctl enable`)
- `manual` → `disabled` (`systemctl disable`)
- `disabled` → `masked` (`systemctl mask`)

Enabling or disabling first unmasks the unit (a no-op when it is not masked)
so a previously masked unit can transition back to `enabled` or `disabled`.

The local POSIX target does not implement the service module; manage POSIX
services over SSH. The `shell` module is the escape hatch for
non-systemd init systems.

### `file`

**Supported runtimes:** `windows-powershell`, `posix-shell`

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

**Supported runtimes:** `windows-powershell`, `posix-shell`

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

### `system_package`

**Supported runtimes:** `posix-shell` · **requires root**

Manage repo packages through `apt` or `dnf` on POSIX targets.

```yaml
- name: Install packages
  system_package:
    packages:
      - name: tree
      - name: jq
        version: "1.6-2.1"
      - name: legacy-tool
        ensure: absent
```

The `packages` list is the primary interface. Each entry supports:

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string (required) | Package name as known to the detected package manager |
| `version` | string | Pin to an exact version, in the native manager format |
| `ensure` | `present` or `absent` | Desired state (default: `present`) |

`system_package` autodetects `apt` or `dnf` from the target's cached detection facts (`facts.os.package_manager`) and is POSIX-only. It mirrors the `winget_package` list shape but uses `name` instead of `id`. A task whose target has neither manager fails with a per-task environment-prerequisite error before `Check()` runs.

`version` is compared as an exact string against the native version string: `dpkg-query ${Version}` for apt, and `rpm %{VERSION}-%{RELEASE}` for dnf. Supply the full native version string the manager reports, including epoch or release where relevant. Targets with a package manager other than `apt` or `dnf` are not supported; use the `shell` module as an escape hatch.

`system_package` requires root. Run as root or set `become: {enabled: true}` to escalate to root; a non-root run fails with a `requires-root-violation` before `Check()`.

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

**Supported runtimes:** `windows-powershell`, `posix-shell` · **requires root**

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

**POSIX (requires root).** Over SSH-POSIX the same schema drives
`useradd`/`userdel`. `ensure: present` creates a missing user with `useradd`
and, when a `password` is supplied, sets it via `chpasswd` **on creation only**.
Group membership is additive (`usermod -aG`) and never strips existing
memberships. `ensure: absent` runs `userdel`.

*Known limitation — POSIX password drift.* The password of an existing user is
never reset, even when `password` is supplied and `Apply` runs for another
reason (for example, to add a group). Managed POSIX endpoints authenticate by
SSH key, so password drift on existing accounts is documented rather than
corrected. To force a password change, manage the password out of band (for
example, via a `shell` task running `chpasswd`).

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

*Known limitation — POSIX.* `environment` is unsupported over POSIX-over-SSH: ambient environment is login-shell plumbing with no faithful analog, and per-service environment belongs in unit files (managed with `file` + `service`). The local POSIX target registers an `os.Setenv`-backed implementation that only affects the preflight process, which is rarely what you want; for managed endpoints use `file` + `service` instead.

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

**Supported runtimes:** `windows-powershell`, `posix-shell`

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

**Supported runtimes:** `windows-powershell`, `posix-shell`

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

**Supported runtimes:** `windows-powershell`, `posix-shell` · **requires root**

Request a reboot.

| Field | Type | Meaning |
| --- | --- | --- |
| `condition` | `always` or `if_needed` | Reboot policy (default: `if_needed`) |
| `timeout` | integer | Reconnect-wait timeout in seconds (default: `300`) |

On POSIX-over-SSH (systemd hosts), `condition: always` issues `systemctl reboot`
and waits for the SSH connection to re-establish within `timeout`. `condition:
if_needed` probes the distro reboot-required signal: `/var/run/reboot-required`
(the apt convention, also honored as a plantable marker on any distro) and
`needs-restarting -r` (dnf). When neither signal is available, no reboot is
needed and the task output says so. `reboot` requires root on POSIX — run as
root or with `become`. The real reboot+reconnect path is unit-tested against
fakes only and is a stated limitation: it is not exercised end-to-end in CI.

### `wait`

**Supported runtimes:** `windows-powershell`, `posix-shell`

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
| `service_running` | Windows service name or systemd unit name | `"W32Time"`, `"nginx.service"` |

On POSIX-over-SSH, `service_running` probes `systemctl is-active --quiet`. It
requires systemd; a host with no init system detected fails the task with the
typed environment-prerequisite error (`missing_prerequisite`).

## Related Docs

- [Playbook and action YAML reference](./yaml.md)
- [Targets, transports, and plugins](../explanation/targets-and-transports.md)
