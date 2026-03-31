# Preflight

Configuration management CLI for Windows exhibit PCs in museum and gallery environments. Compiles to a single static binary with no runtime dependencies.

```
preflight apply playbooks/lobby.yml --target lobby
```

---

## Overview

Preflight is a declarative, idempotent configuration management tool built for managing fleets of Windows PCs — kiosk displays, interactive exhibits, gallery hardware. It takes inspiration from Ansible but is designed around Windows-native primitives and a single redistributable binary that needs no Python, no agent, and no network access at runtime.

**Key properties:**

- **Single binary.** Drop `preflight.exe` on any Windows machine and it runs. No installer, no runtime dependencies, no Python.
- **Idempotent by design.** Every built-in module implements a `Check()` contract. The runner always checks current state before making any change. Running the same playbook twice is safe.
- **Dry-run first-class.** `--check` mode calls `Check()` on every task and reports what would change — it never modifies the system. `diff` shows you the specific changes.
- **Offline-capable.** The standard library ships embedded in the binary. Remote action refs are fetched once and cached. A staged bundle can be applied with no network access at all.
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

Remote sources are fetched once and pinned by exact Git SHA in `preflight.lock`.

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

**macOS / Linux:**

```sh
curl -fsSL https://raw.githubusercontent.com/claytercek/preflight/main/install.sh | sh
```

Installs to `/usr/local/bin` by default. Override with `PREFLIGHT_INSTALL_DIR=/your/path`.

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/claytercek/preflight/main/install.ps1 | iex
```

Installs to `%LOCALAPPDATA%\preflight\` and adds it to your user PATH.

Or download a specific release manually from the [releases page](../../releases).

### Build from source

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

---

## Usage

### Apply a playbook

```bash
preflight apply playbooks/lobby.yml
preflight apply playbooks/lobby.yml --target lobby-pc-01
preflight apply playbooks/gallery.yml --target gallery --var content_root=D:\\content
```

### Dry-run / check mode

```bash
preflight check playbooks/lobby.yml
preflight apply playbooks/lobby.yml --check
```

### See what would change

```bash
preflight diff playbooks/lobby.yml --target lobby-pc-01
```

### Plan without executing

```bash
preflight plan playbooks/lobby.yml
```

Resolves the full execution plan and prints it — no connection to targets required.

### Validate configuration

```bash
preflight validate playbooks/lobby.yml
```

Parses and validates playbook, inventory, and action schemas without executing anything.

### Gather facts

```bash
preflight facts                    # local machine
preflight facts lobby-pc-01        # remote target
```

Returns a JSON object of system facts (OS version, disk layout, environment) used in `when:` conditions.

### Manage actions

```bash
preflight action list
preflight action info preflight/kiosk-mode
preflight action fetch github.com/myorg/actions/signage@v2.1
```

### Inspect state

```bash
preflight state show
```

Prints the result of the last apply from `state/provision.json`.

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
      - name: lobby-pc-02
        address: 192.168.1.11
        transport: winrm

  gallery-2:
    vars:
      resolution: "1920x1080"
    hosts:
      - name: gallery2-pc-01
        address: 192.168.1.20
        transport: winrm
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

---

## Execution Pipeline

Preflight processes every playbook through four explicit phases:

| Phase | What it does |
|---|---|
| **Plan** | Parse playbook, resolve action refs, expand into a flat task DAG, validate inputs. Pure computation — no I/O against targets. |
| **Fetch** | Download remote action refs not in cache. Network-heavy; runs once for N targets. |
| **Stage** | Assemble a self-contained bundle (ZIP) that can be pushed to air-gapped targets. |
| **Apply** | Execute the task graph. For each task: `Check()`, skip if already correct, `Apply()` if change needed, record result. |

Run only up to a specific phase with `--phase plan|fetch|stage|apply`.

---

## Global Flags

| Flag | Description |
|---|---|
| `-t, --target` | Target host(s) or group(s) from inventory |
| `-e, --var key=value` | Override a variable |
| `--tags tag1,tag2` | Only run tasks with these tags |
| `--skip-tags tag1` | Skip tasks with these tags |
| `--check` | Dry-run mode — check without applying |
| `--diff` | Show file diffs |
| `-v, --verbose` | Verbose output |
| `--output text\|json\|jsonl` | Output format |
| `--concurrency N` | Max parallel targets (0 = unlimited) |
| `--timeout duration` | Execution timeout |
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
