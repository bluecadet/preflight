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
| `initialize` | host → plugin | Carry the host's protocol version range, capabilities, and the enriched `TargetInfo`; plugin responds with its own range, capabilities, and name/version |
| `check` | host → plugin | Report whether change is needed; plugin may issue handle ops |
| `apply` | host → plugin | Perform the change; plugin may issue handle ops |
| `cancel` | both | Notification carrying the `id` of a request the sender is abandoning; no response |
| `output` | plugin → host | Notification carrying one streaming line; no response |
| `run_command` | plugin → host | Run a script in the target's native shell, returning stdout/stderr/exit code |
| `put_file` | plugin → host | Write bytes to a path on the target (host does the chunking) |
| `get_file` | plugin → host | Read a path's bytes from the target |

### Protocol Version Negotiation

`initialize` carries `protocol_version` and `min_protocol_version` (both integers), the inclusive range of wire-protocol versions that side speaks, plus an optional `capabilities` array of feature strings. The peer responds with the same three fields for its own side. The handshake succeeds when the two ranges overlap; the negotiated version is the highest value present in both. This SDK build currently advertises `protocol_version: 2, min_protocol_version: 2` — a single-version range — but the mechanism means a future version bump can widen the range instead of hard-rejecting every previously built plugin.

A peer whose range does not overlap (including a plugin that reports no `protocol_version` at all) is rejected with a `plugin_protocol` error naming both sides' supported ranges. Fields neither side recognizes are ignored rather than rejected, so additive wire changes stay backward compatible.

Capabilities are a second, independent extension point: each side advertises the feature names it supports, and the negotiated capability set is whatever both sides listed. Negotiation is symmetric — the host reads it via `Client.HasCapability`/`Client.Capabilities`, and a plugin reads the same negotiated outcome from inside `Check`/`Apply` via `sdk.NegotiatedFromContext(ctx)`. This lets a future optional feature be detected at runtime on either side, without forcing a protocol version bump just to add it.

### Message Ordering: initialize Must Complete First

`initialize` must be the first message on a session, and the host must receive its response before sending `check`, `apply`, or anything else. This is a hard requirement, not just a convention: the plugin server dispatches every decoded request to its own goroutine as soon as it is decoded, so nothing in the wire format itself stops two requests from being in flight at once if a peer sends them without waiting. `initialize` populates state (the negotiated version/capabilities, the delivered `TargetInfo`) that every other method depends on, so a request racing ahead of it would be racing undefined state.

A plugin therefore refuses any method but `initialize` — including a second `initialize` once one has already completed — until `initialize` has finished, with a JSON-RPC error, code `-32011`, naming the method that arrived out of order:

```json
{"jsonrpc":"2.0","id":2,"error":{"code":-32011,"message":"method \"check\" called before initialize completed"}}
```

A conforming host — including the bundled Go `Client` — always waits for the `initialize` response before issuing anything else, so it never sees this error in practice. It exists for peers implementing the wire protocol directly rather than through the SDK, and as a defined, specified rejection rather than an unspecified race for anyone who gets the ordering wrong.

### Cancellation

`check` and `apply` receive a `context.Context`, and it is a real cancellation signal rather than an unused parameter. When the host abandons a call — a `--timeout` expiring, the run being torn down — it sends a `cancel` notification naming that request's `id`. The plugin SDK cancels the context it handed the module's `Check`/`Apply`.

Cancellation is symmetric. If a plugin's `Check` is cancelled while it is waiting on a `run_command`, the plugin sends `cancel` for that op, and the host cancels the target operation it was running — so an interrupt reaches all the way down to the command executing over SSH or WinRM.

After sending `cancel`, the sender waits up to a **two-second grace window** for the abandoned request to unwind and answer before giving up. That window is what a cancelled plugin has to release locks, remove half-written files, and return. It is an upper bound, not a promise: a plugin that ignores its context is killed with the session when the window closes.

The plugin process is deliberately **not** bound to the operation context. If it were, the process would die the instant the operation was cancelled, and the context inside the plugin would never have a chance to fire.

### TargetInfo

