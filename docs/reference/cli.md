# CLI Reference

This page describes the current command surface implemented in `cmd/`.

## Global Flags

| Flag | Short | Description |
| --- | --- | --- |
| `--target` | `-t` | Host or group selectors from inventory. Repeat the flag to combine selectors. |
| `--inventory` |  | Inventory file path. Defaults to `./inventory.yml` for inventory-backed commands. |
| `--var` | `-e` | Set a variable as `key=value`. Later values win. |
| `--tags` |  | Run only tasks with these tags. |
| `--skip-tags` |  | Skip tasks with these tags. |
| `--check` |  | Dry-run mode. |
| `--diff` |  | Show diffs for file changes. |
| `--verbose` | `-v` | Verbose output. |
| `--output` |  | Output format: `text`, `json`, or `jsonl`. |
| `--concurrency` |  | Maximum number of hosts to operate on in parallel. `0` means unlimited. |
| `--timeout` |  | Overall run timeout, for example `30m` or `1h`. |
| `--phase` |  | Run only up to `plan`, `fetch`, `stage`, or `apply`. |

## Target Selection

`--target` resolves selectors in order.

- A selector can be a host name, a group name, or `all`.
- Repeating `--target` builds a union of matches.
- Hosts are deduplicated by name, and the first match wins.
- Omitting `--target` keeps local execution behavior.
- Using only `local` or `localhost` keeps local execution behavior and does not require `inventory.yml`.

## Top-Level Commands

### `preflight apply <playbook>`

Apply a playbook.

Examples:

```bash
preflight apply playbooks/lobby.yml
preflight apply playbooks/lobby.yml --target lobby --inventory inventory.yml
preflight apply --bundle dist/bundles/lobby-baseline-localhost-linux-amd64.zip
```

Flags:

| Flag | Description |
| --- | --- |
| `--bundle` | Apply from a staged bundle zip instead of resolving a playbook |
| `--bundle-output-dir` | Output directory when `--phase stage` is used from `apply` |

### `preflight check <playbook>`

Dry-run a playbook without applying changes.

Examples:

```bash
preflight check playbooks/lobby.yml
preflight check playbooks/lobby.yml --target lobby-pc-01 --inventory inventory.yml
```

### `preflight diff <playbook>`

Compare the current plan to the recorded state file.

```bash
preflight diff playbooks/lobby.yml
```

### `preflight plan <playbook>`

Resolve and print the execution plan.

Examples:

```bash
preflight plan playbooks/lobby.yml
preflight plan playbooks/lobby.yml --target lobby --inventory inventory.yml
```

Notes:

- `plan` stays a pure planning phase and does not contact targets.
- When `--target` resolves multiple hosts, output is grouped by host.
- Fact-dependent template expressions stay unresolved in the printed preview until execution time.

### `preflight validate <playbook>`

Parse a playbook and resolve `uses:` references.

```bash
preflight validate playbooks/lobby.yml
```

### `preflight facts [target]`

Gather facts and print JSON.

Examples:

```bash
preflight facts
preflight facts local
preflight facts --target lobby --inventory inventory.yml
preflight facts lobby-pc-01 --inventory inventory.yml
```

Behavior:

- Use either a positional target or `--target`, not both.
- One resolved host prints a single facts object.
- Multiple resolved hosts print an object keyed by host name.

### `preflight stage <playbook>`

Assemble one offline bundle zip per resolved target.

Example:

```bash
preflight stage playbooks/lobby.yml
```

Flags:

| Flag | Description |
| --- | --- |
| `--bundle-output-dir` | Output directory for generated bundle zips |

## `action` Commands

### `preflight action list`

List embedded and local actions.

### `preflight action info <ref>`

Show action metadata, inputs, outputs, and task count.

Examples:

```bash
preflight action info preflight/autologin
preflight action info myorg/display-config
```

### `preflight action fetch <ref>`

Fetch a remote action ref into `~/.preflight/actions`, resolve it to an exact commit SHA, and create or update `./preflight.lock` in the current project directory. Nested remote `uses:` dependencies are fetched recursively.

## `inventory` Commands

### `preflight inventory list`

List hosts from the inventory file.

Flags:

| Flag | Description |
| --- | --- |
| `--inventory` | Path to inventory file. Defaults to `./inventory.yml`. |

## `plugin` Commands

### `preflight plugin list`

List discovered plugins and their initialization status.

### `preflight plugin info <name>`

Print the resolved path, source directory, reported version, and initialization status for one plugin.

Discovery order:

1. Directory alongside the executable
2. `~/.preflight/plugins`
3. `./plugins`

Plugin filenames must start with `preflight-plugin-`. On Windows they must end in `.exe`.

## `secret` Commands

### `preflight secret list`

List configured secrets from `preflight.yml`.

### `preflight secret encrypt <name>`

Encrypt a plaintext file into the repo-backed store.

Flags:

| Flag | Description |
| --- | --- |
| `--from-file` | Plaintext source file |
| `--recipient` | Override `age` recipients |
| `--identity` | Identity file path for future decrypt/edit operations |

### `preflight secret edit <name>`

Decrypt a secret to a temp file, open it in your editor, then re-encrypt it.

Flags:

| Flag | Description |
| --- | --- |
| `--recipient` | Override recipients for re-encryption |
| `--identity` | Override identity file path for decryption |

## `state` Commands

### `preflight state show`

Print the selected state file as JSON.

### `preflight state diff <playbook>`

Compare the current plan to the recorded state file.

Flags:

| Flag | Description |
| --- | --- |
| `--state-file` | Path to the state file. Defaults to `state/provision.json`. |

Notes:

- Local applies write `state/provision.json` by default.
- Inventory-backed applies write per-host state files under `state/targets/<host>.json`.
- To inspect a remote host state file, pass `--state-file state/targets/<host>.json`.
- Diff statuses include `NEW`, `CHANGED`, `UNCHANGED`, `REMOVED`, and `STATUS-ONLY`.

## Output Formats

| Value | Notes |
| --- | --- |
| `text` | Default non-TTY renderer |
| `tui` | Interactive terminal renderer |
| `json` | JSON event stream |
| `jsonl` | Same renderer as `json` |
