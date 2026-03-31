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

## Task Shape

Each task must have a `name`. It can either:

- reference another action with `uses`
- declare one inline module block

### Shared Task Fields

| Field | Type | Description |
| --- | --- | --- |
| `name` | string | Required task label |
| `uses` | string | Action reference |
| `with` | object | Inputs for `uses` |
| `when` | string | Template condition |
| `depends_on` | string[] | Dependencies by task name |
| `ignore_errors` | bool | Continue on failure |
| `tags` | string[] | Task tags |

> [!WARNING]
> A task cannot set both `uses` and an inline module. It also cannot set more than one inline module block.

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
