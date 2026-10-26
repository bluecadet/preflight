# Execution model

Most of the runner's design follows from one split: what Preflight can know before it touches a machine, and what must wait until execution.

## Plan first, change later

During `plan`, Preflight:

- loads the playbook
- merges imported playbooks
- resolves `uses:` references
- expands actions into a flat task list
- merges variable layers
- builds and validates the task DAG

Planning does not run modules, gather facts, or change a target. Configuration logic stays separate from machine state.

## Actions

Modules are small: one module, one kind of change. Actions sit one level above modules so operators can:

- package repeatable task sequences
- give those sequences named inputs
- share them across playbooks
- pin remote behavior when reproducibility matters

The plan phase expands each `uses:` reference into its tasks before building the DAG. Without actions, every playbook would duplicate its own orchestration logic.

## Action resolution order

Because expansion happens during planning, the plan phase owns a fixed resolver order:

1. embedded stdlib
2. local project `actions/`
3. user cache `~/.preflight/actions`
4. Git-backed remote refs

The order encodes a policy:

- the binary always has a dependable baseline
- project-local actions are easy to author and test
- cached remote content works offline
- remote Git is available without becoming the only source

The embedded stdlib is versioned with the binary, so `preflight/...` actions work without a registry or a first fetch. Stdlib actions cannot be versioned independently; when an action needs its own pinned lifecycle, use a remote ref. See the [stdlib action reference](../reference/stdlib-actions.md) for the current list.

## Pinned remote refs

A remote ref names an action by repository and revision: `host/org/repo[/path/to/action]@revision`. The revision can be a tag, a branch, or a commit SHA. Tags and branches move, so the resolver pins every fetch to its commit SHA and caches that content locally.

`preflight.lock` records the commit resolved for each fetched remote ref, including nested remote actions fetched along the way. A later `plan` reads the cache and lockfile instead of asking the remote again. Disconnected planning, consistent staging, and controlled upgrades all depend on resolution being a lookup against recorded pins. [Update and pin remote actions](../how-to/update-and-pin-actions.md) covers the workflow.

## Facts wait for execution

Many useful conditions depend on the target:

- Windows build number
- disk space
- environment variables
- transport metadata

Gathering them during planning would make planning impure. Preflight keeps fact-dependent expressions unresolved in `plan` output and resolves them during `check` or `apply`. Planning stays cheap and deterministic, and execution still adapts to each host.

## Dry run uses the real module contract

Every module must answer whether a change is needed, so `check` calls the same `Check()` code that `apply` calls before `Apply()`. A dry run therefore exercises the same planning, rendering, dependency, and targeting logic as a real run, with no separate simulation path.

## The plan is a DAG

`depends_on` turns the task list into a graph. The runner uses it to:

- detect cycles before execution
- reject dependencies on unknown tasks
- run tasks in dependency order
- tell tasks skipped because a dependency failed apart from tasks whose `when` condition was false

## Staging uses a rendered plan

`stage` does not archive the source tree for the offline machine to re-plan. It writes the rendered execution plan plus the plugins and secrets needed to run it. Offline execution then involves:

- no fresh action resolution
- no network access
- no drift from changed source files

When an inventory host declares `platform`, stage uses that declaration instead of contacting the host. Otherwise it probes the host. See [Platform fields](../reference/inventory.md#platform-fields).

Secret references (`secret:<name>`) travel in the bundle as `age`-encrypted files, and the applying machine decrypts them with its own identity. Staging refuses tasks that carry a plaintext value under a secret-like key. `--allow-plaintext-secrets-in-bundle` lifts that check and also decrypts every referenced secret into the bundle as plaintext.

## Host fan-out sits above the runner

The runner handles one target. Host selection and concurrency live in the command layer:

- inventory logic resolves selectors and prepares hosts
- transports stay behind the `Target` interface
- the runner handles one plan and one execution context

The cost is a little extra orchestration code in the command layer.
