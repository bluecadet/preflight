# Plugin Reference

This page describes how Preflight discovers, invokes, and stages executable plugins.

## Purpose

Plugins extend the module library without using Go shared objects. Each plugin is a standalone executable that speaks JSON-RPC over stdin and stdout.

## Discovery

Preflight scans plugin directories in this order:

1. The directory alongside the `preflight` binary
2. `~/.preflight/plugins`
3. `./plugins` relative to the working directory

For staged bundle apply, Preflight uses the bundle-local `plugins/` directory first and isolates execution to the staged payload.

## File Naming

Plugin executables must use the `preflight-plugin-<name>` prefix.

Examples:

- `preflight-plugin-signage_sync`
- `preflight-plugin-signage_sync.exe`

On Windows, the executable must end with `.exe`.

## Task Invocation

Plugin-backed tasks use the explicit module form:

```yaml
tasks:
  - name: Run plugin task
    module: signage_sync
    params:
      source: "\\\\nas01\\signage"
```

Preflight treats that plugin exactly like a module task in the runner:

- `Check` runs first
- `Apply` runs only when `Check` reports that change is needed
- dry-run stays on the `Check` side

## JSON-RPC Methods

The runner uses these methods:

| Method | Purpose |
| --- | --- |
| `initialize` | Report plugin name and version |
| `check` | Return whether change is needed |
| `apply` | Perform the change |

The Go SDK for plugin authors lives in `pkg/plugin/sdk`.

## Initialization Rules

Preflight initializes discovered plugins before registering them for execution.

Registration fails when:

- the plugin cannot be started
- `initialize` fails
- two plugins report the same logical name
- a plugin name conflicts with a built-in module name

## Offline Bundles

When a staged plan references a plugin-backed module, the bundle includes:

- the plugin executable
- module metadata in the bundle manifest

Staging fails if the referenced plugin cannot be initialized or copied into the bundle.

## Related Commands

| Command | Purpose |
| --- | --- |
| `preflight plugin list` | Show discovered plugins and initialization status |
| `preflight plugin info <name>` | Show one plugin's path, source, version, and initialization state |

## Related Docs

- [Use Plugin Modules In Playbooks](../how-to/use-plugin-modules.md)
- [CLI reference](./cli.md)
- [YAML reference](./yaml.md)
