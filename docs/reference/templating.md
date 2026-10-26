# Templating and facts reference

## Placeholders

A `{{ expression }}` placeholder renders inside task names, `when` conditions, and parameter values at any depth of nested maps and lists.

```text
{{ vars.content_root }}
{{ facts.os.arch }}
{{ target.name }}
{{ env.COMPUTERNAME }}
```

Expressions use [expr](https://expr-lang.org) syntax with these Preflight rules:

| Syntax | Behavior |
| --- | --- |
| `a.b.c` | Dot-path lookup in a namespace |
| `==`, `!=` | Compare the string forms of both operands |
| `>`, `>=`, `<`, `<=` | Compare numerically; a non-numeric operand is an error |
| `cond ? a : b` | Ternary; `cond` uses the truthy rules in [`when` conditions](#when-conditions) |
| `&&`, `\|\|`, `!` | Boolean logic |
| `'text'` | String literal |
| `secret("name")` | Secret contents; valid only in `file` `content_template` |

```yaml
when: "{{ vars.theme_mode != 'unchanged' }}"
data: "{{ vars.theme_mode == 'light' }}"
```

A rendered value that contains another placeholder is rendered again, up to 16 passes.

When a parameter value is exactly one placeholder and the result is a map or list, the value keeps that type. Other results render as strings.

## Namespaces

| Namespace | Contents | Unknown key |
| --- | --- | --- |
| `vars` | Merged variables | Error: `undefined variable "vars.<path>"` |
| `facts` | Gathered target facts | Empty string |
| `target` | Target connection metadata | Empty string |
| `env` | Target environment variables | Empty string |

Any other first segment renders as an empty string.

During `plan`, placeholders that reference unknown `facts`, `target`, or `env` keys stay unrendered, including comparisons and ternaries that read them. `check` and `apply` render them against the live target. Staged bundles keep them for apply time.

## Variables

`vars` holds the merged project, inventory, group, host, playbook, and `--var` layers. See [variable merge order](./inventory.md#variable-merge-order).

Inside an action, `vars` also holds the action's inputs: input defaults overlaid by the task's `with` values. The overlay is shallow; a `with` map replaces a variable of the same name.

### Built-in `preflight` variables

| Variable | Value |
| --- | --- |
| `vars.preflight.project` | `project` from `preflight.yml` |
| `vars.preflight.environment` | `environment` from `preflight.yml` |

Playbook vars and `--var` can override these; inventory, group, and host vars cannot.

## `when` conditions

`when` renders its template, trims the result, and lowercases it. These results are false and skip the task:

- empty string
- `false`
- `0`
- `no`

Any other result is true.

```yaml
when: "{{ vars.computer_name }}"
when: "{{ facts.os.build >= 22000 }}"
```

## Secrets in templates

A string that is exactly `secret:<name>` resolves in any parameter; see [Secret references](./config.md#secret-references).

`secret("name")` works only in the `content_template` parameter of the `file` module, which renders after all other parameters. Anywhere else it fails with `secret lookup is not configured`.

## Facts

`preflight facts` prints these values, and `check` and `apply` gather them for templates. Gathering is best-effort: a failed probe logs a warning and leaves its fields empty.

| Field | Description |
| --- | --- |
| `facts.os.family` | `windows` or `unknown` |
| `facts.os.name` | OS product name |
| `facts.os.version` | OS version string |
| `facts.os.build` | Build number |
| `facts.os.arch` | Architecture |
| `facts.os.hostname` | Host name reported by the OS |
| `facts.hostname` | Host name |
| `facts.disks` | List of disks |
| `facts.env` | Environment variables |

Each `facts.disks` entry:

| Field | Description |
| --- | --- |
| `path` | Drive path |
| `total_gb` | Capacity in GB |
| `free_gb` | Free space in GB |
| `used_gb` | Used space in GB |

## Target metadata

`target` holds connection metadata and no credentials.

| Field | Description |
| --- | --- |
| `target.name` | Inventory host name |
| `target.hostname` | Inventory host name |
| `target.address` | `address`, or the host name when `address` is unset |
| `target.transport` | `local`, `ssh`, or `winrm` |
| `target.port` | Connection port |

For the local machine, `name`, `hostname`, and `address` are `localhost`, `transport` is `local`, and `port` is unset.

## Related docs

- [Project config reference](./config.md)
- [Inventory reference](./inventory.md)
- [Execution model](../explanation/execution-model.md)
