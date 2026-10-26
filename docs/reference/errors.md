# Error reference

## Plan-time checks

Before any task runs, Preflight checks every task in the plan. A failure stops the run and exits non-zero.

| Check | Message |
| --- | --- |
| Module name is a built-in or a discovered plugin | `task "<name>": module "<module>" is not a known module` |
| A local target runs on a Windows controller | `local target is only supported on Windows (controller OS is <os>); use an ssh or winrm target` |

## Failure reason codes

A failed task caused by a module lookup or plugin handshake carries a `reason` code in the `task_failed` event. Other failures carry no `reason`; the message is in `error`.

| Code | Cause | Message |
| --- | --- | --- |
| `unknown_module` | The name is neither a built-in nor a discovered plugin | `module "<module>" is not a known module` |
| `unsupported_on_runtime` | A discovered plugin is not registered on the target's runtime | `module "<module>" is not supported on windows-powershell` |
| `plugin_protocol` | The plugin reported a different or missing protocol version | `plugin module "<module>": <detail>` |

Messages state the failure only; they carry no suggestions.

## Skip reason codes

A `task_skipped` event carries one of these `reason` codes. The value is never free text.

| Code | Cause |
| --- | --- |
| `tag-filtered` | `--tags` or `--skip-tags` excluded the task |
| `when-condition-false` | The `when` condition rendered false |
| `dependency-failed` | A task in `depends_on` failed without `ignore_errors` |
| `already-satisfied` | The module returned a skipped status |

A task whose `Check()` finds no change needed reports `task_ok`, not a skip.

A skip event may also carry the module's explanation, as `message` in `--output json` and as `detail` in the run log. Match on `reason`, not on this text.

## Unreachable targets

`target_unreachable` is emitted when the first connection to a target fails: an SSH dial or handshake failure, or a WinRM client or session failure. A command that fails over a working connection is a task failure instead.

The event's `reason` is the connection error text. It is followed by a `diagnostic` event with the same text and a `target_complete` event. In the run log, that `target_complete` event has `outcome: "failed"`.

## Schema validation errors

Playbooks, actions, inventory, and `preflight.yml` are validated against embedded JSON Schemas before anything runs. Each violation names the JSON Pointer path and the reason. All violations in a document are reported.

```text
schema validation failed: at '/tasks/0/service/state': value must be one of 'running', 'stopped', 'disabled' (got 'sideways')
schema validation failed: 2 schema violations:
  at '/tasks/0/service/state': value must be one of 'running', 'stopped', 'disabled' (got 'sideways')
  at '/tasks/1/reboot/timeout': got string, want integer
```

Malformed YAML fails with the prefix `schema validation parse error`; a well-formed document that breaks the schema fails with `schema validation failed`.

Any field typed as a number, boolean, enum, array, or object also accepts a string that is one whole `{{ ... }}` placeholder. Errors report only the expected type.

## Related docs

- [Built-in module reference](./modules.md)
- [Plugin reference](./plugins.md)
- [Targets, transports, and plugins](../explanation/targets-and-transports.md)
