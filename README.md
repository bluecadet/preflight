# Preflight

Configuration management CLI for Windows exhibit PCs in museum and gallery environments. Compiles to a single static binary with no runtime dependencies.

```
preflight apply playbooks/lobby.yml
```

---

## Overview

Preflight is a declarative, idempotent configuration management tool built for managing fleets of Windows PCs — kiosk displays, interactive exhibits, gallery hardware. It takes inspiration from Ansible but is designed around Windows-native primitives and a single redistributable binary that needs no Python, no target-side agent, and no extra runtime dependencies on the managed hosts.

**Key properties:**

- **Single binary.** Drop `preflight.exe` on any Windows machine and it runs. No installer, no runtime dependencies, no Python.
- **Idempotent by design.** Every built-in module implements a `Check()` contract. The runner always checks current state before making any change. Running the same playbook twice is safe.
- **Dry-run first-class.** `--check` mode calls `Check()` on every task and reports what would change without modifying the system. `diff` compares the current plan to recorded state.
- **Inventory-backed execution.** The CLI can resolve inventory groups and hosts, render plans per host, gather facts at execution time, and run against WinRM or SSH targets.
- **Structured output.** `--output json` / `--output jsonl` for CI integration and log pipelines.

---

## Concepts

Preflight has three layers: **Modules → Actions → Playbooks**.

### Modules

The lowest layer. Go code compiled into the binary. Each module encapsulates a single primitive Windows operation — write a registry key, manage a service, copy a file, run a PowerShell snippet. Every module implements:

```
Check() → (needed bool, err)   // Is the system already in desired state?
Apply() → err                  // Make it so
```

The runner always calls `Check` first and skips `Apply` if nothing needs to change.

Built-in modules: `registry`, `service`, `file`, `directory`, `package`, `shortcut`, `scheduled_task`, `user`, `windows_feature`, `environment`, `firewall_rule`, `powershell`, `shell`, `reboot`, `wait`.

### Actions

The middle layer. Parameterized, reusable bundles of tasks defined in YAML. Actions are the unit of sharing and versioning — similar to GitHub Actions or Ansible roles. An action takes typed inputs, runs a sequence of tasks (which can themselves call other actions), and optionally emits outputs.

Actions are resolved from a chain of sources:

```
1. Embedded stdlib     preflight/kiosk-mode
2. Local project       ./actions/myorg/display-config/
3. User cache          ~/.preflight/actions/myorg/name@v1.2/
4. Remote Git          github.com/myorg/actions/signage@v2.1
```

Remote action refs can be fetched into the local cache with `preflight action fetch` or automatically during `preflight apply` / `--phase fetch`. The project `preflight.lock` file records the exact commit SHA used for each fetched ref.

### Playbooks

The top layer. What you write per machine or per environment. A playbook declares what actions to run, with what variables, against which targets.

```yaml
name: lobby-baseline
vars:
  resolution: "3840x2160"

tasks:
  - name: Apply kiosk mode
    uses: preflight/kiosk-mode
    with:
      autologin_user: exhibituser

  - name: Configure display
    uses: myorg/display-config
    with:
      resolution: "{{ vars.resolution }}"
```

---

## Installation

Most users should install a precompiled release. Building from source is mainly for contributors or for testing unreleased changes.

**macOS / Linux:**

```sh
curl -fsSL https://raw.githubusercontent.com/bluecadet/preflight/main/install.sh | sh
```

Installs to `/usr/local/bin` by default. Override with `PREFLIGHT_INSTALL_DIR=/your/path`.

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/bluecadet/preflight/main/install.ps1 | iex
```

Installs to `%LOCALAPPDATA%\preflight\` and adds it to your user PATH.

Or download a specific release manually from the [releases page](../../releases).

### Verify a release

Releases are published with checksums and a `cosign` Sigstore bundle for the checksum artifact.

For the most controlled install flow:

1. Download the archive for your platform from the release page.
2. Download the checksum file and its `.sigstore.json` bundle.
3. Verify the archive checksum against the checksum file.
4. Verify the checksum file with `cosign verify-blob`.

See [`docs/how-to/install-preflight.md`](docs/how-to/install-preflight.md) for the full verification flow and current archive naming.

### Build from source

Use this path if you are contributing to Preflight or need a local build from the current branch.

```bash
# Windows (primary target)
GOOS=windows GOARCH=amd64 go build -o dist/preflight-windows-amd64.exe .
GOOS=windows GOARCH=arm64 go build -o dist/preflight-windows-arm64.exe .

