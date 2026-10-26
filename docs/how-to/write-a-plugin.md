# Write a plugin

Add a custom module to Preflight as a standalone executable built with the Go SDK in `pkg/plugin/sdk`.

## Prerequisites

- Go
- A `preflight` binary for testing
- A module name that no built-in module uses

A plugin speaks JSON-RPC over stdin and stdout and follows the same `Check()` then `Apply()` contract as built-in modules. It runs on the controller, the machine running `preflight`, and never on the target. Every target effect goes through the handle passed to `Check` and `Apply`, so the plugin works unchanged over local, SSH, and WinRM targets. Do not touch the local filesystem or start local processes to change the target. For the wire protocol, see the [Plugin reference](../reference/plugins.md).

## 1. Create a Go module

```bash
mkdir preflight-plugin-marker-file
cd preflight-plugin-marker-file
go mod init example.com/preflight-plugin-marker-file
go get github.com/bluecadet/preflight@latest
```

## 2. Implement the module

Create `main.go`:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bluecadet/preflight/pkg/plugin/sdk"
)

type markerFileModule struct{}

func (markerFileModule) Name() string    { return "marker_file" }
func (markerFileModule) Version() string { return "0.1.0" }

func (markerFileModule) Check(ctx context.Context, args map[string]any, h sdk.Handle) (sdk.CheckResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return sdk.CheckResult{}, errors.New("path is required")
	}

	res, err := h.RunCommand(ctx, "if (Test-Path -LiteralPath "+psQuote(path)+" -PathType Leaf) { 'yes' } else { 'no' }")
	if err != nil {
		return sdk.CheckResult{}, err
	}
	exists := strings.TrimSpace(res.Stdout) == "yes"
	return sdk.CheckResult{NeedsChange: !exists}, nil
}

func (markerFileModule) Apply(ctx context.Context, args map[string]any, h sdk.Handle) (sdk.ApplyResult, error) {
	path, _ := args["path"].(string)
	script := "$ErrorActionPreference = 'Stop'; Set-Content -LiteralPath " + psQuote(path) + " -Value 'managed by preflight plugin'"
	res, err := h.RunCommand(ctx, script)
	if err != nil {
		return sdk.ApplyResult{}, err
	}
	if res.ExitCode != 0 {
		return sdk.ApplyResult{}, fmt.Errorf("write %s: %s", path, strings.TrimSpace(res.Stderr))
	}
	return sdk.ApplyResult{Message: "created " + path}, nil
}

// psQuote wraps s in PowerShell single quotes.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func main() {
	sdk.Serve(markerFileModule{}, sdk.ServeOptions{})
}
```

- `Name()` is the name users put in `module:`. It must match the executable name in step 3.
- `Version()` appears in `preflight plugin list` and `preflight plugin info`.
- `Check()` returns `NeedsChange: true` only when `Apply()` should run.
- `Check()` and `Apply()` receive the task's `params:` map as `args`.
- Pass `ctx` to every handle call. See [Honour cancellation](#honour-cancellation).

### Handle methods

| Method | Use |
|---|---|
| `RunCommand(ctx, script)` | Run a PowerShell script on the target. Returns `Stdout`, `Stderr`, and `ExitCode`; a non-zero exit code is not an error, so check it. |
| `PutFile(ctx, path, data)` | Write bytes to a file on the target. |
| `GetFile(ctx, path)` | Read a file from the target. |
| `Info()` | Return the target's `Family`, `Version`, `Arch` (for example `amd64`), `Hostname`, and `RuntimeKind` (`windows-powershell`). Missing values are empty strings. |
| `Output(line)` | Stream a progress or diagnostic line to the user. |

Each handle call except `Info()` and `Output()` is a round trip to the target, and the protocol allows one in flight at a time. Over SSH and WinRM, round trips dominate runtime, so combine work into one `RunCommand` script: check, write, and report in a single call, and return file contents on stdout instead of making a separate `GetFile` call.

`PutFile` and `GetFile` send the whole file in one message held in memory. Keep payloads to a few megabytes.

### Honour cancellation

When Preflight abandons a call, for example when `--timeout` expires or the run is interrupted, the SDK cancels `ctx`. Passing `ctx` to a handle call also cancels the command running on the target. Check `ctx` inside your own loops:

```go
for _, item := range items {
	if err := ctx.Err(); err != nil {
		return sdk.ApplyResult{}, err
	}
	// ...
}
```

After cancellation the plugin has two seconds to clean up and return before Preflight kills it.

## 3. Build the executable

Name the executable `preflight-plugin-<name>`, where `<name>` matches `Name()`. On Windows, add `.exe`.

macOS or Linux:

```bash
go build -o preflight-plugin-marker_file .
```

Windows:

```bash
go build -o preflight-plugin-marker_file.exe .
```

Build for the controller's OS and architecture. For staged bundles, see [Use plugin modules in playbooks](./use-plugin-modules.md#3-stage-plugin-tasks-into-offline-bundles).

## 4. Install the plugin

Preflight looks for plugins in:

- the directory containing the `preflight` binary
- `~/.preflight/plugins`
- `plugins/` in the project root

A plugin name may appear in only one of these directories. For project-local testing:

```bash
mkdir -p plugins
mv preflight-plugin-marker_file plugins/
```

To version the plugin with the project, commit it under `plugins/`. To share it more widely, publish prebuilt binaries or ship them alongside `preflight`.

## 5. Verify discovery

From the project root:

```bash
preflight plugin list
preflight plugin info marker_file
```

`plugin info` shows `Initialize:  ok` when Preflight can start the plugin and complete the handshake.

## 6. Call the plugin from YAML

```yaml
tasks:
  - name: Create marker file through plugin
    module: marker_file
    params:
      path: "C:\\ProgramData\\Preflight\\marker.txt"
```

Then run the playbook as usual:

```bash
preflight validate playbooks/lobby.yml
preflight plan playbooks/lobby.yml
preflight apply playbooks/lobby.yml
```

## Capabilities

Capabilities let a plugin and host agree on optional features by name without a protocol version change. Advertise them in `ServeOptions`:

```go
sdk.Serve(markerFileModule{}, sdk.ServeOptions{
	Capabilities: []string{"marker_file/checksum"},
})
```

Read the agreed set from `ctx` in `Check` or `Apply`:

```go
if neg, ok := sdk.NegotiatedFromContext(ctx); ok && neg.HasCapability("marker_file/checksum") {
	// The host also advertised this capability.
}
```

`HasCapability` returns `true` only for names both sides advertise. Preflight advertises no capabilities today, so these checks return `false` when Preflight runs the plugin.

## Troubleshooting

### `preflight plugin list` does not show the plugin

Check the filename. It must start with `preflight-plugin-`, and on Windows end with `.exe`. Run the command from the project root if the plugin is in `plugins/`.

### The module is unknown in YAML

The `module:` value must equal the filename suffix. If `plugin list` shows a different name, `Name()` disagrees with the filename; make them match.

### `plugin_protocol` error

The plugin and host must speak exactly the same protocol version. Rebuild the plugin against the SDK from the `preflight` release you run.

## Related docs

- [Use plugin modules in playbooks](./use-plugin-modules.md)
- [Plugin reference](../reference/plugins.md)
- [Bundle reference](../reference/bundles.md)
