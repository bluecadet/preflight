# Run A Playbook

Use this guide when you already have a playbook and want to validate it, inspect the plan, or execute it with the right flags.

## Prerequisites

- An installed `preflight` binary
- A `preflight.yml` file if you rely on project vars or secrets
- A playbook file

If you need the CLI first, follow [Install Preflight](./install-preflight.md).


## Validate Before Running

Use `validate` to catch parse and action-resolution errors:

```bash
preflight validate playbooks/lobby.yml
```

Use `plan` to inspect the flattened task list:

```bash
preflight plan playbooks/lobby.yml
```

## Dry-Run A Playbook

Use either command:

```bash
preflight check playbooks/lobby.yml
preflight apply playbooks/lobby.yml --check
```

Both paths run in dry-run mode.

> [!TIP]
> `preflight diff <playbook>` currently routes through the same dry-run execution path. It is useful as a read-only check command, but there is not yet a separate diff engine for file content changes.

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
project vars -> playbook vars -> --var flags
```

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
```

Current behavior:

| Phase | Status |
| --- | --- |
| `plan` | Implemented |
| `fetch` | Stubbed validation step |
| `stage` | Stubbed validation step |
| `apply` | Implemented |

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

## Inspect The Recorded State

After an apply, inspect the stored state:

```bash
preflight state show
preflight state diff playbooks/lobby.yml
```

`state diff` compares the current plan to the recorded `state/provision.json` file.

## Troubleshooting

### A `uses:` reference fails to resolve

Preflight resolves actions in this order:

1. Embedded stdlib
2. `./actions` relative to the playbook directory
3. `~/.preflight/actions`
4. Git resolver

The Git resolver exists in the chain, but remote fetch is not implemented yet.

### `--target` does not change where the playbook runs

The global `--target` flag exists on the CLI, but current playbook execution commands construct a local target. Use the flag cautiously until inventory-backed target selection is wired into `apply`, `check`, and `plan`.
