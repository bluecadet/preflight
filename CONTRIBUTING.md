# Contributing to Preflight

Thanks for your interest in contributing. This document covers how to get set up, the conventions to follow, and how to submit changes.

---

## Getting Started

**Prerequisites:**
- Go 1.25.3+
- A Windows VM or machine for testing Windows-specific modules (or use the stub layer for non-Windows code paths)
- `golangci-lint` for local linting

**Clone the repo:**

```bash
git clone https://github.com/bluecadet/preflight
cd preflight
```

**Build locally for development:**

```bash
go build -o dist/preflight .
```

**Build the primary release targets:**

```bash
GOOS=windows GOARCH=amd64 go build -o dist/preflight-windows-amd64.exe .
GOOS=windows GOARCH=arm64 go build -o dist/preflight-windows-arm64.exe .
```

Or use `make`:

```bash
make build-windows-amd64
make build-windows-arm64
make build-local
```

Install `golangci-lint` if you do not already have it:

```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install golang.org/x/tools/cmd/goimports@latest
```

The linter config is in `.golangci.yml`.

---

## Local Validation

Before opening a pull request, run the same categories of checks that CI expects.

**Format code:**

```bash
gofmt -w .
goimports -w .
```

**Run tests:**

```bash
go test ./...
```

Useful targeted examples:

```bash
go test ./internal/runner/...
go test ./internal/module/...
go test ./pkg/plugin/sdk/...
```

When you change host-selection behavior, cover embedded `preflight.yml` inventory, no-inventory local fallback, group selectors, host selectors, and verify that `--target local` still forces a local target.

For output and renderer changes, also exercise the simulator so text, TUI, and JSON output stay aligned:

```bash
go test ./internal/output/... ./cmd/...
go run ./tools/sim list
go run ./tools/sim streaming --format tui
go run ./tools/sim streaming-multi-host --format tui --verbose
go run ./tools/sim failures --format text
go run ./tools/sim basic --format json
```

**Run vet:**

```bash
go vet ./...
```

**Run the linter:**

```bash
golangci-lint run
```

**Optional release validation:**

If you are changing packaging, release metadata, or installer behavior, do a local GoReleaser snapshot build:

```bash
goreleaser release --snapshot --clean
```

When release plumbing changes, also check that the snapshot output includes:

- platform archives for each supported OS and architecture
- `preflight_checksums.txt`
- release metadata embedded into `preflight --version`

CI runs tests, linting, and build jobs. Fixing failures locally is usually much faster than waiting for CI.

---

## Architecture

The three-layer model — **Modules → Actions → Playbooks** — is central to every design decision. When adding a feature, identify which layer it belongs to before writing any code.

The single most important invariant: **the runner is always target-agnostic**. No `localhost` assumptions in runner code. Every operation must go through the `Target` interface. Violating this blocks the distributed orchestration path.

Task execution metadata such as `become` lives alongside tasks and targets, not inside module params. When changing execution identity behavior, make sure planning, staging, state hashing, and target execution all stay aligned.

---

## Adding a Module

Modules live in `internal/module/`. Each module must implement the two-method idempotency contract:

```go
Check(ctx context.Context, params Params) (needed bool, err error)
Apply(ctx context.Context, params Params) error
```

Rules:
- `Check` must be safe to call repeatedly and must not modify system state.
- `Apply` must be idempotent — calling it twice must not break anything.
- Built-in modules currently receive `map[string]any`; validate and normalize params early so the module still has a clear internal contract.
- Add a `_windows.go` / `_stub.go` pair if the module is Windows-only so the binary still compiles on other platforms.
- Modules that produce user-visible command output can also implement the optional streaming `ApplyWithOutput(ctx, params, onOutput)` hook via `target.StreamingModule` so renderers receive line-by-line updates during `apply`.

---

## Adding a Standard Library Action

Stdlib actions live in `internal/stdlib/actions/preflight/`. Each action is a directory containing an `action.yml`. They are embedded at build time via `//go:embed`.

```
internal/stdlib/actions/preflight/
└── my-action/
    └── action.yml
```

