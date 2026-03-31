# Run A Playbook

Use this guide when you already have a playbook and want to validate it, inspect the plan, or execute it with the right flags.

## Prerequisites

- An installed `preflight` binary
- A `preflight.yml` file if you rely on project vars or secrets
- A playbook file
- An `inventory.yml` file if you want to target remote hosts or groups

If you need the CLI first, follow [Install Preflight](./install-preflight.md).
If you want an end-to-end remote example, follow [Run a playbook against remote hosts](./remote-execution.md).


## Validate Before Running

Use `validate` to catch parse and action-resolution errors:

```bash
preflight validate playbooks/lobby.yml
```

Use `plan` to inspect the flattened task list:

```bash
preflight plan playbooks/lobby.yml
preflight plan playbooks/lobby.yml --target lobby --inventory inventory.yml
```

## Dry-Run A Playbook

Use either command:

```bash
preflight check playbooks/lobby.yml
preflight apply playbooks/lobby.yml --check
```

Both paths run in dry-run mode.

> [!TIP]
> `preflight diff <playbook>` compares the current plan to the recorded `state/provision.json` file. Use `check` when you want a dry-run execution pass.

## Apply A Playbook

Run:

```bash
preflight apply playbooks/lobby.yml
```

Add variable overrides with `--var`:

```bash
preflight apply playbooks/lobby.yml \
  --var content_root=D:\\Exhibits\\Content \
  --var app_env=production
```

Variable precedence during planning is:

```text
project vars -> inventory group vars -> inventory host vars -> playbook vars -> --var flags
```

## Choose Hosts From Inventory

Use `--target` to select one host, one group, or several selectors:

```bash
preflight apply playbooks/lobby.yml --target lobby --inventory inventory.yml
preflight check playbooks/lobby.yml --target lobby-pc-01 --inventory inventory.yml
preflight apply playbooks/lobby.yml --target lobby --target gallery --inventory inventory.yml
```

Selectors are resolved in order, then merged into a deduplicated host set.

## Filter By Tags

Run only selected tasks:

```bash
preflight apply playbooks/lobby.yml --tags kiosk,display
```

Skip selected tasks:

```bash
preflight apply playbooks/lobby.yml --skip-tags reboot
```

## Stop At A Pipeline Phase

Use `--phase` when you want to run only part of the pipeline:

```bash
preflight apply playbooks/lobby.yml --phase plan
preflight apply playbooks/lobby.yml --phase fetch
preflight apply playbooks/lobby.yml --phase stage
preflight stage playbooks/lobby.yml
```

Current behavior:

| Phase | Status |
| --- | --- |
| `plan` | Implemented |
| `fetch` | Implemented for remote action download and lockfile updates |
| `stage` | Writes one offline bundle zip per resolved target |
| `apply` | Implemented |

Use `preflight apply --bundle <bundle.zip>` to execute a staged bundle without re-resolving the playbook.

## Choose An Output Format

Available values:

| Flag | Behavior |
| --- | --- |
| `--output text` | Text renderer |
| `--output json` | Newline-delimited JSON renderer |
| `--output jsonl` | Same renderer as `json` |
| `--output tui` | Terminal UI renderer |

Example:

```bash
preflight apply playbooks/lobby.yml --output jsonl
```

## Control Host Parallelism

Use `--concurrency` to cap how many hosts execute at once:

```bash
preflight apply playbooks/lobby.yml --target all --inventory inventory.yml --concurrency 5
```

`0` means unlimited host concurrency.

## Inspect The Recorded State

After an apply, inspect the stored state:

```bash
preflight state show
preflight state diff playbooks/lobby.yml
```

`state diff` compares the current plan to the recorded `state/provision.json` file.
The output distinguishes `NEW`, `CHANGED`, `UNCHANGED`, `REMOVED`, and `STATUS-ONLY` tasks.

For inventory-backed runs, apply writes per-host state files under `state/targets/<host>.json`. Inspect one directly with:

```bash
preflight state show --state-file state/targets/lobby-pc-01.json
preflight state diff playbooks/lobby.yml --state-file state/targets/lobby-pc-01.json
```

## Troubleshooting

### A `uses:` reference fails to resolve

Preflight resolves actions in this order:

1. Embedded stdlib
2. `./actions` relative to the project root
3. `~/.preflight/actions`
4. Git resolver

Remote refs are resolved offline from the cache and `preflight.lock`. If a remote ref is missing, run `preflight action fetch <ref>` or `preflight apply <playbook>` to populate the cache first.

### A host is missing from the run

Check the selector and inventory path first:

```bash
preflight inventory list --inventory inventory.yml
```

Remember that repeated `--target` flags build a union and deduplicate by host name.

### `plan` output still shows `{{ facts... }}`

That is expected. `plan` stays a pure phase and does not contact hosts. Final fact-dependent rendering happens during `check` and `apply`.
