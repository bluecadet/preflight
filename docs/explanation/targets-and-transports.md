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

## Default Transport

A host entry with no `transport` field resolves to `ssh` on port 22, with `host_key_policy: accept-new` pinning the remote key on first connect. This was previously `winrm`.

**Migration note:** inventories that relied on the implicit WinRM default must now set `transport: winrm` explicitly on every host that should connect that way. Hosts left without a `transport` field will connect over SSH instead.

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

#### Would CredSSP Help?

CredSSP authentication (which preflight does not currently support) creates an interactive logon session on the target rather than a network logon. The following analysis explains whether adding CredSSP to the transport would lift each limitation:

- **DISM online servicing — YES, CredSSP would fix this.** The symlink restriction is a direct consequence of the network-logon session class (LogonType 3). An interactive logon (LogonType 2, which CredSSP provides) has broader reparse-point traversal rights and can follow the WinSxS component-store symlinks that DISM needs to access. This is well-documented in Microsoft enterprise deployment guidance.

- **`remove_appx_packages` with all-users scope — NO, CredSSP does not fix this.** CredSSP gives the *connecting user* an interactive session, but `-AllUsers` needs to access AppX package state in every user profile on the machine. Profiles for users who are not logged in cannot be reliably loaded, regardless of how the connecting user authenticated. The proper solution is running as the SYSTEM account (for example via a scheduled task) or using per-user package operations.

- **Incremental output streaming — NO, WS-Man buffering is auth-independent.** The WS-Management protocol delivers command output through a `Receive` operation that returns whatever the WinRM service has accumulated in its internal buffer. This buffer is filled by a pipe-reading loop deep in the WinRM service implementation — well below the authentication layer. The authentication mechanism (NTLM, Kerberos, or CredSSP) cannot change this. The CRT stdout buffering mode differs slightly between interactive and network logons (line-buffered vs fully buffered), but the WinRM service still accumulates output server-side and delivers it on the next `Receive` call. **Real-time output streaming is not achievable over WS-Man regardless of authentication method.** Windows-over-SSH is the correct path for incremental output.

## SSH Target

SSH now auto-detects one of two runtimes on the remote host:

- `windows-powershell` for Windows hosts that expose a usable PowerShell runtime over SSH
- `posix-shell` for POSIX-style hosts

That split matters:

- Windows-over-SSH uses the same built-in Windows module surface as WinRM.
- POSIX-over-SSH stays conservative and focuses on `directory`, `file`, `shell`, `wait` (`file_exists`, `port_open`), and `powershell` when `pwsh` or `powershell` is installed.
- Plugin modules are not yet supported over SSH.

SSH is now the default and primary remote transport, including for Windows hosts; WinRM remains available and fully supported for hosts where SSH isn't an option.

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

### SSH Authentication

SSH targets try the following authentication methods in order, using the first that yields a usable credential:

