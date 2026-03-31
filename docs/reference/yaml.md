# YAML Reference

This page summarizes the YAML files and task shapes currently defined by the repository schemas and parsers.

## `preflight.yml`

Project config file used for project metadata, shared variables, and secrets.

### Fields

| Field | Type | Description |
| --- | --- | --- |
| `project` | string | Project name identifier |
| `environment` | string | Deployment environment |
| `vars` | object | Project-level variables |
| `secrets.identity` | string | Age identity file path |
| `secrets.recipients` | string[] | Age recipients used for encryption |
| `secrets.entries` | map | Logical secret name to encrypted file metadata |

### Example

```yaml
project: natural-history-museum
environment: production

vars:
  content_root: "C:\\Exhibits\\content"

secrets:
  identity: ".age/keys.txt"
  recipients:
    - "age1example..."
  entries:
    autologin-password:
      file: "secrets/autologin-password.age"
```

## `inventory.yml`

Inventory groups hosts and variables.

### Top-Level Shape

```yaml
groups:
  all:
    vars:
      timezone: "America/New_York"

  lobby:
    vars:
      resolution: "3840x2160"
    hosts:
      - name: lobby-pc-01
        address: 192.168.1.10
        transport: winrm
        username: exhibit-admin
        password_from: secret:winrm-password
```

### Host Fields

| Field | Type | Notes |
| --- | --- | --- |
| `name` | string | Required |
| `address` | string | Hostname or IP |
| `transport` | enum | `winrm`, `ssh`, `local` |
| `port` | int | Optional explicit port |
| `username` | string | Optional |
| `password` | string | Optional |
| `password_from` | string | Secret reference |
| `private_key` | string | SSH key path |
| `private_key_from` | string | Secret reference |
| `https` | bool | WinRM over TLS |
| `vars` | object | Host-level variable overrides |

> [!TIP]
> Variable merging in inventory is `all` group vars, then group vars, then host vars.

### Selector Resolution

Inventory-backed commands resolve selectors from `--target` using these rules:

- A selector may be a host name, a group name, or `all`.
- Repeating `--target` builds a union of matches.
- Hosts are deduplicated by name.
- The first match wins when the same host is selected more than once.

## `playbook.yml`

Playbooks are top-level execution documents.

### Fields

| Field | Type | Description |
| --- | --- | --- |
| `name` | string | Human-readable playbook name |
| `description` | string | Optional description |
| `vars` | object | Playbook-level variable overrides |
| `import` | string[] | Other playbooks to merge before local tasks |
| `tasks` | task[] | Ordered task list |

### Example

```yaml
name: lobby-baseline

vars:
  shell_path: /bin/sh

tasks:
  - name: Ensure app directory exists
    directory:
      path: "./tmp/app"
      ensure: present

  - name: Run bootstrap script
    shell:
      cmd: "{{ vars.shell_path }}"
      args:
        - -c
        - echo "bootstrap"
```

> [!NOTE]
> Imports are merged depth-first, in listed order, before the importing playbook's own tasks. Import paths are resolved relative to the playbook file that declares them.

### Variable Precedence At Runtime

For inventory-backed execution, later entries win:

```text
project vars
  -> inventory group vars
    -> inventory host vars
      -> playbook vars
        -> CLI --var flags
```

## Task Shape

Each task must have a `name`. It can either:

- reference another action with `uses`
- call an explicit module with `module` and `params`
- declare one inline module block

### Shared Task Fields

| Field | Type | Description |
| --- | --- | --- |
| `name` | string | Required task label |
| `uses` | string | Action reference |
| `module` | string | Explicit module name, including plugin-backed modules |
| `params` | object | Parameters for `module` |
| `with` | object | Inputs for `uses` |
| `when` | string | Template condition |
| `depends_on` | string[] | Dependencies by task name |
| `ignore_errors` | bool | Continue on failure |
| `tags` | string[] | Task tags |

### Explicit Module Tasks

