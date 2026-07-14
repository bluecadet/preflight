# Write A Plugin

Use this guide when you want to add a custom module to Preflight as a standalone executable.

## Before You Start

You need:

- Go installed locally
- A working `preflight` binary for local testing
- A plugin name that does not conflict with a built-in module name

Preflight plugins are regular executables, not Go `.so` plugins. They speak JSON-RPC over stdin/stdout and follow the same `Check()` then `Apply()` contract as built-in modules.

## How Plugin Execution Works

The plugin process runs **on the controller** — the machine running `preflight`. It is never copied to or staged on the target. `Check` and `Apply` receive a **target handle**, and **all** target effects flow through that handle: `RunCommand`, `PutFile`, `GetFile`, and `Output`. This is true even when the target is the local machine, so a plugin works identically over local, SSH, and WinRM without knowing which transport it is on.

The handle is the only way a plugin touches the target. Do not read the local filesystem or spawn local processes to affect the target — go through the handle, so your plugin is transport-agnostic.

## 1. Create A Small Go Module

Create a new repository or directory for the plugin:

```bash
mkdir preflight-plugin-marker-file
cd preflight-plugin-marker-file
go mod init example.com/preflight-plugin-marker-file
go get github.com/bluecadet/preflight@latest
```

That pulls in the Go SDK from `github.com/bluecadet/preflight/pkg/plugin/sdk`.

## 2. Implement The Plugin Contract

Create `main.go`:

```go
package main

import (
	"context"

	"github.com/bluecadet/preflight/pkg/plugin/sdk"
)

type markerFileModule struct{}

func (markerFileModule) Name() string    { return "marker_file" }
func (markerFileModule) Version() string { return "0.1.0" }

func (markerFileModule) Check(args map[string]any, h sdk.Handle) (sdk.CheckResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return sdk.CheckResult{}, fmt.Errorf("path is required")
	}

	// Probe the target through the handle — never the local filesystem.
	res, err := h.RunCommand(context.Background(), "test -f "+shellQuote(path)+" && echo yes || echo no")
	if err != nil {
		return sdk.CheckResult{}, err
	}
	exists := strings.TrimSpace(res.Stdout) == "yes"
	return sdk.CheckResult{NeedsChange: !exists}, nil
}

func (markerFileModule) Apply(args map[string]any, h sdk.Handle) (sdk.ApplyResult, error) {
	path, _ := args["path"].(string)
	script := "printf 'managed by preflight plugin\\n' > " + shellQuote(path)
	if _, err := h.RunCommand(context.Background(), script); err != nil {
		return sdk.ApplyResult{}, err
	}
	return sdk.ApplyResult{Message: "created " + path}, nil
}

func main() {
	sdk.Serve(markerFileModule{})
}
```

Important details:

- `Name()` is the module name users put in `module:`.
- `Version()` is reported by `preflight plugin list` and `preflight plugin info`.
- `Check()` must return `NeedsChange: true` only when `Apply()` should run.
- `Apply()` receives the same `params:` map that was defined in YAML.
- All target effects go through `h` (the handle). Use `h.RunCommand`, `h.PutFile`, `h.GetFile`, `h.Info`, and `h.Output`.

## 3. The Handle API

The handle exposes three target primitives plus streaming output and the target info delivered at initialize.

### RunCommand

```go
res, err := h.RunCommand(ctx, script)
// res.Stdout, res.Stderr, res.ExitCode
```

`script` runs in the target's native shell: POSIX `sh` when `TargetInfo.RuntimeKind` is `posix-shell`, PowerShell when it is `windows-powershell`. Branch on `h.Info().RuntimeKind` (or `h.Info().Family`) to write portable plugins.

**RunCommand is the batching lever.** Each handle op is a transport round trip; over SSH or WinRM that round trip has real latency. Prefer one script that does several things over several `RunCommand` calls. For example, check a file, write it if missing, and set its mode in a single script instead of three.

### PutFile / GetFile

```go
err := h.PutFile(ctx, "/tmp/config", []byte("contents"))
data, err := h.GetFile(ctx, "/etc/hostname")
```

`PutFile` takes raw bytes; the host handles chunking for high-latency transports, so the plugin sees a single call regardless of transport. `GetFile` returns the file's bytes.

### Info

```go
info := h.Info()
// info.Family (windows|linux|darwin)
// info.Name, info.Version, info.Arch, info.Hostname
// info.PackageManager (apt|dnf|"" on POSIX; "" on Windows)
// info.Init (systemd|"" on POSIX; "" on Windows)
// info.RuntimeKind (posix-shell|windows-powershell)
```

`Info()` returns the `TargetInfo` delivered at `initialize`. Absent signals are empty strings, never missing keys, so you can branch with simple equality:

```go
if h.Info().Family == "windows" { ... }
if h.Info().PackageManager == "apt" { ... }
if h.Info().Init == "systemd" { ... }
```

Do not re-probe what the controller already cached — `Info()` is the authoritative view.

### Output

```go
h.Output("progress: 50%")
```

`Output` streams a line back to the host's output channel during `Check` or `Apply`. Call it for progress or diagnostics; the host surfaces lines to the user as they arrive.

