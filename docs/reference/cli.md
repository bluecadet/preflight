# CLI Reference

This page describes the current command surface implemented in `cmd/`.

## Global Flags

These flags are registered on the root command and are available everywhere, even when some subcommands do not use them yet.

| Flag | Short | Description |
| --- | --- | --- |
| `--target` | `-t` | Target host(s) or group(s) from inventory |
| `--var` | `-e` | Set a variable as `key=value` |
| `--tags` |  | Only run tasks with these tags |
| `--skip-tags` |  | Skip tasks with these tags |
| `--check` |  | Dry-run mode |
| `--diff` |  | Show diffs for file changes |
| `--verbose` | `-v` | Verbose output |
| `--output` |  | Output format: `text`, `json`, or `jsonl` |
| `--concurrency` |  | Max number of targets to operate on in parallel |
| `--timeout` |  | Overall execution timeout |
| `--phase` |  | Run only up to `plan`, `fetch`, `stage`, or `apply` |

> [!NOTE]
> M1 is local-only. `--target` accepts only `local` or `localhost`, `--concurrency` accepts only `0` or `1`, and `--phase fetch|stage` returns a not-implemented error.

## Top-Level Commands

### `preflight apply <playbook>`

Apply a playbook.

```bash
preflight apply playbooks/lobby.yml
```

### `preflight check <playbook>`

Dry-run a playbook without applying changes.

```bash
preflight check playbooks/lobby.yml
```

### `preflight diff <playbook>`

Compare the current plan to the recorded state file.

```bash
preflight diff playbooks/lobby.yml
```

### `preflight plan <playbook>`

Resolve and print the execution plan.

```bash
preflight plan playbooks/lobby.yml
```

### `preflight validate <playbook>`

Parse a playbook and resolve `uses:` references.

```bash
preflight validate playbooks/lobby.yml
```

### `preflight facts [target]`

Gather facts and print JSON.

```bash
preflight facts
preflight facts local
```

Passing any non-local target returns an error in local-only mode.

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
| `--inventory` | Path to inventory file. Defaults to `./inventory.yml` |

## `plugin` Commands

### `preflight plugin list`

List discovered plugins.

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

Print the last applied state file as JSON.

### `preflight state diff <playbook>`

Compare the current plan to the recorded state file.

Flags:

| Flag | Description |
| --- | --- |
| `--state-file` | Path to the state file. Defaults to `state/provision.json` |

## Output Formats

| Value | Notes |
| --- | --- |
| `text` | Default non-TTY renderer |
| `tui` | Interactive terminal renderer |
| `json` | JSON event stream |
| `jsonl` | Same renderer as `json` |
