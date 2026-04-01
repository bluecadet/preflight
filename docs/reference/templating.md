# Templating And Facts Reference

This page describes the template engine in [`internal/template/`](/Users/clay/repos/preflight/internal/template) and the facts model in [`internal/facts/`](/Users/clay/repos/preflight/internal/facts).

## Template Syntax

Preflight uses a lightweight Jinja-like placeholder form:

```text
{{ vars.content_root }}
{{ facts.os.arch }}
{{ target.name }}
{{ env.PATH }}
```

Supported behavior:

- dot-path lookups only
- string substitution inside task names and parameter values
- recursive rendering through nested maps and lists
- boolean rendering for `when:`

Important limit:

- The engine is intentionally small. It does not implement the full Jinja expression or filter language.

## Namespaces

| Namespace | Meaning |
| --- | --- |
| `vars.*` | Merged variables |
| `facts.*` | Gathered host facts |
| `target.*` | Safe target metadata |
| `env.*` | Gathered target environment variables |

## Variable Precedence

The merge layers are implemented by the variable store in this order:

```text
defaults
  -> project vars
    -> inventory group vars
      -> inventory host vars
        -> playbook vars
          -> CLI --var flags
```

At runtime, the effective merged variable map is exposed through `vars.*`.

## Planning Versus Execution

The runner renders templates in two different contexts:

- During `plan`, unknown `facts.*` and `target.*` expressions are preserved so planning stays pure.
- During `check` and `apply`, the runner gathers facts and target metadata, then renders task names, `when:` conditions, and parameters for real execution.

That is why `plan` may still show `{{ facts... }}` placeholders while `check` and `apply` do not.

## `when:` Conditions

`when:` uses the same template engine, then parses the rendered result as a boolean.

Accepted values include:

- `true` and `false`
- `1` and `0`
- `yes` and `no`

If the rendered value is not a parseable boolean, the task errors.

Preflight does not currently support comparison or arithmetic expressions such as `{{ vars.count > 1 }}` in `when:`. Action authors should pass a boolean input or compute the decision inside a module-specific script.

## Facts Shape

The facts command and execution-time fact gathering expose this structure:

| Field | Meaning |
| --- | --- |
| `facts.os.name` | Friendly operating system name |
| `facts.os.version` | Raw version string |
| `facts.os.build` | Numeric build when available |
| `facts.os.arch` | Architecture |
| `facts.os.hostname` | Hostname from OS facts |
| `facts.hostname` | Top-level hostname |
| `facts.disks` | List of disk objects |
| `facts.env` | Environment-variable map |

Each disk entry includes:

| Field | Meaning |
| --- | --- |
| `path` | Drive or filesystem path |
| `total_gb` | Total capacity in gigabytes |
| `free_gb` | Free space in gigabytes |
| `used_gb` | Used space in gigabytes |

## Fact Gathering Behavior

Fact gathering is best-effort:

- OS facts come from `Target.Info()`.
- Windows disk and environment facts are collected through PowerShell.
- Local non-Windows disk facts are gathered from the local filesystem.
- Partial fact collection still returns a usable result set; individual failures are logged as warnings.

## Safe Target Metadata

`target.*` intentionally exposes only non-secret transport metadata:

| Field | Meaning |
| --- | --- |
| `target.name` | Inventory host name |
| `target.hostname` | Hostname used for safe template metadata |
| `target.address` | Address or fallback host name |
| `target.transport` | `local`, `winrm`, or `ssh` |
| `target.port` | Port, when defined |

Authentication fields such as passwords and private keys are not exposed through `target.*`.

## Related Docs

- [Project config reference](./config.md)
- [Inventory reference](./inventory.md)
- [Execution model](../explanation/execution-model.md)
