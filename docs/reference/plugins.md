# Plugin Reference

This page describes how Preflight discovers, initializes, executes, and stages executable plugins.

## Purpose

Preflight plugins extend the module library without using Go `.so` plugins, which are not a practical fit for Windows. A plugin is a standalone executable that speaks JSON-RPC over stdin and stdout.

The runner treats a plugin-backed module like any other module:

- `Check()` runs first
- `Apply()` runs only when change is needed
- dry-run stays on the `Check()` side

## Discovery

Plugins are scanned in this order:

1. The directory alongside the `preflight` binary
2. `~/.preflight/plugins`
3. `./plugins` relative to the working directory

During staged bundle apply, Preflight uses the bundle-local `plugins/` directory and can isolate discovery to that payload.

## File Naming

Plugin executables must use the `preflight-plugin-<name>` prefix.

Examples:

- `preflight-plugin-signage_sync`
- `preflight-plugin-signage_sync.exe`

On Windows, the executable must end in `.exe`.

## Registration Rules

Preflight initializes discovered plugins before registering them.

Registration fails when:

- the plugin cannot be started
- `initialize` fails
- two plugins resolve to the same logical name
- a plugin name conflicts with a built-in module name

## YAML Invocation

Use the explicit module task form:

```yaml
tasks:
  - name: Sync signage content
    module: signage_sync
    params:
      source: "\\\\nas01\\signage"
```

Plugin-backed modules are discovered at runtime, so they do not appear as static inline-module keys in the JSON schema.

## JSON-RPC Methods

The runner uses these methods:

| Method | Purpose |
| --- | --- |
| `initialize` | Report plugin name and version |
| `check` | Report whether change is needed |
| `apply` | Perform the change |

The bundled Go SDK lives in [`pkg/plugin/sdk/`](/Users/clay/repos/preflight/pkg/plugin/sdk).

## Go SDK Contract

Plugin authors implement:

- `Name() string`
- `Version() string`
- `Check(args map[string]any) (CheckResult, error)`
- `Apply(args map[string]any) (ApplyResult, error)`

Then call `sdk.Serve(module)` from `main()`.

## Bundle Behavior

When a staged plan references a plugin module, the bundle includes:

- the plugin executable under `plugins/`
- module metadata in `manifest.json`

Staging fails if the plugin cannot be initialized or copied.

## Related Commands

| Command | Purpose |
| --- | --- |
| `preflight plugin list` | List discovered plugins and initialization status |
| `preflight plugin info <name>` | Show one plugin’s details |

## Related Docs

- [Write a plugin](../how-to/write-a-plugin.md)
- [Use plugin modules in playbooks](../how-to/use-plugin-modules.md)
- [Bundle reference](./bundles.md)
- [Targets, transports, and plugins](../explanation/targets-and-transports.md)
