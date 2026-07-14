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
| `vars.preflight.*` | Built-in project metadata (see below) |

## Variable Precedence

`vars.*` exposes the merged result of the project, inventory, group, host,
playbook, and CLI variable layers. The authoritative merge order lives in
the [inventory reference](./inventory.md#variable-merge-order).

Undefined `vars.*` references are treated as errors so missing inventory,
playbook, project, or CLI-provided values fail early instead of rendering as
empty strings.

## Built-In `preflight` Variables

The runner injects a `preflight` map into the project variable layer so playbooks can reference project metadata without repeating it in `vars`:

| Variable | Meaning |
| --- | --- |
| `vars.preflight.project` | `project` field from `preflight.yml` |
| `vars.preflight.environment` | `environment` field from `preflight.yml` |

These are injected above host vars in the [merge order](./inventory.md#variable-merge-order), so playbook vars and CLI `--var` flags can override them, but inventory, group, and host vars cannot.

## Planning Versus Execution

The runner renders templates in two different contexts:

- During `plan`, unknown `facts.*` and `target.*` expressions are preserved so planning stays pure.
- During `check` and `apply`, the runner gathers facts and target metadata, then renders task names, `when:` conditions, and parameters for real execution.
- Missing `vars.*` references fail in every phase because they indicate incomplete configuration, not unavailable runtime metadata.

That is why `plan` may still show `{{ facts... }}` placeholders while `check` and `apply` do not.

## `when:` Conditions

`when:` uses the same template engine, then interprets the rendered result with truthy semantics:

- **False** (task skipped): empty string, `false`, `0`, `no`
- **True** (task runs): anything else

This means `when:` works naturally for both boolean inputs and optional string inputs:

```yaml
# gate on a boolean input
when: "{{ vars.winget_enabled }}"

# gate on an optional string — skips if computer_name was not provided
when: "{{ vars.computer_name }}"
```

Preflight does not currently support comparison or arithmetic expressions such as `{{ vars.count > 1 }}` in `when:`. Action authors should pass a boolean input or compute the decision inside a module-specific script.

## Facts Shape

The facts command and execution-time fact gathering expose this structure:

| Field | Meaning |
| --- | --- |
| `facts.os.family` | `windows`, `linux`, `darwin`, or `unknown` |
| `facts.os.name` | os-release ID on POSIX (e.g. `ubuntu`, `rocky`); friendly name on Windows |
| `facts.os.version` | os-release VERSION_ID on POSIX; raw version string on Windows |
| `facts.os.build` | Numeric build number (Windows-only; `0` elsewhere) |
| `facts.os.arch` | Architecture |
| `facts.os.hostname` | Hostname from OS facts |
| `facts.os.package_manager` | `apt` or `dnf` on POSIX; empty on Windows and when none is found |
| `facts.os.init` | `systemd` on POSIX hosts running systemd; empty otherwise |
| `facts.hostname` | Top-level hostname |
| `facts.disks` | List of disk objects |
| `facts.env` | Environment-variable map |

### Absent signals

Every `facts.os.*` key is always present, even when its underlying signal is
absent. An absent signal renders as an empty string (`build` as `0`), never as
a missing key. This lets playbooks branch on `{{ facts.os.package_manager }}`
and similar without distinguishing "not detected" from "empty":

```yaml
# only install the systemd unit on hosts that actually run systemd
when: "{{ facts.os.init }}"
# only render this task on apt hosts via a vars boolean computed elsewhere
when: "{{ vars.is_apt_host }}"
```

On Windows the POSIX-only facts (`package_manager`, `init`) are always empty,
and `family` is `windows`. `build` stays Windows-only (it is `0` on POSIX).

Because `when:` does not support comparison expressions, branch on a host's
package manager or family by rendering the fact and letting the playbook's
variable layer turn it into a boolean, or gate on the truthiness of the value
itself:

Each disk entry includes:

| Field | Meaning |
| --- | --- |
| `path` | Drive or filesystem path |
| `total_gb` | Total capacity in gigabytes |
| `free_gb` | Free space in gigabytes |
| `used_gb` | Used space in gigabytes |

## Fact Gathering Behavior

Fact gathering is best-effort:

- OS facts come from `Target.Info()`, which on POSIX hosts is backed by a single
  lazily-run, per-target detection probe (cached for the run). The probe roster
  is exactly: os-release ID/VERSION_ID, `command -v apt-get` / `command -v dnf`,
  `test -d /run/systemd/system`, plus hostname / `uname -s` / `uname -m`. Both
  `Info()` and the facts gatherer read the cached probe result, so there is no
  second detection path.
- The probe is defensive per-signal: a missing source (e.g. no os-release on
  macOS) empties that field without failing the probe. Only a transport-level
  failure surfaces as an error through `Info()`.
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
