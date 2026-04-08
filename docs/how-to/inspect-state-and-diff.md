# Inspect State And Diffs

Use this guide when you want to answer questions like:

- What did the last successful apply record?
- Which tasks look new or changed since the last run?
- Did a previous failure leave the state file in an important condition?

## Show The Recorded State

For a local run:

```bash
preflight state show
```

For a per-host inventory-backed run:

```bash
preflight state show --state-file state/targets/lobby-pc-01.json
```

The output is the persisted JSON state file, not a summarized table.

## Compare Desired State To Recorded State

Use the state diff command:

```bash
preflight state diff playbooks/lobby.yml
```

It compares the current planned task snapshots to the selected recorded state file.

The default state path is:

- `state/provision.json` for local runs
- `state/targets/<host>.json` for inventory-backed applies

Override the file explicitly when needed:

```bash
preflight state diff playbooks/lobby.yml --state-file ./state/custom.json
```

For inventory-backed diffs, pass the same host selection context you would use for a real run:

```bash
preflight state diff playbooks/lobby.yml --target lobby-pc-01 --inventory inventory.yml --state-file state/targets/lobby-pc-01.json
```

If multiple hosts resolve and you do not set `--state-file`, Preflight compares each host against its own default `state/targets/<host>.json` file and prints one section per host.

## Interpret The Statuses

The comparison output uses these statuses:

- `NEW`: the task exists in the current plan but not in recorded state
- `CHANGED`: the task exists in both places but its structural hash changed
- `UNCHANGED`: the recorded and planned task snapshots match
- `REMOVED`: the task exists in recorded state but not in the current plan
- `STATUS-ONLY`: the task shape matches, but the recorded status still matters operationally, such as a prior failure or skip

## Why This Is Useful

Preflight does not compare only raw task positions. It records stable task keys derived from task lineage, which makes diffs much more meaningful after edits like:

- inserting a task near the top of a playbook
- expanding or refactoring an action
- reordering nearby declarations without actually changing a task’s identity

## Security Notes

State files intentionally avoid persisting decrypted secret values. The recorded parameter summary is redacted for sensitive-looking fields such as passwords, tokens, private keys, and inline `secret:<name>` references.

## Troubleshooting

### Everything looks `NEW`

That usually means you are comparing against the wrong state file, or the playbook structure changed enough that the relevant task lineage no longer matches prior snapshots.

### I want task-by-task execution output, not a plan comparison

Use `preflight check` or `preflight apply` for execution output. `state diff` only compares planned state against recorded state.
