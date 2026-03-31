# State Reference

This page describes the persisted runner state file and the statuses reported by `preflight diff` and `preflight state diff`.

## Default Locations

Preflight writes state files to:

- `state/provision.json` for local applies
- `state/targets/<host>.json` for inventory-backed applies

Override the lookup path with `--state-file`.

## File Shape

Current state files use a v2 snapshot model with these top-level fields:

| Field | Type | Purpose |
| --- | --- | --- |
| `version` | integer | State file format version |
| `last_applied` | timestamp | Time of the most recent successful apply write |
| `tasks` | object | Stable task-key to task snapshot map |

Each task snapshot records:

| Field | Type | Purpose |
| --- | --- | --- |
| `task_key` | string | Stable task identity derived from task lineage |
| `task_name` | string | Rendered task name |
| `module` | string | Module name |
| `depends_on` | string[] | Stable keys for dependencies |
| `task_hash` | string | Hash of structural task identity plus params |
| `param_hash` | string | Hash of rendered params |
| `param_summary` | object | Redacted params summary for diff output |
| `status` | string | Recorded execution status |
| `message` | string | Recorded status detail |
| `timestamp` | timestamp | Time the task result was recorded |

## Redaction

State files do not persist decrypted secret values.

`param_summary` is intentionally redacted for secret-bearing fields such as:

- passwords
- tokens
- private keys
- `*_from` secret references

## Legacy Compatibility

Older state files that only contain a `results` map are still readable. Preflight treats them as legacy input and promotes them into the current snapshot model during load and comparison.

## Diff Statuses

`preflight diff` and `preflight state diff` classify tasks with these statuses:

| Status | Meaning |
| --- | --- |
| `NEW` | The task is present in the current plan but not in recorded state |
| `CHANGED` | The task is present in both places but its structural hash changed |
| `UNCHANGED` | The task matches recorded state |
| `REMOVED` | The task exists in recorded state but not in the current plan |
| `STATUS-ONLY` | The task shape matches, but the recorded status indicates a non-successful prior outcome that still matters operationally |

## What Stable Task Keys Solve

Task keys are derived from task lineage rather than from simple positional indexes.

That means:

- inserting a task near the top of a playbook does not make every later task look new
- action expansion can still produce deterministic identities
- diffs become more useful when tasks move or are refactored nearby

## Related Commands

| Command | Purpose |
| --- | --- |
| `preflight state show` | Print a state file as JSON |
| `preflight state diff <playbook>` | Compare a plan to a selected state file |
| `preflight diff <playbook>` | Shortcut for plan-versus-state comparison |

## Related Docs

- [CLI reference](./cli.md)
- [Run a playbook](../how-to/run-a-playbook.md)