1. **Explicit private key** — `private_key` (inline PEM or a file path) on the host entry. If the key is encrypted, set `private_key_passphrase` alongside it; without a passphrase, an encrypted `private_key` fails with a clear error rather than silently trying other methods.
2. **SSH agent** — when the `SSH_AUTH_SOCK` environment variable is set on the machine running Preflight, its keys are offered to the remote host. A dead or unreachable agent socket is skipped silently as long as another auth method (private key, default key, or password) is available; if the agent is the only candidate, its connection error is surfaced.
3. **Default keys** — when neither `private_key` nor `password` is set, Preflight looks for `~/.ssh/id_ed25519`, `~/.ssh/id_ecdsa`, and `~/.ssh/id_rsa` (in that order) on the machine running Preflight and offers whichever exist and parse as unencrypted keys. Encrypted or unparsable default keys are skipped rather than treated as errors.
4. **Password** — `password` on the host entry, tried last (matching OpenSSH's preference for public-key auth).

If none of these produce a usable auth method, connecting fails immediately with an error naming the host, instead of attempting a connection with no credentials.

```yaml
inventory:
  hosts:
    - name: kiosk-01
      address: 10.0.0.5
      transport: ssh
      private_key: secret:signage-key
      private_key_passphrase: secret:signage-key-passphrase
```

### SSH Host-Key Verification

SSH targets verify the remote host key against a known_hosts file according to `host_key_policy`. There is no way to silently trust an unknown host by omission; every policy either establishes trust explicitly or fails loudly.

- **`accept-new`** (default) — trust-on-first-use (TOFU). Verification runs against `known_hosts_file`; a host with no existing entry is trusted and its key is appended to the file (the file and its parent directory are created with `0600`/`0700` permissions if missing), and a notice is logged. A host with an existing entry whose key no longer matches fails immediately with an error, since that can indicate a MITM attack — if the change is expected (e.g. the remote machine was reimaged), remove the stale line from the known_hosts file and reconnect.
- **`strict`** — verification only; both unknown hosts and mismatched keys fail. Use this for hardened setups where trust must be established out of band before Preflight ever connects: either connect once with `accept-new` to pin the key, or pre-seed the file with `ssh-keyscan -H <host> >> <known_hosts_file>`.
- **`insecure`** — disables host-key verification entirely (the pre-hardening default). Every connection logs a prominent warning. Only appropriate for throwaway labs or fully isolated networks where host identity is established by other means.

When `known_hosts_file` is omitted, it defaults to `known_hosts` under the same directory used for default SSH key discovery (normally `~/.ssh/known_hosts`).

```yaml
inventory:
  hosts:
    - name: kiosk-01
      address: 10.0.0.5
      transport: ssh
      host_key_policy: strict
      known_hosts_file: /home/operator/.ssh/known_hosts
      host_key_algorithms:
        - ssh-ed25519
```

`host_key_algorithms` is optional. When set, only the listed algorithms are accepted during the handshake. When omitted, the SSH client library's default host-key algorithm list is used; the accepted algorithms are not inferred from the contents of the `known_hosts` file.

### Bastion / Jump Hosts

An SSH host can be reached through a single-hop bastion by adding a `jump` block. Preflight dials the jump host first, then tunnels a second SSH handshake to the real target over that connection (an SSH ProxyJump):

```yaml
inventory:
  hosts:
    - name: kiosk-01
      address: 10.0.0.5
      transport: ssh
      username: exhibit
      jump:
        address: bastion.example.org
        username: operator
        private_key: secret:bastion-key
```

The jump host has its own independent authentication and host-key policy — it does not inherit `username`, `password`, `private_key`, `known_hosts_file`, or `host_key_policy` from the host it fronts. Only a single hop is supported; a jump host cannot itself specify a `jump` block.

The keepalive request described below rides the target connection only, not the bastion directly. Since the target connection's traffic flows through the bastion's TCP connection, keeping the target alive also keeps the bastion connection alive.

### Connection Timeout

Both SSH and WinRM targets support a `timeout` host field, a Go duration string such as `30s` or `1m`, that bounds how long the initial connection/handshake may take. SSH defaults to 30s when `timeout` is omitted; WinRM defaults to 60s (the underlying client library's default).

```yaml
inventory:
  hosts:
    - name: kiosk-01
      address: 10.0.0.5
      transport: ssh
      timeout: 10s
```

### Keepalive And Reconnect

Once connected, the SSH target sends an OpenSSH keepalive request every 30s (fixed, not configurable) to keep the connection alive across NAT/firewall idle timeouts during long-running playbooks. If a connection drops mid-run — the keepalive fails twice in a row, or any command hits a connection-level error such as a closed socket or `EOF` — Preflight transparently reconnects once and retries the failed command before giving up. Command-level failures (a non-zero exit code, a script error) and a cancelled/expired context are never retried.

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

**Graceful degradation.** If the WinRM client or SSH runner in use does not expose raw shell or session creation (which is the case for test fakes), the persistent session is skipped and each script runs via the per-invocation path. If the persistent session breaks mid-run due to a transport failure, it is discarded and the failing call is retried via the per-invocation path. Script-level errors (thrown exceptions) do not reset the session — only transport failures do.

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
