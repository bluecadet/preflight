# Plugin reference

A plugin is a standalone executable that adds one module. Preflight talks to it over newline-delimited JSON-RPC 2.0 on stdin and stdout. Plugin modules follow the same `Check()` then `Apply()` contract as built-in modules, and dry-run calls only `Check()`.

## Discovery

Preflight scans these directories:

1. the bundle `plugins/` directory, during `apply --bundle`; no other directory is scanned
2. the directory that contains the `preflight` binary
3. `~/.preflight/plugins`
4. `plugins/` in the project directory; `preflight plugin` commands use the current directory

Registration fails when a plugin name matches a built-in module or when the same name is found more than once:

```text
plugin "x" conflicts with built-in module name
plugin "x" discovered more than once (<path>, <path>)
```

## File naming

| OS | File name |
| --- | --- |
| Windows | `preflight-plugin-<name>.exe` |
| macOS, Linux | `preflight-plugin-<name>` |

`<name>` is the module name. Files without the prefix, or without `.exe` on Windows, are ignored.

## Initialization

Preflight starts a plugin and sends `initialize` at first use, not at discovery. First use is one of:

- `preflight plugin list` or `preflight plugin info <name>`
- staging a bundle that references the plugin
- the first `check` or `apply` of the module on a target

Initialization fails when the process cannot start, `initialize` returns an error, the protocol versions differ, or the plugin reports a name other than `<name>`.

## Task syntax

Plugin modules use the explicit `module` form. The schema has no inline key for plugin modules.

```yaml
tasks:
  - name: Sync signage content
    module: signage_sync
    params:
      source: "\\\\nas01\\signage"
```

## Execution model

The plugin process runs on the controller, never on the target. Every target effect goes through the handle methods below, on local, SSH, and WinRM targets alike.

Each target gets its own plugin process, started on first use, reused for later tasks on that target, and stopped when the run closes the target.

## JSON-RPC methods

Both sides send requests. Request IDs are correlated per direction.

| Method | Direction | Kind | Description |
| --- | --- | --- | --- |
| `initialize` | host to plugin | request | Exchange protocol version, capabilities, and target info; the plugin returns its name and version |
| `check` | host to plugin | request | Return whether a change is needed |
| `apply` | host to plugin | request | Make the change |
| `cancel` | either | notification | Abandon the request with the given `id` |
| `output` | plugin to host | notification | Stream one output line: `{"line": "..."}` |
| `run_command` | plugin to host | request | Run a PowerShell script on the target; returns stdout, stderr, and exit code |
| `put_file` | plugin to host | request | Write a whole file to the target |
| `get_file` | plugin to host | request | Read a whole file from the target; returns base64 `data` |

`check` and `apply` results may carry an `error` string in addition to their result fields.

### Protocol version

`initialize` carries an integer `protocol_version` and an optional `capabilities` string array, in both directions. The current version is `3`. Versions must match exactly. A plugin that reports a different version, or none, fails with a `plugin_protocol` error that names both versions.

Unknown JSON fields are ignored.

### Capabilities

Each side lists the capability names it supports. The negotiated set is the intersection. The current host advertises no capabilities, so the negotiated set is empty. The host reads it with `Client.HasCapability` and `Client.Capabilities`; a plugin reads it with `sdk.NegotiatedFromContext(ctx)` inside `Check` or `Apply`.

### Message ordering: initialize must complete first

`initialize` must be the first request, and the host must receive its response before it sends any other request. Until `initialize` completes, a plugin rejects every other method, including a second `initialize`, with error code `-32011`:

```json
{"jsonrpc":"2.0","id":2,"error":{"code":-32011,"message":"method \"check\" called before initialize completed"}}
```

The Go SDK client always waits for the response.

### Cancellation

When the host abandons a `check` or `apply`, for example on `--timeout` or run teardown, it sends `cancel` with that request's `id`. The SDK cancels the `context.Context` passed to `Check` or `Apply`.

A plugin cancels a pending `run_command`, `put_file`, or `get_file` the same way, and the host cancels the target operation.

The sender then waits up to 2 seconds for the abandoned request to return. A plugin that has not returned when the grace period ends is killed.

### Target info

`initialize` sends `TargetInfo` to the plugin:

| Field | Description |
| --- | --- |
| `family` | OS family |
| `version` | OS version |
| `arch` | Architecture |
| `hostname` | Target host name |
| `runtime_kind` | Always `windows-powershell`, the shell `run_command` uses |

Unknown values are empty strings.

## Go SDK

The SDK is in [`pkg/plugin/sdk`](https://github.com/bluecadet/preflight/tree/main/pkg/plugin/sdk). A plugin implements:

```go
Name() string
Version() string
Check(ctx context.Context, args map[string]any, h sdk.Handle) (sdk.CheckResult, error)
Apply(ctx context.Context, args map[string]any, h sdk.Handle) (sdk.ApplyResult, error)
```

`main()` calls `sdk.Serve(module, sdk.ServeOptions{})`. `ServeOptions.Capabilities` lists the plugin's capability names.

`Handle` provides `RunCommand`, `PutFile`, `GetFile`, `Info`, and `Output`. Pass the `ctx` from `Check` or `Apply` to every handle call; a call made with `context.Background()` cannot be cancelled. See [Write a plugin](../how-to/write-a-plugin.md) for usage and [Capabilities](../how-to/write-a-plugin.md#capabilities).

## Limitations

- Cancellation is cooperative. A plugin that ignores its context is killed after the 2-second grace period.
- The protocol carries no state between calls. Keep state in process memory for the run, or on the target.
- Only one `run_command`, `put_file`, or `get_file` may be in flight per plugin process.
- `put_file` and `get_file` transfer whole files in one message. Keep payloads to a few MB.
- Plugins must be rebuilt when the protocol version changes.

## Bundles

`preflight stage` copies each referenced plugin into the bundle under `plugins/` and records it in `manifest.json` `modules[]`. Staging fails when the plugin cannot be read, fails `initialize`, or reports the wrong name.

`apply --bundle` runs the plugin on the machine that applies the bundle, so the plugin must match that machine's OS and architecture. Staging rejects a plugin whose platform differs from the target's. See [Bundle reference](./bundles.md).

## Commands

| Command | Output |
| --- | --- |
| `preflight plugin list` | Name, version, status (`ready` or `error`), and path of each discovered plugin |
| `preflight plugin info <name>` | Name, version, path, source, and initialization result for one plugin |

## Related docs

- [Write a plugin](../how-to/write-a-plugin.md)
- [Use plugin modules in playbooks](../how-to/use-plugin-modules.md)
- [Targets, transports, and plugins](../explanation/targets-and-transports.md)