`initialize` delivers the enriched `TargetInfo` to the plugin: `{family, name, version, arch, hostname, package_manager, init, runtime_kind}`. Absent signals are empty strings, never missing keys. `runtime_kind` (`posix-shell` or `windows-powershell`) tells the plugin which shell `run_command` speaks; the plugin should not re-probe what the controller already cached.

The bundled Go SDK lives in [`pkg/plugin/sdk/`](https://github.com/bluecadet/preflight/tree/main/pkg/plugin/sdk).

## Go SDK Contract

Plugin authors implement:

- `Name() string`
- `Version() string`
- `Check(ctx context.Context, args map[string]any, h Handle) (CheckResult, error)`
- `Apply(ctx context.Context, args map[string]any, h Handle) (ApplyResult, error)`

Then call `sdk.Serve(module, sdk.ServeOptions{})` from `main()`. `ServeOptions.Capabilities` is where a plugin advertises its own capability names during `initialize`; the zero value advertises none beyond what the SDK build itself supports. The `Handle` exposes `RunCommand`, `PutFile`, `GetFile`, `Info`, and `Output`; see [Write a plugin](../how-to/write-a-plugin.md) for the handle API and batching guidance.

Pass `ctx` to every handle op. An op issued with `context.Background()` cannot be interrupted, and the plugin is torn down mid-flight instead of unwinding. `ctx` also carries the negotiated handshake result — `sdk.NegotiatedFromContext(ctx)` returns the agreed protocol version and capability set, letting `Check`/`Apply` branch on a capability instead of a version bump. See [Capabilities](../how-to/write-a-plugin.md#capabilities).

## Execution Model

Plugins execute **controller-side**: the plugin process always runs on the machine running `preflight`, never on the target. `Check`/`Apply` receive a target handle and all target effects flow through it — including when the target is local. This makes plugins uniform over local, SSH, and WinRM; the plugin does not know (or need to know) which transport it is on.

Process lifetime is run-scoped: a plugin is spawned lazily on first use, reused across every task in the run that invokes that module, and hard-killed at the end of the run.

## `become` Limitation

A plugin task with `become` enabled is refused with a typed `plugin_become` error before the plugin runs. Privilege escalation through the plugin handle is planned for a future protocol version; for now, run plugins as the connection user (or root directly).

## Stated Limitations

Protocol v2 carries a few deliberate limits, documented here so plugin authors can design around them rather than discover them at runtime:

- **`become` is refused** — the handle does not carry `ExecOpts`; see the section above.
- **Cancellation is cooperative** — the host delivers `cancel` and waits a two-second grace window, but it cannot force a plugin that ignores its context to stop. Such a plugin is killed when the window closes.
- **No plugin State plumbing** — a plugin's `Check`/`Apply` receive params and a context; there is no protocol-level state transfer between calls. A plugin that needs cross-call state must keep it in process memory for the run-scoped plugin lifetime, or round-trip it through the target handle.
- **One in-flight target op per session** — the protocol allows one `run_command`/`put_file`/`get_file` in flight at a time; do not issue a second before the first returns. Batching guidance in [Write a plugin](../how-to/write-a-plugin.md) shows the script-shaped-exec pattern that keeps this from dominating latency.
- **Protocol version ranges are negotiated, not pinned** — the host and plugin each advertise a `[min_protocol_version, protocol_version]` range and agree on the highest overlapping version; a plugin whose range does not overlap the host's (including one reporting no `protocol_version` at all) is rejected with a `plugin_protocol` error naming both ranges.
- **`initialize` must complete before anything else** — a plugin refuses `check`, `apply`, or a second `initialize` arriving before the first `initialize` has finished, with a distinct `-32011` error; see [Message Ordering](#message-ordering-initialize-must-complete-first).

## Bundle Behavior

When a staged plan references a plugin module, the bundle includes:

- the plugin executable under `plugins/`
- module metadata in `manifest.json`

Staging fails if the plugin cannot be initialized, reports the wrong logical name, or cannot be copied.

The plugin executable must match the staged host's OS and architecture because
bundle apply runs it locally on that host. Preflight rejects cross-platform
plugin staging; cross-platform bundles currently support built-in modules
only.

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
