# Playbook And Action YAML Reference

This page describes the YAML shapes used for playbooks, actions, and tasks. The schemas live in [`schema/`](/Users/clay/repos/preflight/schema), and the runtime loaders live in [`internal/action/`](/Users/clay/repos/preflight/internal/action).

## `playbook.yml`

Playbooks are the top-level execution documents.

### Fields

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string | Human-readable playbook name |
| `description` | string | Optional description |
| `vars` | object | Playbook-level variable overrides |
| `import` | string[] | Other playbook files to merge before local tasks |
| `tasks` | task[] | Ordered task list |

### Import Behavior

Imports are loaded depth-first.

- Imported vars are merged first.
- The importing playbook’s vars override imported vars.
- Imported tasks are prepended in listed order.
- Import cycles are rejected.
- Relative import paths are resolved from the playbook that declares them.

### Example

```yaml
name: lobby-baseline

import:
  - ./base.yml

vars:
  content_root: "C:\\Exhibits\\Lobby"

tasks:
  - name: Prepare content directory
    directory:
      path: "{{ vars.content_root }}"
      ensure: present
```

## `action.yml`

Actions package reusable tasks behind a typed input surface.

### Fields

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string | Required namespaced action name such as `myorg/display-config` |
| `version` | string | Semantic version string |
| `description` | string | Optional description |
| `author` | string | Optional author |
| `inputs` | object | Named input definitions |
| `outputs` | object | Named output definitions |
| `tasks` | task[] | Ordered task list |

### Input Definition Fields

| Field | Type | Meaning |
| --- | --- | --- |
| `type` | enum | `string`, `bool`, `int`, or `path` |
| `required` | bool | Whether the caller must supply the input |
| `default` | any | Default value injected before caller-provided values |
| `description` | string | Human-readable explanation |

### Example

```yaml
name: preflight/autologin
version: "1.0.0"
description: Configure Windows automatic login

inputs:
  username:
    type: string
    required: true
  password:
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

## Task Shape

Every task requires `name`. A task may use exactly one execution form:

- `uses` plus `with`
- `module` plus `params`
- one inline module block such as `directory`, `service`, or `powershell`

Mixing these forms in the same task is an error.

### Shared Task Fields

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string | Task label used in output and `depends_on` |
| `uses` | string | Action reference |
| `with` | object | Inputs passed to the referenced action |
| `module` | string | Explicit module name, including plugin-backed modules |
| `params` | object | Parameters for `module` |
| `when` | string | Template condition expression |
| `depends_on` | string[] | Task-name dependencies |
| `ignore_errors` | bool | Continue after a task failure |
| `tags` | string[] | Tags used by `--tags` and `--skip-tags` |

### Explicit Module Tasks

Use `module` and `params` for plugin-backed modules or when you want an explicit module name:

```yaml
tasks:
  - name: Sync signage
    module: signage_sync
    params:
      source: "\\\\nas01\\signage"
      destination: "C:\\Signage"
```

### Inline Module Tasks

Use one inline module key when you want a built-in task form:

```yaml
tasks:
  - name: Ensure content directory exists
    directory:
      path: "C:\\Exhibits\\Content"
      ensure: present
```

For the exact built-in module fields, see [Built-in module reference](./modules.md).

## Template Context

Task names, `when:` expressions, and string parameter values may read from:

| Namespace | Meaning |
| --- | --- |
| `vars.*` | Merged variables |
| `facts.*` | Gathered host facts |
| `target.*` | Safe target metadata |
| `env.*` | Gathered target environment variables |

The template engine supports simple dot-path lookups such as `{{ vars.content_root }}`. It renders string values recursively through nested maps and lists, which lets actions template shapes such as registry value lists and power-plan setting arrays. It does not implement the full Jinja filter and expression language.

## Action Resolution Order

When Preflight resolves a `uses:` reference, it checks these sources in order:

1. Embedded stdlib
2. Local project actions under `actions/`
3. User cache under `~/.preflight/actions`
4. Git-backed remote refs

## Remote Action Refs

Supported remote refs use this shape:

```text
host/org/repo[/path/to/action]@revision
```

Examples:

```text
github.com/acme/actions/signage@v1.2.3
github.com/acme/actions/collections/autologin@0123456789abcdef
```

Remote refs are pinned to exact commit SHAs in `preflight.lock` for reproducible resolution.

## Editor Schema Wiring

The JSON schemas in `schema/` enable live validation and autocompletion in editors that support the YAML Language Server protocol (e.g. VS Code with the [YAML extension](https://marketplace.visualstudio.com/items?itemName=redhat.vscode-yaml)).

**Inline file comment** (works in any editor with yaml-language-server support):

```yaml
# yaml-language-server: $schema=https://preflight.dev/schema/action.schema.json
name: myorg/my-action
...
```

**VS Code `settings.json`** (applies to all matching files in the workspace):

```json
{
  "yaml.schemas": {
    "https://preflight.dev/schema/action.schema.json": "**/actions/**/action.yml",
    "https://preflight.dev/schema/playbook.schema.json": "**/playbooks/*.yml",
    "https://preflight.dev/schema/inventory.schema.json": "**/inventory.yml",
    "https://preflight.dev/schema/config.schema.json": "**/preflight.yml"
  }
}
```

## Related Docs

- [Project config reference](./config.md)
- [Inventory reference](./inventory.md)
- [Actions, stdlib, and lockfiles](../explanation/actions-and-lockfiles.md)
