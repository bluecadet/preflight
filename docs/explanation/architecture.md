# Architecture

Preflight separates declaration, resolution, and execution so the same playbook model can eventually run locally, remotely, or in staged offline bundles.

## Why The Project Is Split Into Modules, Actions, And Playbooks

The three-layer structure keeps low-level operations small and reusable:

- **Modules** are the execution primitives. They are compiled into the binary and expose the idempotent `Check()` and `Apply()` contract.
- **Actions** package reusable YAML task sequences behind named inputs.
- **Playbooks** describe the desired outcome for a machine or environment.

This layering matters because each level changes at a different rate:

- Module behavior changes with the binary
- Actions can be shared and versioned
- Playbooks stay specific to a deployment

## Why Idempotency Is A Hard Contract

Every module is expected to answer a simple question before it changes anything:

```text
Is work needed?
```

That contract drives two important behaviors:

- Dry-run mode is a first-class path rather than a separate simulation engine.
- Re-running the same playbook should converge toward no-op behavior instead of repeated mutation.

In the current codebase, the runner always plans first, then executes modules through the `Target` interface, using dry-run mode to stay on the `Check()` side.

## Why The Runner Depends On A `Target`

The runner is intentionally target-agnostic. It does not hardcode "run this on localhost"; instead it operates through `internal/target.Target`.

That design supports:

- local execution
- WinRM and SSH targets
- future agent-based or staged execution without rewriting the planner

The current implementation keeps the runner single-target on purpose. Inventory selection, per-host variable merging, and host concurrency live in a thin orchestration layer above the runner. That split keeps the planner and DAG executor simple while still allowing one command to operate on many hosts.

## The Pipeline: Plan, Fetch, Stage, Apply

Preflight models execution as distinct phases:

| Phase | Responsibility | Current state |
| --- | --- | --- |
| Plan | Parse YAML, resolve actions, expand tasks, resolve variables, validate DAG | Implemented |
| Fetch | Download remote actions into cache and pin them in `preflight.lock` | Implemented |
| Stage | Assemble a self-contained artifact bundle | Stub |
| Apply | Execute the task graph against a target | Implemented |

This is more than an implementation detail. It preserves a clean boundary between:

- pure computation
- dependency acquisition
- packaging
- machine mutation

That separation is especially important for museum and gallery deployments where internet access may be limited or unavailable during rollout.

## Resolver Chain And Embedded Standard Library

Action resolution proceeds in a fixed order:

1. Embedded stdlib
2. Local project actions
3. User cache
4. Git resolver

The embedded standard library gives Preflight a dependable baseline that ships with the binary. That means teams can rely on some core actions without bootstrapping a separate package registry.

The tradeoff is deliberate: stdlib actions are versioned with the binary, not independently.

## Variables And Templates

Variables are merged in layers so overrides stay predictable. For inventory-backed execution, the effective precedence is:

```text
project vars
  -> inventory group vars
    -> inventory host vars
      -> playbook vars
        -> CLI --var flags
```

Template rendering now happens in two stages:

- During `plan`, Preflight expands actions and preserves unresolved `facts.*` expressions so the phase stays pure.
- During `check` and `apply`, Preflight gathers per-host facts and safe target metadata, then performs final template rendering before module execution.

That split is what allows a plan to stay free of target I/O while still supporting `facts.*` and `target.*` in real executions.

## Why Remote Execution Sits Above The Runner

The command layer resolves inventory selectors first, then prepares one host execution context per resolved machine. Each host context carries:

- merged inventory vars
- safe `target.*` metadata
- resolved transport credentials
- a concrete `Target`
- a derived per-host state path

The command then fetches action dependencies once, fans out host work with the configured concurrency limit, and instantiates one runner per host.

This design trades a little orchestration code for clearer boundaries:

- the runner stays focused on one plan and one target
- target transports stay isolated behind `internal/target`
- host-level concurrency stays outside the task DAG executor

## What To Keep In Mind As The Project Grows

Two themes show up throughout the code:

- The public YAML schema is a compatibility boundary.
- The target abstraction is the long-term scaling boundary.

If those stay stable, Preflight can add remote execution, richer Windows-native modules, and staging workflows without forcing users to rewrite their playbooks.
