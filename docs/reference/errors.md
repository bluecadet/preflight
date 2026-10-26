# Error Reference

This page lists the typed reason codes Preflight attaches to task failures
and run refusals. Every transport surfaces the same typed error classes, so
wording and reason codes stay uniform across local, SSH, and WinRM. The
JSON run-log carries a `reason` field on task-failed events; the same codes appear in text output.

## Where Errors Surface

A module that is not supported on a target's runtime is never silently
skipped. Gaps surface at plan time, and nothing executes if the plan
cannot complete:

1. **Plan-time (offline).** Unknown module names — whether a catalog
   built-in or a discovered plugin — fail for all targets.
   Runtime-support violations also fail where the transport implies a
   runtime offline: WinRM is always `windows-powershell`, the local target
   is Windows-only, and staging uses an inventory host's
   declared platform when present. SSH's runtime is only known after probing
   the remote host, so plan-time can only name-check SSH tasks when no staging
   platform is declared. Plan violations exit non-zero like any plan error.
## Reason Codes

| Reason code | Meaning |
| --- | --- |
| `unknown_module` | the module name is neither a catalog built-in nor a discovered plugin |
| `unsupported_on_runtime` | a catalog built-in that is not supported on the target's runtime; the message names the supporting runtimes |
| `plugin_protocol` | a plugin failed the protocol handshake (version mismatch; plugins built against an older protocol are rejected) |

## Task Skip Reasons

A skipped task carries its own closed `reason` field, separate from the
failure reason codes above:

| Reason code | Meaning |
| --- | --- |
| `tag-filtered` | `--tags`/`--skip-tags` excluded the task |
| `when-condition-false` | the task's `when` expression evaluated false |
| `dependency-failed` | a task this one depends on failed and was not `ignore_errors` |
| `already-satisfied` | the module itself reported the task already in the desired state |

`reason` is always one of the codes above — never free text — so a
consumer can switch on it. A skip event may also carry an optional
`detail` field with the module's own human-readable explanation; `detail`
is never a substitute for `reason` and must not be pattern-matched on.

## Unreachable Targets

A `target_unreachable` event is emitted when a target cannot be reached at
all during apply start — an SSH dial/handshake or reconnect failure, or a
WinRM client/session creation failure or transport-level RPC failure — as
opposed to a failure of some command or script that ran successfully over
a connection that was already working. It carries a free-text `reason`
(the underlying connection error) and is always followed by a
`diagnostic` event with the same detail, then the normal `target_complete`
event with `outcome: "failed"`.

## Message Shapes

Wording is complete facts, not a suggestion engine: every message names the
module, the target's runtime, and (for `unsupported_on_runtime`) the
supporting runtimes. There is no did-you-mean and no remediation prose.
Example shapes:

```text
task "install tools": module "my_module" is not supported on windows-powershell (supported: <runtimes>)
```

The support matrix source of truth is `internal/target/catalog.go`'s
capability flags, and a drift test asserts the
[built-in module reference](./modules.md) matches the catalog — so the
docs cannot silently drift from the code.

## Schema Validation Errors

Playbooks, actions, inventory files, and `preflight.yml` are validated
against embedded JSON Schemas before anything runs. A violation names the
JSON Pointer path to the offending field and the concrete reason — never
the raw schema-compiler tree used to find it. When a document has more than
one simultaneous violation, all of them are reported, not just the first.
Example shapes:

```text
schema validation failed: at '/tasks/0/service/state': value must be one of 'running', 'stopped', 'disabled' (got 'sideways')
schema validation failed: 2 schema violations:
  at '/tasks/0/service/state': value must be one of 'running', 'stopped', 'disabled' (got 'sideways')
  at '/tasks/1/reboot/timeout': got string, want integer
```

Every field typed as a string, number, boolean, enum, or object also accepts
a bare `"{{ ... }}"` template expression, so it can be filled in at run
time — this is a blanket contract of every schema in this package, not a
per-field detail, and it is not restated in each error message. A value that
is neither the expected type nor a template expression is reported against
the expected type; the fact that a template would also have been accepted is
not repeated as a phantom alternative.

Malformed YAML (a syntax error, or a non-string mapping key) is a distinct
failure from a well-formed document that violates the schema — the message
prefix (`schema validation parse error` vs. `schema validation failed`) is
part of the contract callers rely on to tell the two apart.

## Related Docs

- [Built-in module reference](./modules.md) — per-module supported
  runtimes
- [Configure another user's settings](../how-to/configure-another-users-settings.md)
- [Targets, transports, and plugins](../explanation/targets-and-transports.md)
- [Plugin reference](./plugins.md)
