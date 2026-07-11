# Plugin Reference

This page describes how Preflight discovers, initializes, executes, and stages executable plugins.

## Purpose

Preflight plugins extend the module library without using Go `.so` plugins, which are not a practical fit for Windows. A plugin is a standalone executable that speaks JSON-RPC over stdin and stdout.

The runner treats a plugin-backed module like any other module:

- `Check()` runs first
- `Apply()` runs only when change is needed
- dry-run stays on the `Check()` side

## Discovery

Plugin executables are discovered by filename in this order:

1. The directory alongside the `preflight` binary
2. `~/.preflight/plugins`
3. `./plugins` relative to the working directory

During staged bundle apply, Preflight uses the bundle-local `plugins/` directory and can isolate discovery to that payload.

Preflight does not initialize every discovered plugin during normal command startup. Initialization is deferred until one of these points:

- `preflight plugin list`
- `preflight plugin info <name>`
- staging a bundle that includes the plugin
- first runtime use of the plugin-backed module

## File Naming

Plugin executables must use the `preflight-plugin-<name>` prefix.

Examples:

- `preflight-plugin-signage_sync`
- `preflight-plugin-signage_sync.exe`

On Windows, the executable must end in `.exe`.

## Registration Rules

Preflight registers discovered plugin executables by filename and validates the plugin's reported logical name when it is initialized.

Registration fails when:

- two discovered plugin filenames resolve to the same module name
- a discovered plugin filename conflicts with a built-in module name

Initialization fails when:

- the plugin cannot be started
- `initialize` fails
- the plugin reports a logical name that does not match the discovered module name

## YAML Invocation

Use the explicit module task form. The `module:` name must match both the executable filename suffix and the plugin's reported logical name:

```yaml
tasks:
  - name: Sync signage content
    module: signage_sync
    params:
      source: "\\\\nas01\\signage"
```

Plugin-backed modules are discovered at runtime, so they do not appear as static inline-module keys in the JSON schema.

## JSON-RPC Methods

The wire protocol is **bidirectional newline-delimited JSON-RPC 2.0** over the plugin's stdin/stdout. Both sides act as client and server: the host sends `initialize`/`check`/`apply` requests, and the plugin sends `run_command`/`put_file`/`get_file` requests back to the host for handle ops. Request IDs are correlated on both sides; one target op is in flight per session (a stated limitation).

| Method | Direction | Purpose |
| --- | --- | --- |
| `initialize` | host → plugin | Carry `protocol_version` and the enriched `TargetInfo`; plugin echoes `protocol_version` back with name/version |
| `check` | host → plugin | Report whether change is needed; plugin may issue handle ops |
| `apply` | host → plugin | Perform the change; plugin may issue handle ops |
| `output` | plugin → host | Notification carrying one streaming line; no response |
| `run_command` | plugin → host | Run a script in the target's native shell, returning stdout/stderr/exit code |
| `put_file` | plugin → host | Write bytes to a path on the target (host does the chunking) |
| `get_file` | plugin → host | Read a path's bytes from the target |

### Protocol Version

`initialize` carries `protocol_version` (currently `"1"`). The plugin must echo it back in its `initialize` response. A pre-v1 plugin (no `protocol_version` or a different one) is rejected with a `plugin_protocol` error — there is no compatibility mode.

### TargetInfo

`initialize` delivers the enriched `TargetInfo` to the plugin: `{family, name, version, arch, hostname, package_manager, init, runtime_kind}`. Absent signals are empty strings, never missing keys. `runtime_kind` (`posix-shell` or `windows-powershell`) tells the plugin which shell `run_command` speaks; the plugin should not re-probe what the controller already cached.

The bundled Go SDK lives in [`pkg/plugin/sdk/`](/Users/clay/repos/preflight/pkg/plugin/sdk).

## Go SDK Contract

Plugin authors implement:

- `Name() string`
- `Version() string`
- `Check(args map[string]any, h Handle) (CheckResult, error)`
- `Apply(args map[string]any, h Handle) (ApplyResult, error)`

Then call `sdk.Serve(module)` from `main()`. The `Handle` exposes `RunCommand`, `PutFile`, `GetFile`, `Info`, and `Output`; see [Write a plugin](../how-to/write-a-plugin.md) for the handle API and batching guidance.

## Execution Model

Plugins execute **controller-side**: the plugin process always runs on the machine running `preflight`, never on the target. `Check`/`Apply` receive a target handle and all target effects flow through it — including when the target is local. This makes plugins uniform over local, SSH, and WinRM; the plugin does not know (or need to know) which transport it is on.

Process lifetime is run-scoped: a plugin is spawned lazily on first use, reused across every task in the run that invokes that module, and hard-killed at the end of the run.

## `become` Limitation

A plugin task with `become` enabled is refused with a typed `plugin_become` error before the plugin runs. Privilege escalation through the plugin handle is planned for a future protocol version; for now, run plugins as the connection user (or root directly).

## Bundle Behavior

When a staged plan references a plugin module, the bundle includes:

- the plugin executable under `plugins/`
- module metadata in `manifest.json`

Staging fails if the plugin cannot be initialized, reports the wrong logical name, or cannot be copied.

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
