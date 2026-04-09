# Preflight

Preflight is a Windows-first configuration management CLI for managed endpoints such as kiosks, signage, exhibit PCs, and other dedicated systems. It compiles to a single static binary and is designed around idempotent modules, reusable YAML actions, and explicit execution phases.

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
Fetch -> Plan -> Stage -> Apply
```

- **Fetch** acquires remote action refs into the local cache and records pinned SHAs in `preflight.lock`.
- **Plan** loads YAML, merges imports, resolves actions, expands tasks, and builds a DAG without contacting targets.
- **Stage** writes a per-target bundle containing the staged plan (task DAG), manifest, referenced plugins, and any bundled secrets needed for offline apply.
- **Apply** gathers facts, renders execution-time templates using `target`, `facts`, and `env`, executes tasks, and records state.

## Quick Example

```yaml
name: quickstart

tasks:
  - name: Apply machine baseline
    uses: preflight/windows-machine
    with:
      computer_name: LOBBY-KIOSK-01
      timezone: Eastern Standard Time

  - name: Create content directory
    become:
      user: exhibit
    directory:
      path: "C:\\Exhibits\\Content"
      ensure: present
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
preflight apply playbooks/lobby.yml --inventory inventory.yml
preflight plan playbooks/lobby.yml --target lobby --inventory inventory.yml
preflight apply playbooks/lobby.yml --target lobby --inventory inventory.yml
preflight facts --target lobby --inventory inventory.yml
```

When an inventory is available, omitting `--target` fans out to all inventory hosts. Use `--target local` to force a local-only run.

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
- [Validate a WinRM connection from macOS](docs/how-to/validate-winrm-from-macos.md)
- [Deploy across restricted networks](docs/explanation/restricted-network-deployment.md)
- [Manage secrets](docs/how-to/manage-secrets.md)
- [Write a plugin](docs/how-to/write-a-plugin.md)
- [Write an action](docs/how-to/write-an-action.md)
- [CLI reference](docs/reference/cli.md)
- [Embedded stdlib action reference](docs/reference/stdlib-actions.md)
- [Built-in module reference](docs/reference/modules.md)
- [Why use Preflight (and when not to)](docs/explanation/why-preflight.md)
- [Architecture](docs/explanation/architecture.md)

## Current Scope

Implemented today:

- Local execution and inventory-backed host selection
- WinRM and SSH targets
- Task-level `become` with inherited defaults and named-user execution via `runas` on Windows and `sudo -u` on POSIX
- Embedded, local, cached, and Git-backed action resolution
- Embedded Windows baseline stdlib actions for machine, shell, input, quiet mode, updates, power, and apps
- Repo-backed `age` secrets
- Plugin discovery and plugin-backed module execution
- Bundle staging and bundle apply
- Output renderers for `text`, `tui`, and `json`, including streamed task output during apply plus richer TUI layouts for plan, facts, state, validate, and action inspection commands

Important current limits:

- SSH auto-detects either a Windows PowerShell or POSIX shell runtime. Windows-over-SSH supports the built-in Windows module set; POSIX-over-SSH supports `directory`, `file`, `shell`, `wait` (`file_exists`, `port_open`), and `powershell` when `pwsh` or `powershell` is installed. Plugin modules are not yet supported over SSH.

## Build And Test

```bash
go test ./...
go test ./internal/runner/...
go vet ./...
```

For output and renderer work, you can preview scenarios without running a full playbook:

```bash
go run ./tools/sim list
go run ./tools/sim streaming --format tui
go run ./tools/sim streaming-multi-host --format tui --verbose
go run ./tools/sim failures --format json
```

Build commands:

```bash
GOOS=windows GOARCH=amd64 go build -o dist/preflight-windows-amd64.exe .
GOOS=windows GOARCH=arm64 go build -o dist/preflight-windows-arm64.exe .
go build -o dist/preflight .
```

## License

[ISC](LICENSE)
