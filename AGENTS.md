# AGENTS.md

This file provides guidance to Codex (Codex.ai/code) when working with code in this repository.

## Project

**Preflight** is a Windows-first configuration management CLI (Go) for deploying and maintaining exhibit PCs in museum/gallery environments. It compiles to a single static binary with no runtime dependencies.

## Build Commands

```makefile
# Cross-compile for Windows (primary target)
GOOS=windows GOARCH=amd64 go build -o dist/preflight-windows-amd64.exe .
GOOS=windows GOARCH=arm64 go build -o dist/preflight-windows-arm64.exe .

# Local build (for testing non-Windows code paths)
go build -o dist/preflight .
```

```bash
go test ./...                          # Run all tests
go test ./internal/runner/...          # Run tests for a specific package
go vet ./...                           # Vet
```

A `Makefile` provides `build-windows-amd64`, `build-windows-arm64`, and `build-local` targets, as well as `test` and `vet`.

## Architecture

Three-layer model: **Modules → Actions → Playbooks**

**Modules** (`internal/module/`) — Go primitives compiled into the binary. Each implements a two-method idempotency contract:
- `Check() → (needed bool, err)` — is system already in desired state?
- `Apply() → err` — make it so

The runner always calls `Check` first; `Apply` is only called when change is needed. This makes `--check` dry-run mode a first-class feature.

**Actions** (`internal/action/`, `internal/stdlib/actions/`) — parameterized YAML bundles of tasks. Resolved via a chain: embedded stdlib → local `./actions/` → user cache `~/.preflight/actions/` → remote Git. Remote refs are pinned in `preflight.lock`.

**Playbooks** (`playbooks/*.yml`) — per-machine/environment declarations referencing actions and inventory targets.

### Execution Pipeline

`Fetch → Plan → Stage → Apply`

- **Fetch**: download remote action refs into cache.
- **Plan**: parse playbook, resolve all action refs, expand into a flat task DAG, validate inputs, resolve variables. Pure computation — no I/O against targets.
- **Stage**: optional — assemble a self-contained artifact bundle (zip) that can be pushed to air-gapped targets.
- **Apply**: execute the task graph against targets via the `Target` interface.

### Target Interface (`internal/target/target.go`)

The runner is **always** injected with a `Target` at construction time and never assumes local execution. This is the critical invariant for future distributed mode. Implementations: `LocalTarget`, `WinRMTarget`, `SSHTarget`. A future `AgentTarget` satisfies the same interface.

### Key Packages

| Path | Responsibility |
|---|---|
| `cmd/` | Cobra CLI commands |
| `internal/runner/` | Phase orchestration, task DAG, state file |
| `internal/action/` | Resolver chain (embedded, local, cache, git) and lockfile |
| `internal/module/` | Built-in Windows primitives |
| `internal/target/` | Target interface + LocalTarget/WinRM/SSH |
| `internal/targeting/` | Host resolution, selector expansion, and per-host var merging |
| `internal/bundle/` | Staged bundle format and extraction |
| `internal/template/` | Jinja-like `{{ }}` templating and layered variable merge |
| `internal/inventory/` | inventory.yml parsing with groups/host vars |
| `internal/facts/` | Windows fact gathering for `when:` conditions |
| `internal/output/` | text, TUI, JSON, and JSONL output renderers and event streaming |
| `internal/stdlib/` | `//go:embed all:actions` standard library |
| `internal/plugins/` | Plugin discovery, registry building, and conflict detection |
| `internal/secrets/` | Age-based secret resolution through named providers (`secret:<name>` refs) |
| `internal/config/` | `preflight.yml` project config parsing (project vars, secrets entries) |
| `internal/winutil/` | Shared param normalization helpers for Windows modules |
| `pkg/plugin/sdk/` | Plugin author SDK (JSON-RPC over stdin/stdout) |
| `schema/` | JSON Schemas for action.yml, playbook.yml, inventory.yml |

### Stdlib Embedding

```go
// internal/stdlib/embed.go
//go:embed all:actions
var FS embed.FS
```

Stdlib actions use the `preflight/` namespace and are always resolved before local or remote refs. They are versioned with the binary — no independent versions.

### Plugin System

Go `.so` plugins don't work on Windows. Plugins are executables speaking JSON-RPC over stdin/stdout. Discovered from: alongside the binary, `~/.preflight/plugins/`, and `./plugins/`. The `pkg/plugin/sdk` package handles protocol boilerplate for Go plugin authors.

During staged bundle apply, the discovery path can be overridden via `ExclusivePreferredDirs` in `plugins.Options`, which restricts discovery to the provided directories only (e.g. the bundle-local `plugins/` directory) and skips the standard fallback dirs.

### Variable Precedence (later wins)

```
Built-in defaults ← preflight.yml ← inventory vars ← playbook vars ← --var CLI flags
```

The inventory parser merges group vars (including the "all" group) and host vars into a single `Vars` map on each `Host` before that host reaches the runner. Group vars are applied first; host-level vars take precedence over them. The runner receives one already-merged vars map per host.

Templates use Jinja2-like syntax (`{{ vars.foo }}`) via a custom subset parser — **not** Go's `text/template`.

## Key Design Principles

1. **Idempotency is a contract.** Every module must implement `Check()`. The runner should never need to call `Apply()` twice — and should never need to trust that doing so is safe.

2. **The runner is target-agnostic.** No `localhost` assumptions in runner code. Always go through the `Target` interface. This is the single most important invariant for future distributed operation.

3. **Phases are explicit.** Plan/Fetch/Stage/Apply are distinct operations with distinct outputs, even when all run sequentially on one machine.

4. **Stdlib is versioned with the binary.** The embedded standard library ships with the binary. No independent stdlib versions. Users who need a pinned action version use a remote ref.

5. **The action schema is the public API.** Breaking changes to `action.yml` schema break every action ever written. Finalize it before writing resolvers or stdlib actions.

6. **Plugins are citizens, not afterthoughts.** Built-in modules should be designed with the plugin protocol in mind, to keep the plugin API honest.

## Making Changes

Validate your changes with `go test ./...` and `go vet ./...`. Lint with `golangci-lint run`.

Make sure to update this doc with any relevant architectural changes or new design principles.

Update `docs/` with any user-facing changes.

Update `README.md` and `CONTRIBUTING.md` with any changes that affect local development or usage.

If your changes change dependencies, run `go mod tidy` and commit the updated `go.mod` and `go.sum`.
