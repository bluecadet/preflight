# CLI Reference

This page describes the command surface implemented under [`cmd/`](/Users/clay/repos/preflight/cmd).

## Common Flags

These flags are defined on the individual commands that use them.

| Flag | Short | Meaning |
| --- | --- | --- |
| `--target` | `-t` | Host or group selector from inventory. Repeat to build a union. When inventory is available and this flag is omitted, commands target all inventory hosts. |
| `--inventory` |  | Inventory file path. Defaults to `./inventory.yml` when present. |
| `--var key=value` | `-e` | Set a variable override. Later values win. |
| `--tags` |  | Run only tasks that have any of the listed tags. |
| `--skip-tags` |  | Skip tasks that have any of the listed tags. |
| `--verbose` | `-v` | Include successful task output blocks in human-readable renderers. |
| `--output` |  | Output format: `text`, `tui`, or `json`. |
| `--concurrency` |  | Maximum number of hosts to operate on in parallel. `0` means unlimited. |
| `--timeout` |  | Overall run timeout such as `30m` or `1h`. |

## Target Selection Rules

When a command supports inventory-backed execution:

- A selector may be a host name, a group name, or `all`.
- Repeating `--target` builds a union of matches.
- Hosts are deduplicated by name.
- Omitting `--target` resolves all hosts from the selected inventory when `--inventory` is set or `./inventory.yml` is discovered.
- Without an inventory file, omitting `--target` resolves a local target.
- Using only `local` or `localhost` forces a local target even when inventory is available.

## Top-Level Commands

### `preflight apply <playbook>`

Apply a playbook to one local or inventory-backed target set.

Examples:

```bash
preflight apply playbooks/lobby.yml
preflight apply playbooks/lobby.yml --inventory inventory.yml
preflight apply playbooks/lobby.yml --target lobby --inventory inventory.yml
```

Apply a staged bundle instead of resolving a playbook:

```bash
preflight apply --bundle dist/bundles/lobby-localhost-windows-amd64.zip
```

Command-specific flags:

| Flag | Meaning |
| --- | --- |
| `--bundle` | Apply a staged bundle zip instead of a playbook |

### `preflight check <playbook>`

Run the same execution pipeline as `apply`, but stop after `Check()` paths and report what would change.

```bash
preflight check playbooks/lobby.yml
preflight check playbooks/lobby.yml --inventory inventory.yml
preflight check playbooks/lobby.yml --target lobby --inventory inventory.yml
```

### `preflight plan <playbook>`

Resolve and print the target-specific execution plan without running tasks.

```bash
preflight plan playbooks/lobby.yml
preflight plan playbooks/lobby.yml --inventory inventory.yml
preflight plan playbooks/lobby.yml --target lobby --inventory inventory.yml
```

Behavior notes:

- `plan` does not contact targets.
- Action expansion and playbook imports are reflected in the printed output.
- `facts.*` expressions may remain unresolved in preview output.
- Remote Git-backed actions must already exist in the local cache and lockfile. Use `preflight action fetch`, `check`, or `apply` to populate uncached refs.

### `preflight validate <playbook>`

Parse a playbook and resolve direct and nested `uses:` references without executing anything. In interactive terminals, `--output tui` renders a validation summary with resolved refs and status cards.

```bash
preflight validate playbooks/lobby.yml
```

### `preflight facts [target]`

Gather facts for one or more targets through the selected output renderer.

```bash
preflight facts
preflight facts --inventory inventory.yml
preflight facts local
preflight facts --target lobby --inventory inventory.yml
preflight facts lobby-pc-01 --inventory inventory.yml
```

Behavior:

- Use either a positional target or `--target`, not both.
- One resolved host prints one facts object.
- Multiple hosts print an object keyed by host name.

### `preflight stage <playbook>`

Assemble one staged bundle zip per resolved target.

```bash
preflight stage playbooks/lobby.yml
```

Command-specific flags:

| Flag | Meaning |
| --- | --- |
| `--bundle-output-dir` | Directory for generated bundle zips |

## `action` Commands

### `preflight action list`

List available embedded and project-local actions. In interactive terminals, `--output tui` renders grouped action catalog cards.

### `preflight action info <ref>`

Show action metadata, inputs, and task count.

```bash
preflight action info preflight/autologin
preflight action info preflight/windows-machine
preflight action info myorg/display-config
```

