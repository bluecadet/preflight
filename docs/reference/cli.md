# CLI reference

Every command exits `0` on success and `1` on any error, including a failed task. The error is printed to standard error.

`preflight --version` prints the version, commit, and build date.

## Shared flags

Each command lists the flag groups it accepts.

### Targeting

| Flag | Short | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `--target` | `-t` | string list | all inventory hosts, or the local machine without an inventory | Host names, group names, `all`, `local`, or `localhost`; repeat or comma-separate. See [Selector resolution](./inventory.md#selector-resolution) |

### Variables

| Flag | Short | Type | Description |
| --- | --- | --- | --- |
| `--var` | `-e` | `key=value`, repeatable | Set a variable. A value without `=` sets the key to an empty string. Later flags win. See [Variable merge order](./inventory.md#variable-merge-order) |

### Tags

| Flag | Type | Description |
| --- | --- | --- |
| `--tags` | string list | Run only tasks with at least one listed tag |
| `--skip-tags` | string list | Skip tasks with any listed tag |

### Output

| Flag | Short | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `--output` | | `text`, `tui`, `json` | `tui` when stdout is a terminal, else `text` | Output format; any other value selects `text` |
| `--verbose` | `-v` | bool | `false` | Show output of successful tasks and debug logging |
| `--max-fail-lines` | | int | `80` | Lines of output shown for a failed task; `0` is unlimited |
| `--color` | | `auto`, `always`, `never` | `auto` | Color output |
| `--no-color` | | bool | `false` | Disable color; overrides `--color` |

### Run

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `--fail-fast` | bool | `false` | Stop all targets on the first failure |
| `--run-id` | string | generated | Run identifier and run directory name |
| `--keep-runs` | int | `20` | Number of run directories with a `run.json` to keep under `.preflight/runs/`; `0` disables pruning |

### Concurrency and timeout

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `--concurrency` | int | `0` | Maximum targets in parallel; `0` is unlimited; negative values fail |
| `--timeout` | Go duration | none | Overall timeout, such as `30m` or `1h` |

## Run commands

### `preflight apply <playbook>`

Plans and applies a playbook on each selected target. Fetches uncached remote action refs first.

Flags: targeting, variables, tags, output, run, concurrency and timeout, plus:

| Flag | Type | Description |
| --- | --- | --- |
| `--bundle` | path | Apply a staged bundle instead of a playbook; see [Bundle apply](./bundles.md#bundle-apply) |
| `--secret-identity` | path | `age` identity file for encrypted bundle secrets; requires `--bundle` |

```bash
preflight apply playbooks/lobby.yml --target lobby
preflight apply --bundle dist/bundles/<bundle>.zip
```

Errors:

- `apply: playbook path and --bundle cannot be used together`
- `apply: --secret-identity requires --bundle`
- `apply: expected exactly one playbook path`

### `preflight check <playbook>`

Runs the `apply` pipeline as a dry run: each module runs `Check()` only and reports the changes it would make.

Flags: targeting, variables, tags, output, run, concurrency and timeout.

### `preflight stage <playbook>`

Writes one bundle zip per selected target. See [Bundle reference](./bundles.md).

Flags: targeting, variables, tags, output, run, concurrency and timeout, plus:

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `--bundle-output-dir` | path | `<project>/dist/bundles` | Directory for bundle zips |
| `--allow-plaintext-secrets-in-bundle` | bool | `false` | Write resolved secrets into the bundle in plaintext |

A host with [`platform`](./inventory.md#platform-fields) is staged without a connection.

### `preflight plan <playbook>`

Prints the execution plan for each selected target without connecting to it. Unknown `facts`, `target`, and `env` placeholders stay unrendered. Does not fetch remote action refs; an uncached ref fails.

Flags: targeting, variables, tags, output, timeout.

### `preflight validate <playbook>`

Parses the playbook and resolves every `uses` ref, including refs inside actions. Does not connect to targets or fetch remote refs.

Flags: output.

### `preflight facts [target...]`

Gathers [facts](./templating.md#facts) from each selected target. Positional targets and `--target` values are combined. Reads `preflight.yml` and discovers plugins from the working directory.

Flags: targeting, output, concurrency and timeout. `--tags` and `--skip-tags` fail.

```bash
preflight facts
preflight facts local
preflight facts lobby lobby-pc-01
```

In `json` output, each target produces one `facts` event.

## `action`

These commands read `actions/` and `preflight.lock` in the working directory.

| Command | Description |
| --- | --- |
| `preflight action list` | List stdlib actions and project actions under `./actions` |
| `preflight action info <ref>` | Print an action's name, version, description, author, inputs, and task names; remote refs must be cached |
| `preflight action fetch <ref>` | Fetch a remote ref and the remote refs it uses into `~/.preflight/actions`, and record each commit SHA in `preflight.lock` |

Flags: output.

## `inventory`

| Command | Description |
| --- | --- |
| `preflight inventory list` | List each host's name, address, transport, port, and groups from `preflight.yml` in the working directory |

Flags: output. `tui` renders as `text`.

Fails with `inventory list: no inventory configured in preflight.yml` when the file has no `inventory`.

## `plugin`

| Command | Description |
| --- | --- |
| `preflight plugin list` | List each discovered plugin's name, version, status (`ready` or `error`), and path |
| `preflight plugin info <name>` | Print one plugin's name, version, path, source directory, and initialization result |

`plugin list` accepts the output flags; `tui` renders as `text`. `plugin info` prints text only. Both discover `./plugins` from the working directory. See [Discovery](./plugins.md#discovery).

## `secret`

These commands read and write `preflight.yml` in the working directory. See [Manage secrets](../how-to/manage-secrets.md).

### `preflight secret list`

Lists the secrets in `secrets.entries`. Flags: output; `tui` renders as `text`.

### `preflight secret encrypt <name>`

Encrypts a value to `secrets.recipients` and writes it to the entry's `file`. A new entry is added to `preflight.yml` with `file: secrets/<name>.age`; characters other than letters, digits, `.`, `_`, and `-` in the file name become `-`.

| Flag | Type | Description |
| --- | --- | --- |
| `--from-file` | path | Read the value from a file |
| `--from-stdin` | bool | Read the value from standard input; one trailing newline is trimmed |
| `--recipient` | string list | Recipients to use instead of `secrets.recipients`; saved to `preflight.yml` |
| `--identity` | path | Identity path saved to `secrets.identity` |

With neither source flag, the command prompts twice without echo. When standard input is not a terminal, it fails with `no plaintext source: pass --from-file, --from-stdin, or run from a terminal`. `--from-file` and `--from-stdin` are mutually exclusive.

Fails with `secret encrypt: no recipients configured (set secrets.recipients or pass --recipient)` when no recipients are set.

### `preflight secret edit <name>`

Decrypts the secret to a temporary file, opens it in `$EDITOR`, and re-encrypts the result.

| Flag | Type | Description |
| --- | --- | --- |
| `--recipient` | string list | Recipients to use instead of `secrets.recipients`; saved to `preflight.yml` |
| `--identity` | path | Identity path saved to `secrets.identity` |

### `preflight secret rekey [names...]`

Decrypts the named secrets, or every secret when no names are given, and re-encrypts them to the current recipients.

| Flag | Type | Description |
| --- | --- | --- |
| `--recipient` | string list | Recipients to use; saved to `preflight.yml` |
| `--identity` | path | Identity to use; saved to `preflight.yml` |

With names, `--recipient` and `--identity` fail with `secret rekey: --identity and --recipient overrides require rekeying all configured secrets`.

### `preflight secret identity generate --out <path>`

Writes a new `age` X25519 identity file and prints its path and public recipient. Creates parent directories. Fails when the file exists.

### `preflight secret identity recipient <path>`

Prints the public recipient of each identity in the file, one per line.

## Output formats

| Format | Description |
| --- | --- |
| `text` | Plain text |
| `tui` | Terminal UI with a live preview of the last 3 output lines of each running task |
| `json` | One JSON event per line |

`text` and `tui` print a failed task's output by default, and successful tasks' output with `--verbose`.

### JSON events

Every event has `type` and `ts`. Run events:

| Type | Fields |
| --- | --- |
| `task_started`, `task_ok`, `task_changed` | `task_id`, `task`, `target` |
| `task_skipped` | as above, plus `reason` and optional `message`; see [Skip reason codes](./errors.md#skip-reason-codes) |
| `task_failed` | as above, plus `error` and optional `reason`; see [Failure reason codes](./errors.md#failure-reason-codes) |
| `task_output` | `task_id`, `target`, `lines` |
| `target_unreachable` | `target`, `reason`; see [Unreachable targets](./errors.md#unreachable-targets) |
| `target_complete` | `target`, `ok_count`, `changed_count`, `failed_count`, `skipped_count` |

Other event types: `version`, `run_start`, `target_start`, `run_summary`, `activity_start`, `activity_end`, `warning`, `diagnostic`, `facts`, `plan`, `validate`, `action_list`, `action_info`, `action_fetch`, `plugin_list`, `inventory_list`, `secret_list`.

### Run directory

`apply`, `check`, and `stage` write `.preflight/runs/<run-id>/` in the project directory:

| File | Contents |
| --- | --- |
| `run.jsonl` | Event log |
| `run.json` | Run summary |
| `status` | `success` or `failed` |
| `rc` | Exit code |

## Related docs

- [Run a playbook](../how-to/run-a-playbook.md)
- [Run a playbook against remote hosts](../how-to/remote-execution.md)
- [Inventory reference](./inventory.md)
- [Error reference](./errors.md)
