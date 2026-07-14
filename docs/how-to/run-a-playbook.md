# Run A Playbook

Use this guide when you have a playbook and want to run it on the
controller itself, or against a small default inventory, and need the
validate, plan, check, and apply loop explained end to end. If you are
fanning a playbook out across many inventory hosts, use
[Run a playbook against remote hosts](./remote-execution.md) instead;
this page teaches the loop that guide builds on.

## Prerequisites

- An installed `preflight` binary
- A playbook file
- A `preflight.yml` file if you rely on shared vars, secrets, or a
  default inventory

If you need an end-to-end onboarding path first, use
[Quickstart](../tutorials/quickstart.md).

## 1. Validate The Playbook

```bash
preflight validate playbooks/lobby.yml
```

`validate` parses the playbook and resolves every `uses:` action
reference, recursively, without executing anything. It is the fastest
sanity check, but it is intentionally shallow: it does not gather
facts, contact targets, or prove that a task will succeed at runtime.

## 2. Inspect The Plan

```bash
preflight plan playbooks/lobby.yml
```

`plan` prints the flattened task list after action expansion and
playbook imports, in execution order, with each task's rendered name,
module, `when:` expression, and tags. It stays pure: no target is
contacted, so `{{ facts.* }}` expressions can remain as literal
placeholders until `check` or `apply` runs.

Add a variable at planning time:

```bash
preflight plan playbooks/lobby.yml -e content_root='C:\Exhibits\Content'
```

`-e`/`--var` is repeatable and works the same way on `plan`, `check`,
and `apply`. It is the fastest way to override a single value for one
run without editing the playbook or `preflight.yml`.

## 3. Dry-Run With Check

```bash
preflight check playbooks/lobby.yml
```

`check` runs the real runner pipeline in dry-run mode: dependency
ordering, execution-time template rendering, and fact gathering all
happen, but no module applies a change. Use `check` when you want to
confirm `when:` conditions and rendered values against a real target
before committing to `apply`.

## 4. Apply The Playbook

```bash
preflight apply playbooks/lobby.yml
```

By default, Preflight stops on the first task failure on a target. Set
`ignore_errors: true` on a task only when later tasks should keep
running after that one fails. `--fail-fast` stops the whole run as
soon as any target fails; on a single local target it behaves the same
as the default, but it matters once you fan a playbook out to more
than one host (see
[Run a playbook against remote hosts](./remote-execution.md)).

Override variables the same way as during `plan` and `check`:

```bash
preflight apply playbooks/lobby.yml \
  -e content_root='C:\Exhibits\Content' \
  -e app_env=production
```

Choose an output renderer with `--output text|tui|json`. Interactive
terminals default to `tui`; non-TTY output falls back to `text`. Add
`--verbose` to see captured output for every completed task, not just
failed ones — useful when a `shell` or `powershell` task succeeds but
you still want to see what it printed.

## Narrow A Run

Run only tasks tagged for the museum lobby kiosks:

```bash
preflight apply playbooks/lobby.yml --tags kiosk,display
```

Skip a tag instead:

```bash
preflight apply playbooks/lobby.yml --skip-tags reboot
```

Tag filtering happens after the plan is built, so a skipped task still
appears in `plan` output but is recorded as skipped during `check` or
`apply`. Combine `--tags`/`--skip-tags` with `-e`/`--var` to rehearse a
narrow slice of a playbook — for example, checking only the `kiosk`
tasks with a non-default `content_root` before touching the rest of
the lobby.

Preflight also merges variables from several other layers ahead of
`-e`/`--var` (inventory, groups, hosts, the playbook itself). See
[Variable Merge Order](../reference/inventory.md#variable-merge-order)
for the full precedence chain.

## Troubleshooting

### A `uses:` reference fails to resolve

Action resolution checks these sources in order:

1. Embedded stdlib
2. `./actions` in the project
3. `~/.preflight/actions`
4. Git-backed refs through the resolver chain

If a remote ref is missing locally, fetch it first:

```bash
preflight action fetch github.com/myorg/actions/signage@v2.1
```

### `plan` still shows `{{ facts... }}`

That is expected. `plan` does not contact targets. Final
fact-dependent rendering happens during `check` and `apply`.

## Related Docs

- [Run a playbook against remote hosts](./remote-execution.md)
- [Inspect state and diffs](./inspect-state-and-diff.md)
- [Stage bundles for air-gapped deployment](./air-gapped-deployment.md)
- [Variable Merge Order](../reference/inventory.md#variable-merge-order)
- [Quickstart](../tutorials/quickstart.md)