## 4. Build The Executable With The Required Name

On macOS or Linux:

```bash
go build -o preflight-plugin-marker_file .
```

On Windows:

```bash
go build -o preflight-plugin-marker_file.exe .
```

The filename matters. Preflight only discovers executables named `preflight-plugin-<name>` or, on Windows, `preflight-plugin-<name>.exe`.

The plugin's `Name()` result must match that `<name>` value. Preflight validates that the discovered filename, YAML module name, and reported logical name all line up.

## 5. Install It Into A Plugin Directory

Preflight scans plugin directories in this order:

1. alongside the `preflight` binary
2. `~/.preflight/plugins`
3. `./plugins` relative to the current working directory

For project-local testing:

```bash
mkdir -p plugins
mv preflight-plugin-marker_file plugins/
```

## 6. Verify Discovery

Check that Preflight can start the plugin and read its metadata:

```bash
preflight plugin list
preflight plugin info marker_file
```

Fix discovery or initialization errors here before using the plugin in a playbook.

## 7. Call The Plugin From YAML

Use the explicit `module` plus `params` form:

```yaml
tasks:
  - name: Create marker file through plugin
    module: marker_file
    params:
      path: "/var/lib/preflight/marker.txt"
```

Then validate or run the playbook as usual:

```bash
preflight validate playbooks/lobby.yml
preflight plan playbooks/lobby.yml
preflight apply playbooks/lobby.yml
```

## Protocol Version 1 (Breaking Change)

Plugin protocol v1 is a **clean break** from the pre-v1 protocol. If you have an existing plugin, you must update it:

- `Check` and `Apply` now take a `Handle` as their second argument. All target effects go through it.
- `initialize` now carries `protocol_version` and the enriched `TargetInfo`; your plugin receives them automatically through the SDK.
- Streaming output is no longer a separate `StreamingModule` interface — call `h.Output(line)` from `Check`/`Apply`.
- `sdk.Serve` handles the wire protocol; your plugin only implements `Module`.

Pre-v1 plugins are rejected with a clear `plugin_protocol` error. There is no compatibility mode — update and rebuild.

## Batching Guidance For High-Latency Transports

Every handle op is a transport round trip. Over SSH and especially WinRM, round trips dominate runtime. Design your plugin to minimize them:

- **One script, several actions.** Instead of `RunCommand` to check, `RunCommand` to create, `RunCommand` to set mode, write one script that does all three and reports the outcome. POSIX `sh` and PowerShell both let you branch on results within a single script.
- **Avoid redundant `GetFile`.** If `RunCommand` can read what you need and include it in stdout, do that instead of a separate `GetFile` round trip.
- **Cache `Info()` once.** Call `h.Info()` and keep the struct; do not re-fetch per op (it is in-memory, but the call pattern reads better).
- **One in-flight op at a time.** The protocol allows one in-flight target op per session. Do not issue a second `RunCommand` before the first returns.

## The `become` Limitation

A plugin task cannot run with `become` enabled in v1. If you set `become: { enabled: true }` on a plugin task, Preflight refuses it with a typed `plugin_become` error before the plugin runs. Run plugins as the connection user (or root directly); privilege escalation through the plugin handle is planned for a future protocol version.

## 8. Distribute The Plugin

Plugins execute controller-side, so they must run on the machine running `preflight`. Build the plugin for the controller's OS and architecture. First-party plugins ship for `windows/linux/darwin` × `amd64/arm64`.

Common patterns:

- publish source plus build instructions for Go users
- attach prebuilt binaries to a GitHub release
- ship the plugin executable alongside the `preflight` binary in an internal tools package
- commit the executable under a project-local `plugins/` directory when the team wants the plugin versioned with the repo

If you use `preflight stage`, the plugin must be discoverable on the staging machine. Preflight copies referenced plugin executables into the staged bundle automatically, but staging fails if the plugin cannot initialize or reports the wrong logical name.

The discovered executable must also match the staged host's OS and
architecture. During bundle apply, the destination host becomes the
controller and starts that executable locally. Preflight rejects
cross-platform plugin staging rather than copying a controller-native binary
that cannot run on the destination. Stage plugin bundles from a matching
platform; cross-platform staging currently supports built-in modules only.

## Troubleshooting

### `preflight plugin list` does not show the plugin

Check the filename first. It must start with `preflight-plugin-`, and on Windows it must end with `.exe`.

### The plugin appears, but the module is unknown in YAML

Use the logical name reported by:

```bash
preflight plugin info <name>
```

That reported name is what belongs in `module:`.

### `plugin_protocol` error at runtime

Your plugin was built against a pre-v1 SDK. Update to the current SDK, add the `Handle` argument to `Check`/`Apply`, and rebuild.

### `plugin_become` error

A plugin task had `become` enabled. Remove `become` from the task (or run the connection account with the privileges the plugin needs). Plugin+become is not supported in v1.

## Related Docs

- [Use plugin modules in playbooks](./use-plugin-modules.md)
- [Plugin reference](../reference/plugins.md)
- [Bundle reference](../reference/bundles.md)
