# Execution Model

The central question in Preflight is not just "how do tasks run?" It is "what should already be knowable before we touch a machine, and what must wait until execution time?"

That question explains most of the runner design.

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

This preserves a clean boundary between configuration logic and machine state.

## Why Facts Wait For Execution

Many useful conditions depend on the target:

- Windows build number
- disk space
- environment variables
- transport metadata

If planning gathered those values, the plan phase would stop being pure. Preflight instead preserves unknown fact-dependent expressions during preview and resolves them only during `check` or `apply`.

That tradeoff is intentional:

- planning stays cheap and deterministic
- execution still gets host-aware behavior

## Why Dry-Run Uses The Real Module Contract

Some tools fake dry-run behavior with a separate code path. Preflight does not.

Dry-run works because modules already have to answer a real question: "is change needed?" The runner can use the same `Check()` path in both dry-run and apply mode.

This makes dry-run more trustworthy because it exercises the same planning, rendering, dependency, and targeting logic as a real run.

## Why The Plan Becomes A DAG

Tasks are not just a list. `depends_on` turns them into a graph.

That matters because the runner needs to:

- detect cycles early
- reject unknown dependencies
- execute tasks in dependency order
- distinguish skipped work caused by dependency failures from tasks that were never applicable

The DAG is one of the places where Preflight becomes more than a simple YAML loop.

## Why State Uses Stable Task Keys

Comparing raw list positions produces noisy diffs. Insert one task near the top of a playbook and suddenly everything below it looks new.

Preflight avoids that by deriving task identities from lineage:

- the playbook task name or module form
- action expansion ancestry
- repeated task disambiguation

That makes state comparisons much more useful after normal refactors.

## Why Staging Uses A Rendered Plan

The stage phase does not archive the original source tree and ask the offline machine to re-plan later. It stages the rendered execution plan plus the runtime pieces needed to execute it.

That choice keeps offline execution predictable:

- no fresh action resolution
- no dependency on live network access
- no accidental drift from changed source files

The cost is that staged bundles cannot safely embed decrypted secrets, so those plans are rejected.

## Why Host Fan-Out Lives Above The Runner

The runner is single-target by design. Host selection, concurrency, and per-host state paths live in the command layer above it.

That split keeps each part simpler:

- inventory logic handles selectors and host preparation
- transports stay behind the `Target` interface
- the runner stays focused on one plan and one execution context

It is a small amount of extra orchestration code in exchange for much clearer boundaries.
