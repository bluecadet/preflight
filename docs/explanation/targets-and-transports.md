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

SSH now auto-detects one of two runtimes on the remote host:

- `windows-powershell` for Windows hosts that expose a usable PowerShell runtime over SSH
- `posix-shell` for POSIX-style hosts

That split matters:

- Windows-over-SSH uses the same built-in Windows module surface as WinRM.
- POSIX-over-SSH stays conservative and focuses on `directory`, `file`, `shell`, `wait` (`file_exists`, `port_open`), and `powershell` when `pwsh` or `powershell` is installed.
- Plugin modules are not yet supported over SSH.

WinRM is still the clearest Windows-first remote transport when it is available, but SSH is no longer limited to simple file and shell tasks on Windows hosts.

### SSH Module Support Matrix

| Module | POSIX shell | Windows PowerShell SSH |
|---|---|---|
| `directory` | yes | yes |
| `file` | yes | yes |
| `shell` | yes | yes |
| `wait` (`file_exists`, `port_open`) | yes | yes |
| `powershell` | yes (requires `pwsh` or `powershell` installed) | yes |
| `environment` | no | yes |
| `registry` | no | yes |
| `service` | no | yes |
| `reboot` | no | yes |
| `user` | no | yes |
| `windows_feature` | no | yes |
| plugin modules | no | no |

Unsupported module usage is caught at first task execution and returns a clear error. There is no silent fallback.

### SSH Host-Key Verification

By default, when no `known_hosts_file` is configured in inventory, host-key checking is skipped. This is insecure and should only be used on isolated networks where host identity is established by other means.

To enable host-key verification, set `known_hosts_file` on the host entry in inventory:

```yaml
hosts:
  - name: kiosk-01
    address: 10.0.0.5
    transport: ssh
    known_hosts_file: /home/operator/.ssh/known_hosts
    host_key_algorithms:
      - ssh-ed25519
```

`host_key_algorithms` is optional. When set, only the listed algorithms are accepted during the handshake. When omitted, all algorithms supported by the known_hosts file are accepted.

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
