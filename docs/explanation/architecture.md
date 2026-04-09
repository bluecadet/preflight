# Architecture

Preflight is designed for a specific operational niche: Windows exhibit machines that need repeatable configuration, low ceremony, and the option to run in constrained environments.

The architecture reflects that goal. It favors explicit phases, a target-agnostic runner, and a small number of durable abstractions over a large amount of hidden magic.

## The Core Model: Modules, Actions, Playbooks

Preflight splits configuration into three layers:

- **Modules** are the execution primitives. They are small units of behavior compiled into the binary or supplied by plugins.
- **Actions** are reusable YAML bundles of tasks with typed inputs.
- **Playbooks** are the top-level declarations for a machine or environment.

That layering matters because each level changes at a different rate:

- Module behavior is versioned with the binary.
- Actions are a sharing and composition surface.
- Playbooks remain local to an environment or deployment.

Keeping those concerns separate makes the system easier to evolve without turning every change into a schema break.

## Why Idempotency Is A Contract

Every module must implement `Check()` and `Apply()`.

That is not just a stylistic choice. It is the foundation for:

- dry-run mode without a parallel simulation engine
- safe repeated applies
- meaningful state snapshots
- predictable plugin behavior

The runner does not assume that applying twice is harmless. Instead, it requires each module to tell the truth about whether work is needed before it mutates anything.

## Why The Runner Is Target-Agnostic

The runner is always constructed with a `Target`. It does not hardcode localhost assumptions.

That decision protects the long-term shape of the project:

- local execution uses the same pipeline as remote execution
- WinRM and SSH become transport choices instead of alternate runners
- staged bundles can execute through the same abstractions
- plugins fit into the same module contract regardless of transport

The command layer resolves hosts and builds target implementations. The runner then receives one target and stays focused on planning and execution for that single context.

## Package Responsibilities

The codebase is intentionally split into a handful of major responsibilities:

| Path | Responsibility |
| --- | --- |
| [`cmd/`](../../cmd) | Cobra command surface and host orchestration |
| [`internal/runner/`](../../internal/runner) | Planning, DAG building, staging, applying, state |
| [`internal/action/`](../../internal/action) | Playbook loading, action resolution, remote refs, lockfile |
| [`internal/module/`](../../internal/module) | Built-in module implementations |
| [`internal/target/`](../../internal/target) | Target interface plus local, WinRM, SSH, and plugin module adapters |
| [`internal/template/`](../../internal/template) | Variable layering and template rendering |
| [`internal/inventory/`](../../internal/inventory) | Inventory parsing and selector resolution |
| [`internal/facts/`](../../internal/facts) | Fact gathering and normalization |
| [`internal/output/`](../../internal/output) | Text, TUI, and JSON renderers |
| [`internal/plugins/`](../../internal/plugins) | Plugin discovery and registry construction |
| [`internal/bundle/`](../../internal/bundle) | Staged bundle format and extraction |
| [`pkg/plugin/sdk/`](../../pkg/plugin/sdk) | Go plugin author SDK |

## Why The Phases Are Explicit

Preflight models four phases:

```text
Fetch -> Plan -> Stage -> Apply
```

That separation is about more than code organization.

- **Fetch** is dependency acquisition and cache pinning.
- **Plan** is pure computation and should stay safe to run anywhere.
- **Stage** is packaging for disconnected or delayed execution.
- **Apply** is the only phase that should mutate machines.

This is especially valuable in environments where deployment often crosses network boundaries and operational windows are tight, such as museums, galleries, digital signage, and other managed-endpoint fleets.

## Why The Embedded Stdlib Ships With The Binary

The embedded stdlib gives users a baseline action library without introducing a package registry as a prerequisite.

That is why the `preflight/` namespace resolves first and is versioned with the binary. The tradeoff is deliberate:

- users get predictable built-in actions
- maintainers keep version compatibility simple
- environments that need independently pinned behavior can still use remote actions and `preflight.lock`

## Why Plugins Are Executables

Go shared-object plugins are not the right portability story for Windows. Executable plugins speaking JSON-RPC keep the extension model cross-language and Windows-friendly.

That choice also keeps the built-in module API honest. If an external process can implement the same idempotent contract, then the core abstractions are staying small and composable.

## The Design Boundary To Protect

Two boundaries matter more than almost anything else as the project grows:

- the public YAML schema
- the `Target` abstraction

If those stay stable, Preflight can add richer modules, transports, staging flows, and plugins without forcing users to rewrite their playbooks or abandon the core execution model.
