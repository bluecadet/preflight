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

### WinRM Session Limitations

The WinRM transport authenticates with NTLM/Negotiate and runs each operation under a non-interactive network logon. Some Windows operations require privileges or a user profile that this kind of session does not provide, so they cannot be performed over WinRM today regardless of the module used:

- **`windows_feature` (DISM online servicing)** — enabling or disabling an optional feature fails with *"The symbolic link cannot be followed because its type is disabled."* DISM follows symlinks in the component store (WinSxS), and a network-logon token is not permitted to follow them. Reading feature state works; changing it does not.
- **`remove_appx_packages` with all-users scope** — `Remove-AppxPackage -AllUsers` fails with HRESULT `0x80073D19` (*"An error occurred because a user was logged off."*). All-users AppX removal needs an interactive session context.
- **Incremental output streaming** — over WinRM, the WS-Man channel buffers a command's stdout and delivers it in a single batch when the command completes. Output from the `powershell` module is still delivered correctly and in order; it just does not arrive line-by-line as it is produced.

These are properties of the WinRM session, not defects in the modules, so preflight cannot work around them in PowerShell. There is no CredSSP option in the WinRM transport. When you need these operations:

- run them with the **local target** or a **staged bundle** executed on the box, or
- use an **interactive/elevated context** (for example a scheduled task), or
- for live streaming specifically, use **Windows-over-SSH**, where output is delivered incrementally.

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

To enable host-key verification, set `known_hosts_file` on the host entry in `preflight.yml`:

```yaml
inventory:
  hosts:
    - name: kiosk-01
      address: 10.0.0.5
      transport: ssh
      known_hosts_file: /home/operator/.ssh/known_hosts
      host_key_algorithms:
        - ssh-ed25519
```

`host_key_algorithms` is optional. When set, only the listed algorithms are accepted during the handshake. When omitted, the SSH client library's default host-key algorithm list is used; the accepted algorithms are not inferred from the contents of the `known_hosts` file.

## Persistent PowerShell Sessions

Remote Windows tasks are slow if every Check and Apply call starts a fresh `powershell.exe` process. PowerShell startup takes 200–500 ms, and on WinRM each invocation also creates and tears down a WinRM shell — a further five HTTP round-trips. A 20-task playbook against a remote Windows host could spend 20 seconds doing nothing but process startup.

Both the WinRM and SSH Windows runtimes address this by keeping a single PowerShell process alive for the duration of a run.

### How it works

When the first Check or Apply call arrives, the target creates a persistent PowerShell session:

- **WinRM:** calls `CreateShell()` once on the underlying WinRM client, then launches `powershell.exe -NoProfile -NonInteractive -Command -` inside that shell.
- **SSH Windows:** opens a single SSH channel and starts the same PowerShell invocation inside it.

`-Command -` tells PowerShell to read commands from stdin rather than terminate. The session stays open until the run completes.

Each subsequent script is delivered via a marker-based protocol:

1. The script is base64-encoded and sent to PowerShell's stdin as a single line that decodes and executes it as a `ScriptBlock`.
2. The wrapper appends a unique random marker to stdout when the script finishes — `DONE` on success, `ERR:<base64-error>` on a thrown exception.
3. The Go side reads stdout line by line until it sees that marker, then returns the collected output or error.

This reduces per-task overhead to roughly one write and one read on the existing pipe, with no new process or shell creation.

### Tradeoffs

**State does not persist between tasks.** Each script runs in a fresh child scope. Variables set in one task are not visible to the next. This preserves the idempotency contract — a task that can be re-run safely in isolation should not depend on ambient state left by a prior task.

**The session is not shared across concurrent target calls.** A mutex serialises access. Task execution is already serial within a single target, so this is not a practical constraint.

**Graceful degradation.** If the WinRM client or SSH runner in use does not expose raw shell or session creation (which is the case for test fakes), the persistent session is skipped and each script runs via the legacy per-command path. If the persistent session breaks mid-run due to a transport failure, it is discarded and the failing call is retried via the legacy path. Script-level errors (thrown exceptions) do not reset the session — only transport failures do.

### When to expect the speedup

The benefit is proportional to the number of tasks that reach a remote Windows target. A 20-task playbook against a Windows host over WinRM or SSH will run noticeably faster. A playbook that runs mostly against local targets, or one where most tasks are skipped because the host is already in the desired state, will see less of a difference in wall-clock time — though Check calls are still faster because they also go through the persistent session.

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
