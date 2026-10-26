# Run a playbook

Validate, plan, check, and apply a playbook on one target. To run against many inventory hosts, see [Run a playbook against remote hosts](./remote-execution.md).

## Prerequisites

- `preflight` installed ([Install Preflight](./install-preflight.md))
- A playbook file
- A `preflight.yml` if the playbook uses shared vars, secrets, or a default inventory

## 1. Validate the playbook

```bash
preflight validate playbooks/lobby.yml
```

`validate` parses the playbook and resolves every `uses:` reference. It does not contact targets or gather facts, so a valid playbook can still fail at runtime.

## 2. Inspect the plan

```bash
preflight plan playbooks/lobby.yml
```

`plan` prints the expanded task list in execution order, with each task's name, module, `when:` expression, and tags. It does not gather facts, so `{{ facts.* }}` expressions stay unrendered.

## 3. Dry-run with check

```bash
preflight check playbooks/lobby.yml
```

`check` connects to the target, gathers facts, renders templates, and evaluates `when:` conditions. No module changes anything.

## 4. Apply the playbook

```bash
preflight apply playbooks/lobby.yml
```

Preflight stops a target at its first failed task. Set `ignore_errors: true` on a task to keep going past it. With several targets, `--fail-fast` stops every target on the first failure.

## Override variables

Pass `-e`/`--var` to `plan`, `check`, or `apply`. The flag is repeatable and takes precedence over every other variable source.

```bash
preflight apply playbooks/lobby.yml \
  -e content_root='C:\Exhibits\Content' \
  -e app_env=production
```

See [Variable merge order](../reference/inventory.md#variable-merge-order) for the full precedence chain.

## Run a subset of tasks

Run only tasks with the given tags:

```bash
preflight apply playbooks/lobby.yml --tags kiosk,display
```

Skip tasks with a tag:

```bash
preflight apply playbooks/lobby.yml --skip-tags reboot
```

`plan` lists every task regardless of tags. `check` and `apply` report filtered tasks as skipped.

## Control output

`--output text|tui|json` selects the renderer. Interactive terminals default to `tui`; other output defaults to `text`. `--verbose` shows captured output for every task, not only failed ones.

## Troubleshooting

### A `uses:` reference fails to resolve

Preflight resolves actions in this order:

1. Embedded stdlib (`preflight/...`)
2. `./actions` in the project
3. The cache at `~/.preflight/actions`
4. Git, for remote refs not yet cached

`check`, `apply`, and `stage` fetch missing remote refs before running. `validate` and `plan` read only the cache, so fetch a new ref first:

```bash
preflight action fetch github.com/myorg/actions/signage@v2.1
```

## Related docs

- [Run a playbook against remote hosts](./remote-execution.md)
- [Stage bundles for air-gapped deployment](./air-gapped-deployment.md)
- [Quickstart](../tutorials/quickstart.md)
