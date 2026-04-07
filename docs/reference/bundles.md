# Bundle Reference

This page describes the staged offline bundle format implemented by [`internal/bundle/`](/Users/clay/repos/preflight/internal/bundle).

## Purpose

A bundle is a self-contained zip archive that lets you apply a staged execution plan on another machine without re-reading the original playbook or refetching actions.

## Archive Contents

Every bundle contains:

- `manifest.json`
- `plan.json` — the staged execution plan, with the task DAG and module names resolved before bundling; template expressions are preserved and rendered at apply time, including conditions (`when:`), task name templates, and parameters that reference `target`, `facts`, or `env` values from the live execution context
- the runtime binary under `runtime/`
- zero or more plugin executables under `plugins/`

## Bundle Filename

Bundle filenames are derived from:

- playbook name
- target name
- target OS
- target architecture

The generated name is sanitized and ends in `.zip`.

## `manifest.json`

The manifest includes:

| Field | Type | Meaning |
| --- | --- | --- |
| `format_version` | integer | Bundle format version |
| `created_at` | timestamp | Creation time |
| `playbook_name` | string | Source playbook name |
| `target_name` | string | Target name used during staging |
| `target_os` | string | OS reported by the target |
| `target_arch` | string | Architecture reported by the target |
| `runtime_binary` | string | Relative path to the staged runtime binary |
| `build` | object | Version, commit, and build date of the staging binary |
| `modules` | array | Referenced built-in and plugin modules |
| `checksums` | object | File checksum map |
| `lock_entries` | array | Pinned remote action refs from `preflight.lock` |

Each `modules[]` entry records:

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string | Module name |
| `kind` | string | `builtin` or `plugin` |
| `path` | string | Relative plugin path when `kind` is `plugin` |
| `version` | string | Plugin version when available |

## Staging Constraints

Staging fails when:

- a task references an unknown module
- a referenced plugin cannot be initialized or copied
- a task preview contains secret values that would need to be embedded in the bundle

## Bundle Apply

`preflight apply --bundle <bundle.zip>`:

1. extracts the bundle to a temporary directory
2. loads `manifest.json`
3. reads `plan.json`
4. builds a module registry from built-ins plus bundled plugins
5. executes the bundled plan locally

Bundle apply is intentionally isolated from the normal project layout.

## Related Docs

- [Stage bundles for air-gapped deployment](../how-to/air-gapped-deployment.md)
- [Plugin reference](./plugins.md)
