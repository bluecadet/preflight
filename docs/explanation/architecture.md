# Architecture

Preflight configures Windows exhibit machines, signage, and kiosks that need repeatable setup with little ceremony, sometimes on networks the operator cannot reach. The design favors explicit phases, a runner that does not know which transport it uses, and a few stable abstractions.

The docs use one term throughout: the controller is the machine that runs the `preflight` CLI, usually an operator's workstation or a CI runner. It can run Windows, macOS, or Linux. Targets always run Windows.

## Modules, actions, and playbooks

Preflight splits configuration into three layers, each changing at its own rate:

- Modules are the execution primitives, compiled into the binary or supplied by plugins. Their behavior is versioned with the binary.
- Actions are reusable YAML bundles of tasks with typed inputs. They are the unit of sharing.
- Playbooks declare the tasks for one machine or environment and stay local to a deployment.

Keeping the layers separate lets each evolve without forcing schema breaks on the others.

## Idempotency as a contract

Every module implements `Check()` and `Apply()`. The runner does not assume that applying twice is harmless; each module must report whether work is needed before it changes anything. That contract gives Preflight:

- a dry run without a separate simulation engine
- safe repeated applies
- the same behavior from plugins as from built-ins

## A target-agnostic runner

The runner receives one `Target` and plans and executes for that target alone. It has no localhost assumptions, so:

- local execution uses the same pipeline as remote execution
- WinRM and SSH are transport choices, not alternate runners
- staged bundles run through the same abstractions
- plugins keep one module contract across transports

The command layer resolves inventory hosts, builds a target for each, and runs them concurrently.

## Package responsibilities

| Path | Responsibility |
| --- | --- |
| `cmd/` | Cobra commands and host orchestration |
| `internal/runner/` | Planning, DAG building, staging, and applying |
| `internal/action/` | Playbook loading, action resolution, remote refs, lockfile |
| `internal/target/` | `Target` interface, the Windows PowerShell module registry, and local, WinRM, SSH, and plugin adapters |
| `internal/template/` | Variable layering and template rendering |
| `internal/inventory/` | Inventory parsing and selector resolution |
| `internal/facts/` | Fact gathering and normalization |
| `internal/secrets/` | `age` secret resolution |
| `internal/output/` | Text, TUI, and JSON renderers |
| `internal/plugins/` | Plugin discovery and registry construction |
| `internal/bundle/` | Staged bundle format and extraction |
| `pkg/plugin/sdk/` | Go SDK for plugin authors |

## Explicit phases

```text
Fetch -> Plan -> Stage -> Apply
```

- Fetch acquires remote actions and pins them in the cache and lockfile.
- Plan is pure computation and is safe to run anywhere.
- Stage packages a plan for disconnected or delayed execution.
- Apply is the only phase that changes machines.

The split matters most where deployments cross network boundaries and maintenance windows are short, as in museums, galleries, and signage networks. [Execution model](./execution-model.md) covers each phase in detail.

## An embedded stdlib

The embedded stdlib gives every install a baseline action library without a package registry. The `preflight/` namespace resolves first and is versioned with the binary:

- built-in actions behave the same on every machine running a given release
- maintainers have one version to keep compatible
- projects that need independently pinned behavior can use remote actions and `preflight.lock`

## Plugins as executables

Go shared-object plugins do not work on Windows. Standalone executables speaking JSON-RPC over stdin and stdout work on every platform and in any language.

Executable plugins also keep the built-in module API honest: if an external process can implement the same idempotent contract, the core abstractions are small enough.

## Boundaries to protect

- the public YAML schema
- the `Target` abstraction

While both stay stable, Preflight can add modules, transports, staging flows, and plugins without forcing users to rewrite playbooks.