Use `module` plus `params` when you want to call a module by name instead of using an inline module block.

Example:

```yaml
tasks:
  - name: Sync signage content
    module: signage_sync
    params:
      source: "\\\\nas01\\signage"
      destination: "C:\\Signage"
```

This is the supported way to invoke plugin-backed modules from playbooks and actions.

### Template Context

Task templates can read from these namespaces:

| Namespace | What it contains |
| --- | --- |
| `vars.*` | Merged variables for the selected host |
| `facts.*` | Gathered host facts, available during execution |
| `target.*` | Safe target metadata: `name`, `hostname`, `address`, `transport`, `port` |
| `env.*` | Gathered target environment variables during execution |

`plan` stays a pure phase, so fact-dependent expressions may remain unresolved in plan output until execution time.

> [!WARNING]
> A task cannot mix `uses`, `module`, and inline module blocks. Choose exactly one task execution form. A task also cannot set more than one inline module block.

## Inline Modules In The Schema

The schema currently defines these inline module keys:

| Module Key | Purpose |
| --- | --- |
| `registry` | Manage registry keys and values |
| `service` | Manage Windows services |
| `file` | Manage files |
| `directory` | Manage directories |
| `package` | Manage package installers |
| `shortcut` | Manage `.lnk` shortcuts |
| `scheduled_task` | Manage Windows scheduled tasks |
| `user` | Manage users |
| `windows_feature` | Manage Windows features |
| `environment` | Manage environment variables |
| `firewall_rule` | Manage firewall rules |
| `powershell` | Run PowerShell |
| `shell` | Run shell commands |
| `reboot` | Request a reboot |
| `wait` | Wait for a condition |

## Modules Currently Registered In Code

The module registry currently includes:

| Module Key | Status |
| --- | --- |
| `file` | Implemented |
| `directory` | Implemented |
| `powershell` | Implemented |
| `shell` | Implemented |
| `environment` | Implemented |
| `wait` | Implemented |
| `reboot` | Implemented |
| `registry` | Implemented on Windows; stubbed elsewhere |
| `service` | Implemented on Windows; stubbed elsewhere |
| `package` | Implemented on Windows; stubbed elsewhere |
| `shortcut` | Implemented on Windows; stubbed elsewhere |
| `scheduled_task` | Implemented on Windows; stubbed elsewhere |
| `user` | Implemented on Windows; stubbed elsewhere |
| `windows_feature` | Implemented on Windows; stubbed elsewhere |
| `firewall_rule` | Implemented on Windows; stubbed elsewhere |

Windows-only modules remain non-functional on non-Windows hosts, where the registry exposes explicit stubs that fail fast.

Plugin-backed modules are discovered at runtime and therefore are not enumerated in the static schema. Use `module: <plugin-name>` with `params` for those tasks.

## `action.yml`

Actions package reusable tasks behind typed inputs.

### Fields

| Field | Type | Description |
| --- | --- | --- |
| `name` | string | Required namespaced action name |
| `version` | string | Semantic version string |
| `description` | string | Optional description |
| `author` | string | Optional author |
| `inputs` | map | Named input definitions |
| `outputs` | map | Named output definitions |
| `tasks` | task[] | Ordered task list |

### Example

```yaml
name: preflight/autologin
version: "1.0.0"
description: Configure Windows automatic login

inputs:
  username:
    type: string
    required: true
  password_from:
    type: string
    required: false

tasks:
  - name: Enable auto-login
    registry:
      path: 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Winlogon'
      values:
        AutoAdminLogon: "1"
        DefaultUserName: "{{ vars.username }}"
        DefaultPassword: "{{ vars.password }}"
```

## Action Resolution Order

When Preflight resolves a `uses:` reference, it checks:

1. Embedded stdlib
2. Local `actions/` under the project directory
3. User cache at `~/.preflight/actions`
4. Git resolver

The Git resolver is present in the chain, but remote fetch is still a stub.
