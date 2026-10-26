# Playbook and action YAML reference

Playbooks and actions are validated against the schemas in [`schema/`](https://github.com/bluecadet/preflight/tree/main/schema). Unknown fields are errors.

## Playbook

| Field | Type | Description |
| --- | --- | --- |
| `name` | string | Playbook name, shown in output and used in bundle file names |
| `description` | string | Description |
| `vars` | object | Playbook variables |
| `import` | string[] | Playbook files to merge before this playbook |
| `tasks` | task[] | Tasks |

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
```

### Imports

- Relative paths resolve from the importing file.
- Imports load depth-first, in listed order.
- Imported tasks run before the importing playbook's tasks.
- Variables merge by top-level key: later imports override earlier ones, and the importing playbook overrides all imports. Nested maps are replaced, not merged.
- The importing playbook's `name` and `description` are kept.
- An import cycle fails with `playbook: import cycle detected: <a> -> <b> -> <a>`.

## Action

An action is an `action.yml` file that packages tasks behind named inputs.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | yes | Action name, optionally namespaced, such as `myorg/display-config`; letters, digits, `.`, `_`, `-`, and `/` |
| `tasks` | task[] | yes | Tasks |
| `version` | string | no | Semantic version, such as `1.2.0` |
| `description` | string | no | Description |
| `author` | string | no | Author |
| `inputs` | object | no | Input names mapped to input definitions |

### Input definition

| Field | Type | Description |
| --- | --- | --- |
| `type` | enum | `string`, `bool`, `int`, `path`, or `list`; documentation only, values are not checked |
| `required` | bool | Fail planning with `required input "<name>" is missing` when the value is absent or empty |
| `default` | any | Value used when the task's `with` omits the input |
| `description` | string | Description |

Inside the action, inputs are read as `vars.<input>`. See [Variables](./templating.md#variables) for scoping.

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
  domain:
    type: string
    default: "."

tasks:
  - name: Enable auto-login
    registry:
      path: 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Winlogon'
      values:
        - name: AutoAdminLogon
          data: "1"
        - name: DefaultUserName
          data: "{{ vars.username }}"
        - name: DefaultPassword
          data: "{{ vars.password }}"
        - name: DefaultDomainName
          data: "{{ vars.domain }}"
```

## Task

Each task uses exactly one form:

- `uses` with optional `with`: run an action
- `module` with optional `params`: run a module by name, including plugin modules
- one inline module key, such as `directory` or `service`: run a built-in module

| Field | Type | Description |
| --- | --- | --- |
| `name` | string | Required. Task label in output; the dependency key when `id` is unset |
| `id` | string | Dependency key |
| `uses` | string | Action ref; see [Action refs](#action-refs) |
| `with` | object | Action inputs |
| `module` | string | Module name |
| `params` | object | Module parameters; requires `module` |
| `when` | string | Condition; see [`when` conditions](./templating.md#when-conditions) |
| `depends_on` | string[] | Keys of tasks that must run first |
| `ignore_errors` | bool | Continue the run when this task fails |
| `tags` | string[] | Tags matched by `--tags` and `--skip-tags` |
| `<module>` | object | Inline built-in module parameters; see [Built-in module reference](./modules.md) |

Setting more than one form fails, for example `task "<name>": cannot set both 'uses' and 'module'`.

```yaml
tasks:
  - name: Prepare content volume
    id: prepare-volume
    directory:
      path: "C:\\Content"

  - name: Sync content
    module: signage_sync
    depends_on: [prepare-volume]
    params:
      source: "\\\\nas01\\content"
      destination: "C:\\Content"
```

### Ordering and dependencies

Tasks run one at a time on each target. Tasks without `depends_on` run in file order. A task with `depends_on` runs after the tasks it names, which can place it later than its position in the file. A dependency key is the task's `id`, or its `name` when `id` is unset, and resolves only among tasks in the same playbook or action.

Planning fails when two tasks share a key, a key is unknown, or dependencies form a cycle.

When a dependency fails without `ignore_errors`, the dependent task is skipped with reason `dependency-failed`.

## Action refs

`uses` resolves through these sources, in order:

| Source | Ref form | Location |
| --- | --- | --- |
| Stdlib | `preflight/<name>` | Embedded in the binary; see [Stdlib action reference](./stdlib-actions.md) |
| Project | `<name>` or `<org>/<name>`, with no `.` in the first segment and no `@` | `<project>/actions/<ref>/action.yml` |
| Cache | Remote ref | `~/.preflight/actions` |
| Git | Remote ref | Fetched over HTTPS |

A ref that no source resolves fails with `action "<ref>" not found (tried: <sources>)`.

### Remote action refs

```text
<host>/<org>/<repo>[/<path>]@<revision>
```

```text
github.com/acme/actions/signage@v1.2.3
github.com/acme/actions/collections/autologin@0123456789abcdef
```

- `<revision>` is a branch, tag, or commit SHA. `<path>` is the directory in the repository that holds `action.yml`.
- Preflight clones `https://<host>/<org>/<repo>.git`, then `https://<host>/<org>/<repo>`.
- `apply`, `check`, and `stage` fetch uncached refs, including refs used by fetched actions, and record each resolved commit SHA in `preflight.lock` in the project directory. `preflight action fetch` does the same without running.
- A ref already pinned in `preflight.lock` resolves to the pinned SHA.
- `plan` does not fetch. An uncached ref fails with `remote action "<ref>" is not cached`.

See [Update and pin actions](../how-to/update-and-pin-actions.md).

## Editor schemas

Editors that use the YAML Language Server validate files against these schema IDs:

| File | Schema ID |
| --- | --- |
| Playbook | `https://preflight.dev/schema/playbook.schema.json` |
| `action.yml` | `https://preflight.dev/schema/action.schema.json` |
| `preflight.yml` | `https://preflight.dev/schema/config.schema.json` |

Per file:

```yaml
# yaml-language-server: $schema=https://preflight.dev/schema/action.schema.json
```

VS Code `settings.json`:

```json
{
  "yaml.schemas": {
    "https://preflight.dev/schema/action.schema.json": "**/actions/**/action.yml",
    "https://preflight.dev/schema/playbook.schema.json": "**/playbooks/*.yml",
    "https://preflight.dev/schema/config.schema.json": "**/preflight.yml"
  }
}
```

## Related docs

- [Templating and facts reference](./templating.md)
- [Project config reference](./config.md)
- [Execution model](../explanation/execution-model.md)
