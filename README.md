# Preflight

Preflight is a Windows-first configuration management CLI for exhibit PCs in museum and gallery environments. It compiles to a single static binary and is designed around idempotent modules, reusable YAML actions, and explicit execution phases.

```bash
preflight apply playbooks/lobby.yml
```

## What It Is

Preflight is built around three layers:

- **Modules** are built-in or plugin-backed execution primitives.
- **Actions** are reusable YAML bundles of tasks with typed inputs.
- **Playbooks** are the top-level machine or environment declarations you run.

Every module follows the same contract:

```text
Check() -> (needsChange, err)
Apply() -> err
```

That makes dry-run mode a real execution path instead of a fake preview path.

## Execution Model

Runs flow through four explicit phases:

```text
Plan -> Fetch -> Stage -> Apply
```

- **Plan** loads YAML, merges imports, resolves actions, expands tasks, and builds a DAG without contacting targets.
- **Fetch** acquires remote action refs into the local cache and records pinned SHAs in `preflight.lock`.
- **Stage** writes a per-target bundle containing the rendered plan, runtime binary, manifest, and referenced plugins.
- **Apply** gathers facts, renders execution-time templates, executes tasks, and records state.

## Quick Example

```yaml
name: quickstart

tasks:
  - name: Create content directory
    directory:
      path: "C:\\Exhibits\\Content"
      ensure: present

  - name: Configure auto-login
    uses: preflight/autologin
    with:
      username: exhibituser
      password_from: secret:autologin-password
```

## Installation

### macOS And Linux

```bash
curl -fsSL https://raw.githubusercontent.com/bluecadet/preflight/main/install.sh | sh
```

### Windows PowerShell

```powershell
irm https://raw.githubusercontent.com/bluecadet/preflight/main/install.ps1 | iex
```

For manual verification and source builds, see [Install Preflight](docs/how-to/install-preflight.md).

## Common Commands

```bash
preflight validate playbooks/lobby.yml
preflight plan playbooks/lobby.yml
preflight check playbooks/lobby.yml
preflight apply playbooks/lobby.yml
preflight state diff playbooks/lobby.yml
preflight plugin list
preflight action list
```

Inventory-backed examples:

```bash
preflight plan playbooks/lobby.yml --target lobby --inventory inventory.yml
preflight apply playbooks/lobby.yml --target lobby --inventory inventory.yml
preflight facts --target lobby --inventory inventory.yml
```

Air-gapped flow:

```bash
preflight stage playbooks/lobby.yml
preflight apply --bundle dist/bundles/<bundle>.zip
```

## Docs

The full docs live under [`docs/`](docs/):

- [Docs index](docs/index.md)
- [Quickstart](docs/tutorials/quickstart.md)
- [Run a playbook](docs/how-to/run-a-playbook.md)
- [Run against remote hosts](docs/how-to/remote-execution.md)
- [Manage secrets](docs/how-to/manage-secrets.md)
- [Write an action](docs/how-to/write-an-action.md)
- [CLI reference](docs/reference/cli.md)
- [Built-in module reference](docs/reference/modules.md)
- [Architecture](docs/explanation/architecture.md)

## Current Scope

Implemented today:

- Local execution and inventory-backed host selection
- WinRM and SSH targets
- Embedded, local, cached, and Git-backed action resolution
- Repo-backed `age` secrets
- Plugin discovery and plugin-backed module execution
- Bundle staging and bundle apply
- Output renderers for `text`, `tui`, `json`, and `jsonl`

Important current limits:

- SSH currently supports `directory`, `file`, and `shell`.
- The embedded stdlib currently ships `preflight/autologin`.
- `--diff` exists on the CLI surface but is not yet wired into task execution output.

## Build And Test

```bash
go test ./...
go test ./internal/runner/...
go vet ./...
```

Build commands:

```bash
GOOS=windows GOARCH=amd64 go build -o dist/preflight-windows-amd64.exe .
GOOS=windows GOARCH=arm64 go build -o dist/preflight-windows-arm64.exe .
go build -o dist/preflight .
```

## License

[ISC](LICENSE)