# macOS / Linux
go build -o dist/preflight .
```

Or via Make:

```bash
make build-windows-amd64
make build-windows-arm64
make build-local
```

## Documentation

Additional docs live in [`docs/`](docs/):

- [Install Preflight](docs/how-to/install-preflight.md)
- [Quickstart](docs/tutorials/quickstart.md)
- [Run a playbook](docs/how-to/run-a-playbook.md)
- [Run a playbook against remote hosts](docs/how-to/remote-execution.md)
- [Manage secrets](docs/how-to/manage-secrets.md)
- [Secrets and `age`](docs/explanation/secrets-and-age.md)
- [Architecture](docs/explanation/architecture.md)
- [CLI reference](docs/reference/cli.md)
- [YAML reference](docs/reference/yaml.md)

---

## Usage

### Apply a playbook

```bash
preflight apply playbooks/lobby.yml
preflight apply playbooks/gallery.yml --var content_root=D:\\content
preflight apply playbooks/lobby.yml --target lobby --inventory inventory.yml
```

### Dry-run / check mode

```bash
preflight check playbooks/lobby.yml
preflight apply playbooks/lobby.yml --check
```

### See what would change

```bash
preflight diff playbooks/lobby.yml
```

### Plan without executing

```bash
preflight plan playbooks/lobby.yml
preflight plan playbooks/lobby.yml --target lobby --inventory inventory.yml
```

Resolves the execution plan and prints it. With inventory-backed runs, the output is grouped by resolved host. Fact-dependent expressions stay unresolved until execution time.

### Validate configuration

```bash
preflight validate playbooks/lobby.yml
```

Parses and validates playbook, inventory, and action schemas without executing anything.

### Gather facts

```bash
preflight facts                    # local machine
preflight facts local
preflight facts --target lobby --inventory inventory.yml
```

Returns system facts (OS version, disk layout, environment) used in `when:` conditions. For multiple resolved hosts, the output is a JSON object keyed by host name.

### Manage actions

```bash
preflight action list
preflight action info preflight/kiosk-mode
preflight action fetch github.com/myorg/actions/signage@v2.1
```

### Manage secrets

```bash
preflight secret list
preflight secret encrypt autologin-password --from-file ./secrets/autologin-password.txt --recipient age1...
preflight secret edit autologin-password
```

### Inspect state

```bash
preflight state show
preflight state show --state-file state/targets/lobby-pc-01.json
```

Prints the recorded state file. Local applies default to `state/provision.json`. Inventory-backed applies write per-host state files under `state/targets/<host>.json`.

---

## Project Layout

```
project/
├── preflight.yml          # project-level config and vars
├── inventory.yml          # target machine definitions
├── preflight.lock         # pinned remote action SHAs
├── playbooks/
│   ├── lobby.yml
│   ├── gallery.yml
│   └── base.yml           # shared baseline, imported by others
├── actions/               # local custom actions
│   └── myorg/
│       └── display-config/
│           └── action.yml
└── vars/
    ├── prod.yml
    └── staging.yml
```

### preflight.yml

```yaml
project: natural-history-museum
environment: production

vars:
  content_root: "C:\\Exhibits\\content"
  app_root: "C:\\Exhibits\\app"
  fileserver: "\\\\nas01\\exhibits"

secrets:
  identity: ".age/keys.txt"
  recipients:
    - "age1ql3z7hjy54pw5k8kr0jsjrl4f8yl0v0l7x7y9h8n5v9s0k4m5qkq9v9abc"
  entries:
    autologin-password:
      file: "secrets/autologin-password.age"
    lobby-ssh-key:
      file: "secrets/lobby-ssh-key.age"
      type: "file"
```

### inventory.yml

```yaml
groups:
  all:
    vars:
      timezone: "America/New_York"

  lobby:
    vars:
      resolution: "3840x2160"
    hosts:
      - name: lobby-pc-01
        address: 192.168.1.10
        transport: winrm
        username: exhibit-admin
        password_from: secret:autologin-password
      - name: lobby-pc-02
        address: 192.168.1.11
        transport: winrm

  gallery-2:
    vars:
      resolution: "1920x1080"
    hosts:
      - name: gallery2-pc-01
        address: 192.168.1.20
        transport: ssh
        username: exhibit
        private_key_from: secret:lobby-ssh-key