Stdlib actions use the `preflight/` namespace and are versioned with the binary — there are no independent versions. If a user needs to pin an older version of an action, they should reference it via a remote Git ref instead.

When adding or materially changing a stdlib action, update the public docs that describe it:

- `docs/reference/stdlib-actions.md` for the embedded action surface
- `docs/reference/modules.md` if the action depends on new built-in module fields or behavior

For Windows stdlib actions, prefer a clear scope model over flexible-but-ambiguous toggles. Current-user preferences should apply to the current execution identity; use playbook/task `become` when callers need to target a different user. Reserve machine-scoped behavior for settings that are truly machine or policy backed.

---

## Adding a CLI Command

Commands live in `cmd/`. Each command is a file or subdirectory following Cobra conventions. Register new commands in `cmd/root.go`.

Keep command implementations thin — they should parse flags, set up context, and delegate to `internal/` packages. Business logic does not belong in `cmd/`.

## Adding Or Updating Plugin Support

Plugin executables are part of the public runtime surface, not just a development convenience.

When changing plugin behavior:

- keep built-in module names reserved
- preserve the `Check` then `Apply` execution contract
- preserve the filename ↔ logical-name contract (`preflight-plugin-<name>` must agree with `Name()` and YAML `module:` usage)
- update the plugin author guide in `docs/how-to/write-a-plugin.md`
- update the plugin docs in `docs/how-to/use-plugin-modules.md` and `docs/reference/plugins.md`
- test both normal execution and staged bundle behavior when the change affects discovery or runtime packaging

---

## Code Conventions

- **No `localhost` in runner code.** Always use the `Target` interface.
- **Prefer explicit phases.** Plan, Fetch, Stage, and Apply are distinct. Don't blur them.
- **Idempotency is a contract, not a best-effort.** Every module must have a meaningful `Check`.
- **No unnecessary abstractions.** Don't generalize for hypothetical future requirements. Build what the task needs.
- **Windows-first.** The primary compilation target is `GOOS=windows GOARCH=amd64`. Code that only runs on Windows should be in `_windows.go` files with corresponding stubs.
- **Templating is Jinja2-like**, not Go `text/template`. Don't introduce `{{ }}` syntax that diverges from the spec.

---

## Submitting Changes

1. Fork the repo and create a branch from `main`.
2. Make your changes. Add tests for new behavior.
3. Run local validation before opening the PR:
   `gofmt -w .`, `goimports -w .`, `go test ./...`, `go vet ./...`, and `golangci-lint run`.
4. Open a pull request against `main` with a clear description of what you changed and why.

## Releases

Releases are automated via GoReleaser. To cut a release:

```bash
git tag v1.2.3
git push origin v1.2.3
```

Pushing a `v*` tag triggers the release workflow, which builds Windows, macOS, and Linux archives, generates checksums, signs the checksum artifact with `cosign` using GitHub OIDC, generates a changelog from commit messages, and publishes a GitHub release.

The checksum artifact name is `preflight_checksums.txt`. Installer and smoke-test changes should be verified against that filename and against the release workflow's Linux and Windows install checks.

Use [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, etc.) — GoReleaser groups the changelog by prefix. Tags named `v1.2.3-beta.1` are automatically marked as pre-releases.

For significant changes (new modules, schema changes, changes to the action resolution chain), open an issue first to discuss the approach. Breaking changes to `action.yml` or `playbook.yml` schemas are treated with extra care — the schema is the public API.

---

## Schema Changes

The JSON schemas in `schema/` are the public API contract for action authors. Any breaking change to `action.schema.json` or `playbook.schema.json` breaks every action ever written. Changes to schemas require:

1. An issue documenting the motivation and migration path.
2. A deprecation cycle if the change is breaking.
3. Updated stdlib actions if the schema change affects them.

---

## Reporting Bugs

Open an issue with:
- The `preflight` version (`preflight --version`)
- The OS and Go version
- A minimal reproduction (playbook + inventory, redacted if needed)
- The full output with `--verbose`

---

## License

By contributing, you agree that your contributions will be licensed under the [ISC License](LICENSE).
