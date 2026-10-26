# Choosing Preflight

Preflight targets one deployment problem: repeatable Windows configuration where reliability, low ceremony, and offline delivery matter.

## Summary

Preflight fits when you want:

- one static binary with no runtime dependencies
- idempotent applies and dry runs that call the real module checks
- reusable tasks and playbooks defined in YAML
- explicit `fetch -> plan -> stage -> apply` phases
- offline delivery through staged bundles

It fits poorly when you mainly need:

- enterprise policy management across very large fleets
- cloud endpoint governance and compliance dashboards
- dynamic secrets and centralized secret brokering
- Linux or macOS targets

## Preflight vs. ad hoc PowerShell scripts

PowerShell scripts are the usual starting point on Windows, but each script author handles idempotency, ordering, and reuse alone. Preflight adds:

- an idempotency contract: modules answer `Check()` before `Apply()`, so dry runs and applies share one execution model
- composition: actions package task sequences with typed inputs instead of copied script fragments
- dependency ordering: DAG validation rejects broken `depends_on` graphs before anything runs

Scripts alone remain the better choice for:

- one-off migrations
- custom procedural logic not worth modeling as an action
- environments that need no shared conventions

## Preflight vs. Ansible

Ansible covers many platforms with a large module ecosystem. Preflight narrows its scope to Windows endpoints:

- modules and stdlib actions target common kiosk and signage tasks
- a single static binary is easier to deploy into locked-down environments
- staged bundles are a first-class workflow
- phases separate pure planning from target changes

Choose Ansible when:

- you need a large existing module and role ecosystem
- you mostly automate Linux or Unix infrastructure
- your team already runs Ansible well

## Preflight vs. DSC, Intune, and endpoint management platforms

These tools solve adjacent problems. Preflight offers:

- project-local config: playbooks, actions, and encrypted secrets live in the project repo
- portable execution: the same playbook runs locally, against remote targets, or from a staged bundle
- low setup cost for small and medium deployments, with no management plane to stand up

Platforms offer:

- centralized policy, reporting, and governance at enterprise scale
- inventory and compliance reporting across very large fleets
- organization-level administrative controls

The two can coexist: a platform for fleet governance and Preflight for application and environment setup.

## Preflight vs. CI pushing scripts

CI-only orchestration tends to merge planning, artifact preparation, and changes into one pipeline. Preflight separates them:

- `plan` can be reviewed without touching a target
- `fetch` and the lockfile make action resolution repeatable
- `stage` makes offline handoff explicit
- `apply` runs the idempotent module contract on each target

The separation helps when deployment windows are short and operators need confidence before touching endpoints.

## Decision guide

Choose Preflight when most of these hold:

- your critical targets are Windows kiosks, signage, exhibit PCs, or other dedicated endpoints
- you want repeatable machine configuration from versioned YAML
- you want dry runs that reflect real execution logic
- you need offline staging
- you prefer a small operational footprint over control-plane features

Consider alternatives first when most of these hold:

- you need organization-wide endpoint governance and compliance tooling
- your workloads are mostly Linux or cloud infrastructure
- you need a central policy engine more than portable execution

## Related docs

- [Architecture](./architecture.md)
- [Execution model](./execution-model.md)
- [Targets, transports, and plugins](./targets-and-transports.md)