```

### action.yml

```yaml
name: myorg/display-config
version: 1.0.0
description: Configure display resolution and orientation

inputs:
  resolution:
    type: string
    required: true
    description: "Target resolution, e.g. 1920x1080"
  orientation:
    type: string
    default: landscape

tasks:
  - name: Set resolution via PowerShell
    powershell:
      script: |
        Set-DisplayResolution -Width {{ vars.resolution | split('x')[0] }} -Height {{ vars.resolution | split('x')[1] }}
    when: "{{ facts.os.version >= '10' }}"
```

---

## Variable Precedence

Later entries win:

```
Built-in defaults
  ← preflight.yml
    ← inventory group vars
      ← inventory host vars
        ← playbook vars
          ← --var CLI flags
```

Templates use Jinja2-like syntax: `{{ vars.foo }}`, `{{ facts.os.version }}`.

For secrets, prefer explicit secret references such as `password_from: secret:autologin-password` or `private_key_from: secret:lobby-ssh-key` instead of committing plaintext values.

### Secrets

Preflight supports repo-backed secrets encrypted with `age`.

- Encrypted secret files are declared in `preflight.yml` under `secrets.entries`.
- Secret references use `provider:name` syntax. The built-in repo-backed provider is `secret`, so refs look like `secret:autologin-password`.
- Decryption happens on whichever machine is running `preflight` at execution time.
- If you copy a project to a target PC and run `preflight apply` there, that target PC needs a private age identity matching one of the configured recipients.
- Plans, runner state, and renderer output do not persist decrypted secret values.
- Plaintext fields like `password` and `private_key` still work for compatibility, but `*_from` fields are the preferred API.

---

## Execution Pipeline

Preflight processes every playbook through four explicit phases:

| Phase | What it does |
|---|---|
| **Plan** | Parse playbook, resolve cached action refs, expand into a flat task DAG, validate inputs. Pure computation — no network or target I/O. |
| **Fetch** | Download remote actions into the local cache and update `preflight.lock`. |
| **Stage** | Reserved for future artifact bundles. It currently returns an explicit not-implemented error. |
| **Apply** | Fetch remote dependencies, build the execution plan, then execute the task graph. For each task: `Check()`, skip if already correct, `Apply()` if change needed, record result. |

Run only up to a specific phase with `--phase plan|fetch|stage|apply`.

For inventory-backed runs, the CLI resolves the selected hosts first, fetches shared action dependencies once, then creates one single-target runner per host. That keeps planning and task execution target-agnostic while still allowing host-level concurrency and per-host state files.

---

## Global Flags

| Flag | Description |
|---|---|
| `-t, --target` | Select inventory hosts or groups; repeat to combine selectors |
| `--inventory` | Inventory file path for inventory-backed commands |
| `-e, --var key=value` | Override a variable |
| `--tags tag1,tag2` | Only run tasks with these tags |
| `--skip-tags tag1` | Skip tasks with these tags |
| `--check` | Dry-run mode — check without applying |
| `--diff` | Show file diffs |
| `-v, --verbose` | Verbose output |
| `--output text\|json\|jsonl` | Output format |
| `--concurrency N` | Max number of hosts to execute in parallel; `0` means unlimited |
| `--timeout duration` | Overall execution timeout |
| `--phase` | Run only up to this phase |

---

## Standard Library

The `preflight/` namespace is embedded in the binary and always available without fetching.

| Action | Description |
|---|---|
| `preflight/autologin` | Set up automatic login |

---

## Plugin System

Preflight supports external plugins that extend the module library. Plugins are executables (any language) that speak JSON-RPC over stdin/stdout. They are discovered from:

1. Alongside the binary
2. `~/.preflight/plugins/`
3. `./plugins/` in the project directory

The `pkg/plugin/sdk` package provides a Go SDK for plugin authors that handles protocol boilerplate.

---

## Building & Testing

```bash
go test ./...                       # all tests
go test ./internal/runner/...       # specific package
go vet ./...                        # vet
```

---

## License

[ISC](LICENSE)
