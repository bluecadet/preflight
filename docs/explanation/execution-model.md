# Execution Model

The central question in Preflight is not just "how do tasks run?" It is
"what should already be knowable before we touch a machine, and what
must wait until execution time?"

That question explains most of the runner design, from planning through
the way actions are resolved and pinned before a plan ever touches a
host.

## Plan First, Mutate Later

The runner treats planning as a first-class phase.

During `plan`, Preflight:

- loads the playbook
- merges any imported playbooks
- resolves `uses:` references
- expands actions into a flat task list
- merges variable layers
- builds and validates the task DAG

What it does not do:

- contact hosts
- gather facts
- decrypt secrets unnecessarily
- mutate the target

This preserves a clean boundary between configuration logic and machine
state.

## Why Actions Exist

Modules are intentionally small. They are the primitive operations —
one module, one kind of change. Actions sit one level above modules so
operators can:

- package repeatable task sequences
- give those sequences named inputs
- share them across playbooks
- pin remote behavior when reproducibility matters

Resolving a `uses:` reference into its expanded tasks is part of what
the plan phase above does before it builds the DAG. Without actions,
every playbook would have to duplicate its own orchestration logic
instead of expanding a shared one.

## Action Resolution Order

Because action expansion happens during planning, the plan phase also
owns a fixed resolver order:

1. embedded stdlib
2. local project `actions/`
3. user cache `~/.preflight/actions`
4. Git-backed remote refs

That order encodes a policy, not just an implementation detail:

- the binary always has a dependable baseline
- project-local work stays easy to author and test
- cached remote content can be reused offline
- remote Git is available without becoming the only source of truth

The embedded stdlib is versioned with the binary, so `preflight/...`
actions work without a registry or a first fetch. The cost is that
stdlib actions do not get independent versioning — when an operator
needs a separately pinned lifecycle for an action, a remote ref is the
better fit than waiting for a stdlib change. See
[Stdlib Actions Reference](../reference/stdlib-actions.md) for the
current list.

## Why Remote Refs Are Pinned

Remote refs identify an action by repository and revision:
`host/org/repo[/path/to/action]@revision`. A revision can be a tag, a
branch, or a commit SHA. Floating refs such as tags or branches are
convenient to write but bad for reproducibility on their own, so the
resolver always pins what it fetches to the exact commit SHA and caches
that content locally.

`preflight.lock` is what makes that pin durable across runs and across
controllers. It records the exact commit resolved for each fetched
remote ref, which gives the project two properties a floating ref
cannot on its own:

- repeatable future resolution — a later `plan` reads the cache and the
  lockfile instead of re-resolving the ref against the remote
- a clear record of what remote content was actually used, including
  any nested remote actions fetched recursively along the way

That is why the resolver, cache, and lockfile behave together like a
pinned dependency model rather than a "latest by default" package
lookup: disconnected planning, consistent staging, and controlled
upgrades all depend on resolution being a lookup against recorded
state, not a fresh network decision every time. The operational
workflow for fetching, inspecting, and moving a pinned ref forward is
covered in
[Update And Pin Remote Actions](../how-to/update-and-pin-actions.md).

## Why Facts Wait For Execution

Many useful conditions depend on the target:

- Windows build number
- disk space
- environment variables
- transport metadata

If planning gathered those values, the plan phase would stop being
pure. Preflight instead preserves unknown fact-dependent expressions
during preview and resolves them only during `check` or `apply`.

That tradeoff is intentional:

- planning stays cheap and deterministic
- execution still gets host-aware behavior

## Why Dry-Run Uses The Real Module Contract

Some tools fake dry-run behavior with a separate code path. Preflight
does not.

Dry-run works because modules already have to answer a real question:
"is change needed?" The runner can use the same `Check()` path in both
dry-run and apply mode.

This makes dry-run more trustworthy because it exercises the same
planning, rendering, dependency, and targeting logic as a real run.

## Why The Plan Becomes A DAG

Tasks are not just a list. `depends_on` turns them into a graph.

That matters because the runner needs to:

- detect cycles early
- reject unknown dependencies
- execute tasks in dependency order
- distinguish skipped work caused by dependency failures from tasks
  that were never applicable

The DAG is one of the places where Preflight becomes more than a
simple YAML loop.

## Why State Uses Stable Task Keys

Comparing raw list positions produces noisy diffs. Insert one task near
the top of a playbook and suddenly everything below it looks new.

Preflight avoids that by deriving task identities from lineage:

- the playbook task name or module form
- action expansion ancestry
- repeated task disambiguation

That makes state comparisons much more useful after normal refactors.

## Why Staging Uses A Rendered Plan

The stage phase does not archive the original source tree and ask the
offline machine to re-plan later. It stages the rendered execution plan
plus the runtime pieces needed to execute it.

That choice keeps offline execution predictable:

- no fresh action resolution
- no dependency on live network access
- no accidental drift from changed source files

The cost is that staged bundles cannot safely embed decrypted secrets,
so those plans are rejected.

## Why Host Fan-Out Lives Above The Runner

The runner is single-target by design. Host selection, concurrency, and
per-host state paths live in the command layer above it.

That split keeps each part simpler:

- inventory logic handles selectors and host preparation
- transports stay behind the `Target` interface
- the runner stays focused on one plan and one execution context

It is a small amount of extra orchestration code in exchange for much
clearer boundaries.