### `preflight action fetch <ref>`

Fetch a remote action ref into the user cache, recursively fetch nested remote `uses:` dependencies, and create or update `preflight.lock` in the current project.

```bash
preflight action fetch github.com/myorg/actions/signage@v2.1
```

## `inventory` Commands

### `preflight inventory list`

List all hosts from the selected inventory file.

```bash
preflight inventory list --inventory inventory.yml
```

## `plugin` Commands

### `preflight plugin list`

List discovered plugins and their initialization status.

### `preflight plugin info <name>`

Show one plugin’s path, source directory, version, and initialization result.

## `secret` Commands

### `preflight secret list`

List configured repo-backed secrets from `preflight.yml`.

### `preflight secret encrypt <name>`

Encrypt a plaintext value into the repo-backed secret store. The plaintext can
come from a file, from standard input, or from an interactive no-echo prompt
(confirmed twice). If no source flag is set and stdin is not a terminal, the
command exits with an error rather than consuming piped input.

Command-specific flags:

| Flag | Meaning |
| --- | --- |
| `--from-file` | Read plaintext from a file |
| `--from-stdin` | Read plaintext from standard input (single trailing newline trimmed) |
| `--recipient` | Override configured recipients |
| `--identity` | Override the identity path stored for decrypt/edit flows |

`--from-file` and `--from-stdin` are mutually exclusive.

### `preflight secret edit <name>`

Decrypt a configured secret to a temporary file, open it in `$EDITOR`, and re-encrypt it.

Command-specific flags:

| Flag | Meaning |
| --- | --- |
| `--recipient` | Override recipients for re-encryption |
| `--identity` | Override the identity used for decryption |

### `preflight secret identity generate --out <path>`

Generate an age X25519 identity file. The command creates parent directories, refuses to overwrite an existing file, writes the identity with restrictive file permissions, and prints the identity path plus public recipient.

Command-specific flags:

| Flag | Meaning |
| --- | --- |
| `--out` | Path to write the generated identity file |

### `preflight secret identity recipient <path>`

Read an existing age identity file and print its public recipient strings. Identity files with multiple identities produce one recipient per line in file order.

### `preflight secret rekey [names...]`

Decrypt configured secrets and re-encrypt them to the current configured recipients. When names are provided, only those secrets are rekeyed.

Command-specific flags:

| Flag | Meaning |
| --- | --- |
| `--recipient` | Override recipients for a full rekey and save them to `preflight.yml` |
| `--identity` | Override the identity for a full rekey and save it to `preflight.yml` |

Overrides are rejected when specific secret names are supplied, because `secrets.recipients` and `secrets.identity` are project-wide defaults.

## `state` Commands

### `preflight state show`

Show the selected state file through the chosen output renderer.

### `preflight state diff <playbook>`

Compare the current plan to the selected state file.

```bash
preflight state diff playbooks/lobby.yml
preflight state diff playbooks/lobby.yml --target lobby-pc-01 --inventory inventory.yml
```

Command-specific flags:

| Flag | Meaning |
| --- | --- |
| `--state-file` | Override the state file path |

Behavior notes:

- Local applies default to `state/provision.json`.
- Inventory-backed applies write `state/targets/<host>.json`.
- Inventory-backed diffs should pass the same `--target` and `--inventory` context used for the host being compared.
- When multiple hosts resolve and `--state-file` is not set, the command prints one section per host and reads each host's default state file.

## Output Formats

| Value | Behavior |
| --- | --- |
| `text` | Plain human-readable renderer |
| `tui` | Interactive terminal UI renderer with richer layouts for run, plan, facts, state, validate, and action inspection commands |
| `json` | Newline-delimited JSON events |

When a task streams output during `apply`, the `json` renderer emits `task_output` events keyed by `task_id` and `target`, with the streamed lines in `lines`. Failed `task_result` events may also include an `output` array containing the captured task output block.

For human-readable output, the `text` renderer shows failure logs by default and prints logs below every completed task when `--verbose` is enabled. The `tui` renderer always shows a rolling preview of the last three streamed lines for each active task, prints captured failure logs by default, includes successful-task logs after completion when `--verbose` is enabled, and uses Lip Gloss cards, tables, and progress bars for plan and inspection commands.
