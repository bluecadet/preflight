# Why Use Preflight (And When Not To)

Preflight exists for a practical deployment problem: repeatable Windows configuration in environments where reliability, low ceremony, and offline-friendly workflows matter.

This page answers a practical question:

```text
Why use Preflight over other common approaches?
```

## The Short Version

Preflight is a strong fit when you want:

- one static binary with minimal runtime dependencies
- idempotent apply behavior with real dry-run checks
- YAML-defined reusable tasks and playbooks
- explicit `fetch -> plan -> stage -> apply` phases
- an offline-capable staging and bundle workflow

Preflight is a weaker fit when you primarily need:

- deep enterprise policy management across very large fleets
- cloud-native endpoint governance and compliance dashboards
- highly dynamic secrets and centralized secret brokering
- mature Linux-first module ecosystems

## Preflight Vs Ad-Hoc PowerShell Scripts

PowerShell scripts are often the default starting point on Windows. They are powerful, but they usually leave lifecycle concerns up to each script author.

Why you might prefer Preflight over script-only workflows:

- **Idempotency contract:** modules must answer `Check()` before `Apply()`, so dry-run and apply use the same execution model.
- **Reusable composition:** actions package task sequences with typed inputs instead of copy-pasting script fragments between machines.
- **Dependency ordering:** task DAG validation catches bad `depends_on` graphs early.
- **State and diff model:** stable task keys make before/after comparisons useful across playbook refactors.

Where script-only can still be better:

- one-off migration tasks
- highly custom procedural logic not worth modeling as reusable actions
- environments that do not need shared config conventions

## Preflight Vs Ansible (General Purpose Automation)

Ansible has a broad ecosystem and cross-platform depth. Preflight intentionally optimizes for a narrower Windows-first niche.

Why choose Preflight over Ansible in some environments:

- **Windows-focused defaults:** module surface and stdlib actions target common kiosk/signage style Windows tasks.
- **Single static binary:** simpler deployment footprint in constrained or locked-down environments.
- **Offline staging model:** staged bundles are a first-class workflow, not an afterthought.
- **Opinionated execution phases:** clearer boundaries between pure planning and target mutation.

Why choose Ansible instead:

- you need a very large existing module/role ecosystem
- your automation is primarily Linux/Unix infrastructure
- your environment already has strong Ansible operational maturity

## Preflight Vs DSC / Intune / Endpoint Management Platforms

These tools solve adjacent but not identical problems.

Preflight advantages in the environments it targets:

- **Project-local declarative config:** playbooks, actions, and encrypted secrets live with the project repo.
- **Portable execution:** same configuration can run locally, against remote targets, or from staged bundles.
- **Low setup overhead for small/medium deployments:** no requirement to stand up a management plane before first value.

Platform advantages over Preflight:

- centralized policy, reporting, and governance at enterprise scale
- richer inventory/compliance reporting across very large device fleets
- built-in administrative controls and organization-level policy tooling

A hybrid approach is possible: a platform for fleet governance and Preflight for deterministic application/environment shaping.

## Preflight Vs "Just Use CI To Push Scripts"

CI-only orchestration can work, but it tends to conflate planning, artifact preparation, and mutation in one pipeline.

Preflight gives a clearer operational model:

- `plan` can be reviewed without target mutation
- `fetch` and lockfile pinning produce repeatable action resolution
- `stage` makes offline handoff explicit
- `apply` runs the idempotent execution contract on target contexts

That separation is useful when deployment windows are tight and operators need confidence before touching managed endpoints.

## Decision Heuristics

Choose Preflight when most of these are true:

- your critical targets are Windows kiosks, signage systems, exhibit PCs, or other managed endpoints
- you need repeatable, idempotent machine configuration from versioned YAML
- you want dry-run behavior that reflects real execution logic
- your environment benefits from offline-ready staging
- you prefer small operational footprint over large control-plane features

Consider alternatives first when most of these are true:

- you need organization-wide endpoint governance and compliance tooling
- your workloads are mostly Linux/cloud infrastructure automation
- you need heavyweight central policy engines more than portable execution

## Related Docs

- [Architecture](./architecture.md)
- [Execution model](./execution-model.md)
- [Targets, transports, and plugins](./targets-and-transports.md)
- [Execution model](./execution-model.md)
