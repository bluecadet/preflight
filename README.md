# Preflight

<p align="center">
  <img src="assets/header.gif" alt="preflight apply across a mixed-transport fleet" width="640" />
</p>

Preflight is a configuration management CLI for Windows endpoints such as kiosks, signage, and exhibit PCs. It ships as a single static binary. Targets must run Windows; macOS and Linux machines can act as controllers.

```bash
preflight apply playbooks/lobby.yml
```

## Concepts

- Modules are the execution primitives, built in or supplied by plugins.
- Actions are reusable YAML bundles of tasks with typed inputs.
- Playbooks declare the tasks for a machine or environment.

Every module implements the same contract:

```text
Check() -> (needsChange, err)
Apply() -> err
```

Dry runs call `Check()` on the real modules, so a dry run exercises the same code as an apply.

## Execution model

```text
Fetch -> Plan -> Stage -> Apply
```

- Fetch downloads remote action refs into the local cache and pins their commit SHAs in `preflight.lock`.
- Plan loads YAML, merges imports, resolves actions, expands tasks, and builds a DAG without contacting targets.
- Stage writes a per-target bundle with the rendered plan, a manifest, any referenced plugins, and encrypted copies of the secrets the plan uses.
- Apply gathers facts, renders templates that use `target`, `facts`, and `env`, runs tasks, and writes a run log.

## Example

```yaml
name: quickstart

tasks:
  - name: Apply machine baseline
    uses: preflight/windows-machine
    with:
      computer_name: LOBBY-KIOSK-01
      timezone: Eastern Standard Time

  - name: Create content directory
    directory:
      path: "C:\\Exhibits\\Content"
      ensure: present
```

## Installation

### macOS and Linux

```bash
curl -fsSL https://preflight.bluecadet.com/install.sh | sh
```

### Windows PowerShell

```powershell
irm https://preflight.bluecadet.com/install.ps1 | iex
```

For checksum verification and source builds, see [Install Preflight](docs/how-to/install-preflight.md).

## Common commands

```bash
preflight validate playbooks/lobby.yml
preflight plan playbooks/lobby.yml
preflight check playbooks/lobby.yml
preflight apply playbooks/lobby.yml
preflight plugin list
preflight action list
```

With an inventory:

```bash
preflight plan playbooks/lobby.yml --target lobby
preflight apply playbooks/lobby.yml --target lobby
preflight facts --target lobby
```

When `preflight.yml` contains an `inventory:` block, omitting `--target` runs against every inventory host. `--target local` runs on the controller, which must be a Windows machine.

Air-gapped flow:

```bash
preflight stage playbooks/lobby.yml
preflight apply --bundle dist/bundles/<bundle>.zip
```

## Docs

- [Quickstart](docs/tutorials/quickstart.md)
- [Run a playbook](docs/how-to/run-a-playbook.md)
- [Run against remote hosts](docs/how-to/remote-execution.md)
- [Troubleshoot remote connections](docs/how-to/troubleshoot-remote-connections.md)
- [Deploy across restricted networks](docs/explanation/restricted-network-deployment.md)
- [Manage secrets](docs/how-to/manage-secrets.md)
- [Write a plugin](docs/how-to/write-a-plugin.md)
- [Write an action](docs/how-to/write-an-action.md)
- [CLI reference](docs/reference/cli.md)
- [Embedded stdlib action reference](docs/reference/stdlib-actions.md)
- [Built-in module reference](docs/reference/modules.md)
- [Choosing Preflight](docs/explanation/why-preflight.md)
- [Architecture](docs/explanation/architecture.md)

## Current scope

- Local, SSH, and WinRM targets, with host and group selection from inventory
- Per-user settings via `user:` on the `registry` and `environment` modules and the `windows-shell`, `windows-input`, `windows-power`, `windows-quiet-mode`, and `debloat` stdlib actions, loading the profile hive on demand
- Action resolution from the embedded stdlib, the project, the user cache, and Git
- Embedded stdlib actions for machine baseline, shell, input, quiet mode, Windows Update, power, autologin, debloat, and Git sync
- Repo-backed `age` secrets
- Plugin modules discovered as standalone executables
- Bundle staging and offline bundle apply
- `text`, `tui`, and `json` output, with task output streamed during apply

Known limits:

- Targets must run Windows. SSH targets must provide Windows PowerShell.
- The local target and `apply --bundle` require a Windows controller.
- Some modules cannot finish over WinRM; see [WinRM session limitations](docs/reference/modules.md#winrm-session-limitations).

## Build and test

```bash
go test ./...
go vet ./...
```

```bash
GOOS=windows GOARCH=amd64 go build -o dist/preflight-windows-amd64.exe .
GOOS=windows GOARCH=arm64 go build -o dist/preflight-windows-arm64.exe .
go build -o dist/preflight .
```

## License

[ISC](LICENSE)
