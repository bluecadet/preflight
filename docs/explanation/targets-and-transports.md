# Targets, Transports, And Plugins

Preflight is not just "local shell commands with YAML." The target layer is the abstraction that lets the same runner work locally, remotely, and through staged bundles.

## The `Target` Interface Is The Fulcrum

Every target implementation provides the same core operations:

- execute a module
- copy a file
- read a file
- report reachability
- report basic machine information

That keeps the runner from caring whether the task runs:

- on the same machine
- over WinRM
- over SSH
- through a future agent-based transport

## Local Target

The local target executes modules in process using the module registry.

Why it matters:

- the fastest path for development
- the reference execution model for built-in modules
- the execution mode used when applying a staged bundle

## WinRM Target

WinRM is the main remote Windows transport. It is where the Windows-first design shows up most clearly.

The WinRM target supports the full built-in module set by translating operations into PowerShell or Windows command invocations on the remote host.

This is the right transport for:

- registry edits
- service control
- user management
- Windows feature management
- PowerShell-heavy configuration

## SSH Target

SSH is intentionally narrower today. It currently implements:

- `directory`
- `file`
- `shell`

That makes it useful for mixed environments and simple remote file or command tasks, but it is not the full Windows configuration surface. If a playbook leans on Windows-native modules, WinRM is the correct target.

## Why Plugin Modules Fit Cleanly

Plugin modules are adapted into the same module contract the targets already use. That is a strong architectural signal: plugins are not a sidecar feature. They are part of the execution model.

Because plugins satisfy the same `Check()` then `Apply()` shape:

- dry-run still works
- staging still works
- state tracking still works
- the target layer does not need a second concept of "custom task"

## Why Safe Target Metadata Exists

Templates can read `target.*`, but only from a sanitized metadata map. This exposes useful context like host name, address, transport, and port without leaking authentication details into templating or output.

That boundary matters because inventory entries may contain secret-backed credentials.

## Why Host Orchestration Lives Above Targets

Inventory selection and host concurrency are handled before the runner receives a target. Each resolved host becomes:

- a concrete transport
- a host-specific variable map
- a host-specific state path
- a safe target metadata map

This keeps the target abstraction small and reusable while still letting one CLI invocation fan out across many machines.
