# CLI Reference

This page describes the command surface implemented under [`cmd/`](/Users/clay/repos/preflight/cmd).

## Global Flags

These flags are defined on the root command and are available to subcommands where they make sense.

| Flag | Short | Meaning |
| --- | --- | --- |
| `--target` | `-t` | Host or group selector from inventory. Repeat to build a union. |
| `--inventory` |  | Inventory file path. Defaults to `./inventory.yml` when inventory is needed. |
| `--var key=value` | `-e` | Set a variable override. Later values win. |
| `--tags` |  | Run only tasks that have any of the listed tags. |
| `--skip-tags` |  | Skip tasks that have any of the listed tags. |
| `--check` |  | Dry-run mode. |
| `--diff` |  | Present on the CLI surface but not currently wired into task execution output. |
| `--verbose` | `-v` | Reserved verbose flag. |
| `--output` |  | Output format: `text`, `tui`, `json`, or `jsonl`. |
| `--concurrency` |  | Maximum number of hosts to operate on in parallel. `0` means unlimited. |
| `--timeout` |  | Overall run timeout such as `30m` or `1h`. |
| `--phase` |  | Run only up to `plan`, `fetch`, `stage`, or `apply`. |

## Target Selection Rules

When a command supports inventory-backed execution:

- A selector may be a host name, a group name, or `all`.
- Repeating `--target` builds a union of matches.
- Hosts are deduplicated by name.
- Omitting `--target` resolves a local target.
- Using only `local` or `localhost` also resolves a local target.

## Top-Level Commands

### `preflight apply <playbook>`

Apply a playbook to one local or inventory-backed target set.

Examples:

```bash
preflight apply playbooks/lobby.yml
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
| `--bundle-output-dir` | Output directory when running the stage phase through `apply` |

### `preflight check <playbook>`

Run the same execution pipeline as `apply`, but stop after `Check()` paths and report what would change.

```bash
preflight check playbooks/lobby.yml
preflight check playbooks/lobby.yml --target lobby --inventory inventory.yml
```

### `preflight diff <playbook>`

Compare the current plan to the selected recorded state file.

```bash
preflight diff playbooks/lobby.yml
```

### `preflight plan <playbook>`

Resolve and print the target-specific execution plan without running tasks.

```bash
preflight plan playbooks/lobby.yml
preflight plan playbooks/lobby.yml --target lobby --inventory inventory.yml
```

Behavior notes:

- `plan` does not contact targets.
- Action expansion and playbook imports are reflected in the printed output.
- `facts.*` expressions may remain unresolved in preview output.

### `preflight validate <playbook>`

Parse a playbook and resolve direct `uses:` references without executing anything.

```bash
preflight validate playbooks/lobby.yml
```

### `preflight facts [target]`

Gather facts and print JSON.

```bash
preflight facts
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

List available embedded and project-local actions.

### `preflight action info <ref>`

Show action metadata, inputs, outputs, and task count.

```bash
preflight action info preflight/autologin
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

Encrypt a plaintext file into the repo-backed secret store.

Command-specific flags:

| Flag | Meaning |
| --- | --- |
| `--from-file` | Plaintext source file |
| `--recipient` | Override configured recipients |
| `--identity` | Override the identity path stored for decrypt/edit flows |

### `preflight secret edit <name>`

Decrypt a configured secret to a temporary file, open it in `$EDITOR`, and re-encrypt it.

Command-specific flags:

| Flag | Meaning |
| --- | --- |
| `--recipient` | Override recipients for re-encryption |
| `--identity` | Override the identity used for decryption |

## `state` Commands

### `preflight state show`

Print the selected state file as JSON.

### `preflight state diff <playbook>`

Compare the current plan to the selected state file.

Command-specific flags:

| Flag | Meaning |
| --- | --- |
| `--state-file` | Override the state file path |

Behavior notes:

- Local applies default to `state/provision.json`.
- Inventory-backed applies write `state/targets/<host>.json`.
- `preflight diff` is a shortcut into the same comparison machinery.

## Output Formats

| Value | Behavior |
| --- | --- |
| `text` | Plain human-readable renderer |
| `tui` | Interactive terminal UI renderer |
| `json` | Newline-delimited JSON events |
| `jsonl` | Same renderer and event shape as `json` |
