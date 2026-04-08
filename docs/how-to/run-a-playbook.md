# Run A Playbook

Use this guide when you already have a playbook and want to validate it, inspect the plan, dry-run it, or apply it with the right flags.

## Prerequisites

- An installed `preflight` binary
- A playbook file
- A `preflight.yml` file if you rely on shared vars or secrets
- An `inventory.yml` file if you want to select remote hosts or groups

If you need an end-to-end onboarding path first, use [Quickstart](../tutorials/quickstart.md).

## Validate Before Execution

Check that the playbook parses and direct `uses:` references resolve:

```bash
preflight validate playbooks/lobby.yml
```

This is the fastest sanity check, but it is intentionally shallow. It does not gather facts, contact targets, or prove that a task will succeed at runtime.

## Inspect The Flattened Plan

Preview the expanded task list:

```bash
preflight plan playbooks/lobby.yml
```

Add variables at planning time:

```bash
preflight plan playbooks/lobby.yml --var content_root=D:\\Exhibits\\Content
```

Why `plan` matters:

- It shows the order of execution after action expansion and playbook imports.
- It lets you verify tag filters and rendered task names.
- It stays pure, so `facts.*` expressions may remain unresolved until `check` or `apply`.

## Dry-Run The Real Execution Path

```bash
preflight check playbooks/lobby.yml
```

This runs the normal runner pipeline in dry-run mode. Tasks still go through dependency ordering and execution-time rendering, but changes are not applied.

## Apply The Playbook

Run the normal apply:

```bash
preflight apply playbooks/lobby.yml
```

By default, Preflight stops on the first task failure. Set `ignore_errors: true`
on a task only when later tasks should continue running after that failure.

Override variables from the CLI when needed:

```bash
preflight apply playbooks/lobby.yml \
  --var content_root=D:\\Exhibits\\Content \
  --var app_env=production
```

Later variable layers win. For a normal inventory-backed run the precedence is:

```text
preflight.yml vars
  -> inventory group vars
    -> inventory host vars
      -> playbook vars
        -> --var flags
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

Tag filtering happens in the runner after the plan has been built, so skipped tasks still appear in the plan but are recorded as skipped during execution.

## Select Inventory Hosts

Pick one host, one group, or several selectors:

```bash
preflight apply playbooks/lobby.yml --target lobby --inventory inventory.yml
preflight check playbooks/lobby.yml --target lobby-pc-01 --inventory inventory.yml
preflight apply playbooks/lobby.yml --target lobby --target gallery --inventory inventory.yml
```

Selector rules:

- A selector may be a host name, a group name, or `all`.
- Repeating `--target` builds a union.
- Hosts are deduplicated by name.
- With no `--target`, Preflight uses a local target.

For a complete inventory example, see [Run a playbook against remote hosts](./remote-execution.md).

## Stage Offline Bundles

Use the dedicated `stage` command when you want offline bundles:

```bash
preflight stage playbooks/lobby.yml
```

Use bundle apply later with:

```bash
preflight apply --bundle dist/bundles/<bundle>.zip
```

If you are staging for an isolated site, follow [Stage bundles for air-gapped deployment](./air-gapped-deployment.md).

## Choose An Output Format

Examples:

```bash
preflight apply playbooks/lobby.yml --output text
preflight apply playbooks/lobby.yml --output tui
preflight apply playbooks/lobby.yml --output json
```

Notes:

- `text` is the plain renderer.
- `tui` is the interactive terminal UI renderer.
- `json` emits newline-delimited JSON events.
- With no explicit flag, interactive terminals auto-select `tui`; non-TTY output falls back to `text`.

When a module supports streamed output, Preflight forwards each line while the task is still running:

- `text` shows captured failure logs below failed tasks by default. With `--verbose`, it prints logs below every completed task that produced output.
- `tui` shows a rolling preview of the last three lines for running tasks and prints captured output blocks on failures. With `--verbose`, it also prints captured output blocks for successful tasks after they complete.
- `json` emits `task_output` events with `task_id`, `task`, `target`, and `lines`. Failed `task_result` events may also include an `output` array with the captured task output.

## Control Host Parallelism

Cap concurrent host execution:

```bash
preflight apply playbooks/lobby.yml --target all --inventory inventory.yml --concurrency 5
```

`0` means unlimited host concurrency.

This setting only affects fan-out across resolved hosts. Inside each host run, task execution still follows the playbook DAG order.

## Inspect Recorded State

Look at the latest recorded state:

```bash
preflight state show
```

Compare the current plan to recorded state:

```bash
preflight state diff playbooks/lobby.yml
```

For per-host inventory-backed state:

```bash
preflight state show --state-file state/targets/lobby-pc-01.json
preflight state diff playbooks/lobby.yml --target lobby-pc-01 --inventory inventory.yml --state-file state/targets/lobby-pc-01.json
```

See [Inspect state and diffs](./inspect-state-and-diff.md) for a task-focused workflow.

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

That is expected. `plan` does not contact targets. Final fact-dependent rendering happens during `check` and `apply`.
