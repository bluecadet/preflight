# Built-in module reference

Modules:
[`registry`](#registry),
[`service`](#service),
[`file`](#file),
[`directory`](#directory),
[`package`](#package),
[`winget_package`](#winget_package),
[`remove_appx_packages`](#remove_appx_packages),
[`shortcut`](#shortcut),
[`scheduled_task`](#scheduled_task),
[`user`](#user),
[`power_plan`](#power_plan),
[`windows_feature`](#windows_feature),
[`environment`](#environment),
[`firewall_rule`](#firewall_rule),
[`powershell`](#powershell),
[`shell`](#shell),
[`reboot`](#reboot),
[`wait`](#wait).

## Execution contract

Each module implements `Check()` and `Apply()`. Preflight calls `Check()` first:

- No change needed: the task reports `ok` and `Apply()` does not run.
- Change needed: `Apply()` runs and the task reports `changed`. Under `preflight check`, `Apply()` does not run and the task reports `changed` with the message `would apply change (dry-run)`.

## Task forms

Inline:

```yaml
- name: Create content directory
  directory:
    path: "C:\\Exhibits\\Content"
```

Explicit:

```yaml
- name: Create content directory
  module: directory
  params:
    path: "C:\\Exhibits\\Content"
```

Inline parameters are validated against the module schema before the run, and unknown parameters are errors. `params` in the explicit form is not schema-validated.

## Platform and transport support

Every built-in module runs on `local`, `ssh`, and `winrm` targets. Every target must be Windows with Windows PowerShell. The `local` target requires a Windows controller.

Some operations cannot complete over WinRM; see [WinRM session limitations](#winrm-session-limitations).

Plugin modules run on every transport; see [Execution model](./plugins.md#execution-model).

An unknown module name fails at plan time; see [Plan-time checks](./errors.md#plan-time-checks).

## WinRM session limitations

WinRM authenticates with Basic auth, so only local accounts work, and each operation runs under a non-interactive network logon. These operations fail in that session:

| Operation | Failure |
| --- | --- |
| `windows_feature` enable or disable | `The symbolic link cannot be followed because its type is disabled.` DISM cannot follow component store links under a network logon. Reading feature state works. |
| `remove_appx_packages` with `all_users` or `both` scope | `Remove-AppxPackage -AllUsers` fails with HRESULT `0x80073D19`, `An error occurred because a user was logged off.` |
| Output streaming | Output arrives in one batch when the command ends, not line by line. |

WinRM has no CredSSP option; see [Would CredSSP help?](../explanation/targets-and-transports.md#would-credssp-help). Workarounds:

- Run on the machine with the `local` target or a staged bundle.
- Run from an interactive or elevated context, such as a scheduled task.
- For streaming output, use SSH.

## Modules

### `registry`

Manages a registry key and its values.

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `path` | string | yes | | Key path. Accepts `HKLM`, `HKCU`, `HKCR`, `HKU`, and `HKCC` prefixes, with or without `:`, and the `HKEY_*` names |
| `values` | list | no | | [Value specs](#registry-value-spec) |
| `ensure` | `present`, `absent` | no | `present` | `absent` deletes the key and its subkeys |
| `user` | string | no | | Local user whose `HKCU` hive to edit; requires an `HKCU` path |

#### Registry value spec

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `name` | string | yes | | Value name |
| `type` | `string`, `expand_string`, `dword`, `qword`, `binary`, `multi_string` | no | inferred from `data` | Value type |
| `data` | any | with `ensure: present` and no `patch` | | Value data |
| `patch` | list | no | | Byte patches for an existing `binary` value; each entry has `offset` (integer) and `data` (byte, 0-255). Cannot be combined with `data` |
| `ensure` | `present`, `absent` | no | `present` | Desired value state |

Without `type`, a string is `string`, an integer or boolean is `dword`, a list of strings is `multi_string`, and a list of numbers is `binary`.

With `user`, the user's profile hive is loaded for the task and unloaded after it. Errors:

- `user <name> was not found on this machine`
- `user <name> has no profile on this machine; sign in once to create it`
- `registry: user can only be used with HKCU/HKEY_CURRENT_USER paths`

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

Manages an existing Windows service.

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `name` | string | yes | | Service name |
| `state` | `running`, `stopped`, `disabled` | no | | `disabled` stops the service and sets startup to disabled, and ignores `startup_type` |
| `startup_type` | `automatic`, `manual`, `disabled` | no | | Startup type |

Fails with `service not found: <name>` when the service does not exist.

```yaml
- name: Start Windows Time
  service:
    name: W32Time
    state: running
    startup_type: automatic
```

### `file`

Manages a file on the target.

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `dest` | string | yes | | Path on the target |
| `src` | string | no | | Path on the controller to copy |
| `content` | string | no | | File content; accepts `secret:<name>` |
| `content_template` | string | no | | File content rendered as a template; accepts `secret("name")` |
| `ensure` | `present`, `absent` | no | `present` | Desired state |

- Set at most one of `src`, `content`, and `content_template`.
- `Check()` compares SHA-256 hashes of the source and the target file.
- `content` and `content_template` are written as UTF-8 without a byte order mark. Parent directories are created.
- With none of the three set, a missing file is created empty and an existing file is left unchanged.
- `content_template` renders after all other parameters; see [Secrets in templates](./templating.md#secrets-in-templates).

```yaml
- name: Write app config
  file:
    dest: "C:\\Exhibits\\app.ini"
    content_template: |
      username={{ vars.app_user }}
      password={{ secret("app-password") }}
```

### `directory`

Manages a directory.

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `path` | string | yes | | Directory path |
| `ensure` | `present`, `absent` | no | `present` | `present` creates parent directories; `absent` deletes the directory and its contents |

```yaml
- name: Create content directory
  directory:
    path: "C:\\Exhibits\\Content"
```

### `package`

Installs or removes MSI and EXE packages.

| Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `packages` | list | yes | Package entries |

Each entry:

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `product_id` | string | yes | | Product code, matched against the uninstall registry keys |
| `source` | string | with `ensure: present` | | Installer path on the controller; copied to a temporary directory on the target before install |
| `args` | string[] | no | | Extra installer arguments |
| `ensure` | `present`, `absent` | no | `present` | Desired state |

- An `.msi` source runs `msiexec /i <source> /qn /norestart <args>`. Any other source runs directly with `args`.
- `absent` runs `msiexec /x <product_id> /qn /norestart`.
- A non-zero exit code fails the task.

```yaml
- name: Install packages
  package:
    packages:
      - product_id: "{D5E71B88-9A6C-4B6B-89C0-123456789ABC}"
        source: "C:\\Installers\\app.msi"
      - product_id: "{0F1E2D3C-4B5A-6978-8796-A5B4C3D2E1F0}"
        ensure: absent
```

### `winget_package`

Installs or removes packages with `winget`.

| Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `packages` | list | yes | Package entries |

Each entry:

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `id` | string | yes | | `winget` package ID, matched exactly |
| `version` | string | no | | Exact version to install |
| `source` | string | no | | `winget` source name |
| `args` | string[] | no | | Extra `winget` arguments for this package |
| `scope` | `machine`, `user` | no | `machine` | Install scope |
| `ensure` | `present`, `absent` | no | `present` | Desired state |

- Without `version`, an installed package satisfies `present`. With `version`, the installed version must match.
- Installs run `winget install --id <id> --exact --silent --scope <scope>` with agreements accepted. Removals run `winget uninstall --id <id> --exact`.

```yaml
- name: Install tools
  winget_package:
    packages:
      - id: Microsoft.VisualStudioCode
        version: "1.85.0"
      - id: Microsoft.VisualStudio.2022.Community
        args: ["--override", "--quiet --wait --norestart"]
      - id: OldApp
        ensure: absent
```

### `remove_appx_packages`

Removes AppX packages.

| Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `packages` | list | yes | Package entries |

Each entry:

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `name` | string | yes | | Package name; accepts `*` wildcards |
| `scope` | `current_user`, `all_users`, `provisioned`, `both` | no | `both` | `both` covers all users and provisioned packages |
| `ensure` | `absent` | no | | Desired state |

- Packages that Windows marks `NonRemovable` are ignored.
- A failed provisioned-package removal prints a warning and does not fail the task.

```yaml
- name: Remove preinstalled apps
  remove_appx_packages:
    packages:
      - name: Microsoft.Xbox*
      - name: Microsoft.BingNews
        scope: provisioned
```

### `shortcut`

Creates or updates a `.lnk` shortcut.

| Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `destination` | string | yes | `.lnk` path |
| `target` | string | no | Target path |
| `args` | string | no | Arguments |
| `icon` | string | no | Icon location |

Environment variables in `destination`, `target`, and `icon` are expanded on the target. Parent directories are created.

```yaml
- name: Add kiosk shortcut
  shortcut:
    destination: "%PUBLIC%\\Desktop\\Kiosk.lnk"
    target: "C:\\Exhibits\\kiosk.exe"
    args: "--fullscreen"
```

### `scheduled_task`

Manages a scheduled task.

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `name` | string | yes | | Task name |
| `execute` | string | unless `ensure: absent` | | Executable |
| `path` | string | no | `\` | Task folder, such as `\Preflight\`; created when missing |
| `arguments` | string | no | | Arguments |
| `working_dir` | string | no | | Working directory |
| `trigger` | `startup`, `onlogon`, `daily`, `once` | no | `startup` | Trigger |
| `start_at` | string | with `trigger: once` | `03:00` for `daily` | Start time |
| `delay` | string | no | | Trigger delay for `startup` and `onlogon`: an ISO 8601 duration (`PT30S`) or a Go duration (`30s`) |
| `run_as` | string | no | | Run-as user. `SYSTEM`, `LOCALSERVICE`, and `NETWORKSERVICE` run as service accounts; other users use an interactive logon |
| `run_level` | `least`, `highest` | no | `least` | Privilege level |
| `enabled` | bool | no | `true` | Enabled state |
| `ensure` | `present`, `absent` | no | `present` | Desired state |

`Apply()` unregisters and re-registers the task.

```yaml
- name: Launch kiosk at logon
  scheduled_task:
    path: \Preflight\
    name: Kiosk
    execute: "C:\\Exhibits\\kiosk.exe"
    trigger: onlogon
    delay: 30s
    run_level: highest
```

### `user`

Manages a local user.

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `name` | string | yes | | User name |
| `password` | string | no | | Password; accepts `secret:<name>` |
| `groups` | string[] | no | | Local groups to add the user to |
| `ensure` | `present`, `absent` | no | `present` | Desired state |

- A new user without `password` is created with no password. For an existing user, omitting `password` leaves it unchanged.
- `groups` adds memberships and removes none.

```yaml
- name: Create exhibit user
  user:
    name: exhibit
    password: secret:exhibit-password
    groups: [Users]
```

### `power_plan`

Manages a named power scheme.

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `name` | string | yes | | Scheme name |
| `base` | string | no | `balanced` | Scheme to copy when creating: `balanced`, `high_performance`, `power_saver`, or a GUID |
| `activate` | bool | no | `true` | Activate the scheme |
| `settings` | list | no | | Setting overrides |
| `ensure` | `present`, `absent` | no | `present` | `absent` deletes the scheme, activating Balanced first if the scheme is active |

Each `settings` entry:

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `subgroup` | string | yes | Subgroup alias or GUID |
| `setting` | string | yes | Setting alias or GUID |
| `ac_value` | integer | no | Value on AC power |
| `dc_value` | integer | no | Value on battery |

```yaml
- name: Never sleep
  power_plan:
    name: Exhibit
    base: high_performance
    settings:
      - subgroup: SUB_SLEEP
        setting: STANDBYIDLE
        ac_value: 0
        dc_value: 0
```

### `windows_feature`

Enables or disables a Windows optional feature.

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `name` | string | yes | | Feature name |
| `ensure` | `present`, `absent` | no | `present` | Desired state |

- Changes do not restart Windows. Enabling uses `-LimitAccess`, so Windows Update is not contacted.
- Fails with `windows_feature not found: <name>` when the feature does not exist.
- Changes fail over WinRM; see [WinRM session limitations](#winrm-session-limitations).

```yaml
- name: Enable Telnet client
  windows_feature:
    name: TelnetClient
```

### `environment`

Manages an environment variable.

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `name` | string | yes | | Variable name |
| `value` | string | no | | Value |
| `scope` | `machine`, `user` | no | `machine` | `user` without `user` targets the connecting account |
| `user` | string | no | | Local user whose environment to edit; requires `scope: user`. The profile hive is loaded for the task |
| `ensure` | `present`, `absent` | no | `present` | Desired state |

`user` without `scope: user` fails with `environment: user requires scope: user`.

```yaml
- name: Set content root
  environment:
    name: EXHIBIT_CONTENT
    value: "C:\\Exhibits\\Content"
```

### `firewall_rule`

Manages a Windows Firewall rule, matched by display name.

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `name` | string | yes | | Rule display name |
| `direction` | `inbound`, `outbound` | no | | Direction |
| `action` | `allow`, `block` | no | | Action |
| `protocol` | `tcp`, `udp`, `any` | no | | Protocol |
| `ports` | integer, string, or list | no | | Local ports; a list is joined with commas |
| `ensure` | `present`, `absent` | no | `present` | Desired state |

```yaml
- name: Allow kiosk control port
  firewall_rule:
    name: Kiosk control
    direction: inbound
    action: allow
    protocol: tcp
    ports: 8080
```

When `direction` or `action` is unset, `Check()` reports a change on every run.

### `powershell`

Runs a PowerShell script in a new session.

| Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `script` | string | one of `script` or `file` | Inline script |
| `file` | string | one of `script` or `file` | Script path on the controller; its contents run on the target |
| `args` | string[] | no | Arguments for `file` |
| `check_script` | string | no | Script that decides whether a change is needed |
| `creates` | string | no | Path that, when it exists, means no change is needed |
| `working_dir` | string | no | Working directory; relative `creates` paths resolve from it |
| `env` | object | no | Process environment variables for the script, `check_script`, and `creates` |

- Without `check_script` or `creates`, the script runs every time.
- `check_script` takes precedence over `creates`. It must output a boolean, where `true` means a change is needed, or an object with `needs_change` and an optional `message`.

```yaml
- name: Register kiosk app
  powershell:
    check_script: |
      -not (Test-Path 'HKLM:\SOFTWARE\Kiosk')
    script: |
      New-Item -Path 'HKLM:\SOFTWARE\Kiosk' -Force | Out-Null
```

### `shell`

Runs a command with the PowerShell call operator: `& <cmd> <args>`.

| Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `cmd` | string | yes | Command or executable path |
| `args` | string[] | no | Arguments |
| `creates` | string | no | Path that, when it exists, means no change is needed |
| `working_dir` | string | no | Working directory; relative `creates` paths resolve from it |
| `env` | object | no | Environment variables set for the command; restored afterward |

- Without `creates`, the command runs every time.
- A non-zero exit code fails the task with `<cmd> exited with code <code>`.

```yaml
- name: Register license
  shell:
    cmd: "C:\\Exhibits\\license.exe"
    args: ["--register"]
    creates: "C:\\Exhibits\\license.dat"
```

### `reboot`

Schedules a restart with `shutdown /r /t <timeout>`.

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `timeout` | integer | no | `300` | Seconds before the restart |
| `condition` | `always`, `if_needed` | no | | Accepted but not applied |

- The task always reports a change.
- Preflight does not wait for the target to restart or reconnect. Tasks after `reboot` run while the restart is pending.

```yaml
- name: Restart in one minute
  reboot:
    timeout: 60
```

### `wait`

Waits until a condition is true.

| Parameter | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `condition` | `port_open`, `file_exists`, `service_running` | yes | | Condition |
| `target` | string | yes | | Condition target; see below |
| `timeout` | string | no | `5m` | Go duration, such as `30s` or `1h` |

| `condition` | `target` | True when |
| --- | --- | --- |
| `port_open` | `host:port` | A TCP connection succeeds within 2 seconds |
| `file_exists` | Path | The path exists |
| `service_running` | Service name | The service status is `Running` |

The condition is checked every 5 seconds, and the task fails with `wait: timeout after <timeout> waiting for condition "<condition>" on "<target>"` when the timeout passes. A condition that is already true reports no change.

```yaml
- name: Wait for the content server
  wait:
    condition: port_open
    target: "localhost:8080"
    timeout: 2m
```

## Related docs

- [Playbook and action YAML reference](./playbooks.md)
- [Error reference](./errors.md)
- [Targets, transports, and plugins](../explanation/targets-and-transports.md)
