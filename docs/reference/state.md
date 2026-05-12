# State Reference

This page describes the persisted runner state written by [`internal/runner/state.go`](/Users/clay/repos/preflight/internal/runner/state.go).

## Default Locations

Preflight writes state files to:

- `state/provision.json` for local applies
- `state/targets/<host>.json` for inventory-backed applies

Inspection commands such as `preflight state show`, `preflight state diff`, and `preflight apply --bundle` can read or write a different state file with `--state-file`.

## File Shape

Current state files use the v2 snapshot model:

| Field | Type | Meaning |
| --- | --- | --- |
| `version` | integer | State file format version |
| `last_applied` | timestamp | Time of the most recent successful apply write |
| `tasks` | object | Stable task-key to task snapshot map |

Legacy `results`-only files are still readable and are promoted in memory to the current snapshot form.

## Task Snapshot Fields

Each task snapshot records:

| Field | Type | Meaning |
| --- | --- | --- |
| `task_key` | string | Stable task identity derived from task lineage |
| `task_name` | string | Rendered task name |
| `module` | string | Module name |
| `depends_on` | string[] | Stable keys for dependencies |
| `task_hash` | string | Hash of task identity plus params |
| `param_hash` | string | Hash of rendered params plus task execution options such as `become` |
| `param_summary` | object | Redacted summary of rendered params and execution options used for comparisons |
| `status` | string | Recorded execution status |
| `message` | string | Recorded task message |
| `timestamp` | timestamp | Task result timestamp |

## Redaction

State files do not persist decrypted secret values.

`param_summary` is redacted for secret-bearing fields such as:

- passwords
- tokens
- private keys
- become passwords
- any `secret:<name>` reference values

## Diff Statuses

`preflight state diff` uses these statuses:

| Status | Meaning |
| --- | --- |
| `NEW` | The task is present in the current plan but not in recorded state |
| `CHANGED` | The task exists in both places but its structural hash changed |
| `UNCHANGED` | The task matches recorded state |
| `REMOVED` | The task exists in recorded state but not in the current plan |
| `STATUS-ONLY` | The task shape matches, but the recorded status still matters operationally |

## Host Context Matters

State comparison uses the selected host context to render task names and params before hashing them. For inventory-backed diffs, pass the relevant `--target` value so `vars.*`, `target.*`, `facts.*`, and `env.*` expressions are evaluated for the correct machine.

## Why Stable Task Keys Matter

Task keys are based on lineage, not just list position. That improves comparisons when:

- a task is inserted earlier in a playbook
- an action expands into several child tasks
- nearby refactors happen without actually changing a task’s identity

## Related Commands

| Command | Purpose |
| --- | --- |
| `preflight state show` | Show a state file through the selected output renderer |
| `preflight state diff <playbook>` | Compare a plan to a selected state file |

## Related Docs

- [Inspect state and diffs](../how-to/inspect-state-and-diff.md)
- [Execution model](../explanation/execution-model.md)
