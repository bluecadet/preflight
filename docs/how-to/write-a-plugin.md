# Write A Plugin

Use this guide when you want to add a custom module to Preflight as a standalone executable.

## Before You Start

You need:

- Go installed locally
- A working `preflight` binary for local testing
- A plugin name that does not conflict with a built-in module name

Preflight plugins are regular executables, not Go `.so` plugins. They speak JSON-RPC over stdin/stdout and must follow the same `Check()` then `Apply()` contract as built-in modules.

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
	"os"

	"github.com/bluecadet/preflight/pkg/plugin/sdk"
)

type markerFileModule struct{}

func (markerFileModule) Name() string {
	return "marker_file"
}

func (markerFileModule) Version() string {
	return "0.1.0"
}

func (markerFileModule) Check(args map[string]any) (sdk.CheckResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return sdk.CheckResult{}, os.ErrInvalid
	}

	_, err := os.Stat(path)
	if err == nil {
		return sdk.CheckResult{
			NeedsChange: false,
			State:       map[string]any{"path": path, "exists": true},
		}, nil
	}
	if os.IsNotExist(err) {
		return sdk.CheckResult{
			NeedsChange: true,
			State:       map[string]any{"path": path, "exists": false},
		}, nil
	}
	return sdk.CheckResult{}, err
}

func (markerFileModule) Apply(args map[string]any) (sdk.ApplyResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return sdk.ApplyResult{}, os.ErrInvalid
	}

	if err := os.WriteFile(path, []byte("managed by preflight plugin\n"), 0o644); err != nil {
		return sdk.ApplyResult{}, err
	}

	return sdk.ApplyResult{
		State: map[string]any{"path": path, "created": true},
	}, nil
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

## 3. Build The Executable With The Required Name

On macOS or Linux:

```bash
go build -o preflight-plugin-marker_file .
```

On Windows:

```bash
go build -o preflight-plugin-marker_file.exe .
```

The filename matters. Preflight only discovers executables named `preflight-plugin-<name>` or, on Windows, `preflight-plugin-<name>.exe`.

## 4. Install It Into A Plugin Directory

Preflight scans plugin directories in this order:

1. alongside the `preflight` binary
2. `~/.preflight/plugins`
3. `./plugins` relative to the current working directory

For project-local testing:

```bash
mkdir -p plugins
mv preflight-plugin-marker_file plugins/
```

## 5. Verify Discovery

Check that Preflight can start the plugin and read its metadata:

```bash
preflight plugin list
preflight plugin info marker_file
```

Fix discovery or initialization errors here before using the plugin in a playbook.

## 6. Call The Plugin From YAML

Use the explicit `module` plus `params` form:

```yaml
tasks:
  - name: Create marker file through plugin
    module: marker_file
    params:
      path: "./tmp/plugin-marker.txt"
```

Then validate or run the playbook as usual:

```bash
preflight validate playbooks/lobby.yml
preflight plan playbooks/lobby.yml
preflight apply playbooks/lobby.yml
```

## 7. Distribute The Plugin

For other users, distribute the plugin as the compiled executable for their target OS and architecture.

Common patterns:

- publish source plus build instructions for Go users
- attach prebuilt binaries to a GitHub release
- ship the plugin executable alongside the `preflight` binary in an internal tools package
- commit the executable under a project-local `plugins/` directory when the team wants the plugin versioned with the repo

If you use `preflight stage`, the plugin must be discoverable on the staging machine. Preflight copies referenced plugin executables into the staged bundle automatically.

## Troubleshooting

### `preflight plugin list` does not show the plugin

Check the filename first. It must start with `preflight-plugin-`, and on Windows it must end with `.exe`.

### The plugin appears, but the module is unknown in YAML

Use the logical name reported by:

```bash
preflight plugin info <name>
```

That reported name is what belongs in `module:`.

### The plugin works locally but not over SSH

Plugin modules are not yet supported over SSH execution. Use local execution or a staged bundle instead.

## Related Docs

- [Use plugin modules in playbooks](./use-plugin-modules.md)
- [Plugin reference](../reference/plugins.md)
- [Bundle reference](../reference/bundles.md)
