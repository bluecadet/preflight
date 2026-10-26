# Targets, transports, and plugins

A target is what the runner executes against: an inventory host resolved to a concrete transport (local, SSH, or WinRM). Every target runs Windows. The target layer lets one runner work locally, remotely, and from staged bundles.

## The `Target` interface

Every target implements the same small interface:

- execute a module, in check or apply mode
- report basic machine information
- report its transport

The runner never needs to know whether a task runs on the same machine, over SSH, or over WinRM.

## Local target

The local target requires a Windows controller. It runs the same Windows PowerShell module implementations as the remote transports, running each script from a temporary `.ps1` file with `powershell.exe -File` on the local machine instead of sending scripts over a connection. As a result:

- each module has one implementation, local or remote
- there is no connection setup, which makes it the fastest path during development
- `apply --bundle` runs staged bundles through it

## Default transport

A host entry without a `transport` field uses `ssh` on port 22 with `host_key_policy: accept-new`. Set `transport: winrm` on hosts that should use WinRM.

## SSH target

SSH is the default remote transport. The remote host must provide Windows PowerShell; connecting to a host without it fails. Over SSH, Preflight supports every built-in module, streams task output as it arrives, and encrypts traffic by default. See [Enable remote access on a Windows target](../how-to/enable-remote-access.md).

### SSH authentication

SSH targets try these methods in order and use the first that yields a usable credential:

1. Explicit private key: `private_key` (inline PEM or a file path) on the host entry. For an encrypted key, set `private_key_passphrase` too. An encrypted key without a passphrase fails with an error instead of falling through to other methods.
2. SSH agent: when `SSH_AUTH_SOCK` is set on the controller, Preflight offers the agent's keys. A dead agent socket is skipped when another method is available; when the agent is the only candidate, its connection error is reported.
3. Default keys: when neither `private_key` nor `password` is set, Preflight tries `~/.ssh/id_ed25519`, `~/.ssh/id_ecdsa`, and `~/.ssh/id_rsa` on the controller, in that order. It skips encrypted or unparsable keys.
4. Password: `password` on the host entry, tried last, matching OpenSSH's preference for public-key auth.

If no method yields a credential, the connection fails immediately with an error naming the host.

```yaml
inventory:
  hosts:
    - name: kiosk-01
      address: 10.0.0.5
      transport: ssh
      private_key: secret:signage-key
      private_key_passphrase: secret:signage-key-passphrase
```

### SSH host-key verification

SSH targets check the remote host key against a known_hosts file according to `host_key_policy`. Each policy either establishes trust explicitly or fails.

- `accept-new` (default): trust on first use. A host with no entry in `known_hosts_file` is trusted, its key is appended, and a notice is logged. Preflight creates a missing file with mode `0600` and its directory with `0700`. A host whose key no longer matches its entry fails immediately, since a changed key can mean a man-in-the-middle attack. If the change is expected, for example after a reimage, delete the stale line and reconnect.
- `strict`: verification only. Unknown hosts and mismatched keys both fail. Establish trust before Preflight connects, either by connecting once with `accept-new` or by seeding the file with `ssh-keyscan -H <host> >> <known_hosts_file>`.
- `insecure`: no host-key verification. Every connection logs a warning. Use it only in throwaway labs or isolated networks where host identity is established another way.

`known_hosts_file` defaults to `known_hosts` in the default SSH key directory, normally `~/.ssh/known_hosts`.

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

`host_key_algorithms` restricts the handshake to the listed algorithms. Without it, the SSH library's default list applies; Preflight does not infer algorithms from `known_hosts`.

### Bastion / jump hosts

A `jump` block routes an SSH host through a single-hop bastion. Preflight connects to the jump host, then tunnels a second SSH handshake to the target over that connection, like OpenSSH's ProxyJump:

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

The jump host has its own authentication and host-key policy. It inherits none of `username`, `password`, `private_key`, `known_hosts_file`, or `host_key_policy` from the target. A jump host cannot have its own `jump` block.

Keepalives travel on the target connection. Because that connection runs through the bastion's TCP connection, they keep both alive.

### Connection timeout

SSH and WinRM hosts accept a `timeout` field, a Go duration such as `30s` or `1m`, that bounds the initial connection and handshake. SSH defaults to 30s; WinRM uses its client library's default of 60s.

```yaml
inventory:
  hosts:
    - name: kiosk-01
      address: 10.0.0.5
      transport: ssh
      timeout: 10s
```

### Keepalive and reconnect

The SSH target sends an OpenSSH keepalive request every 30 seconds, a fixed interval, so NAT and firewall idle timeouts do not drop long runs. If the connection drops mid-run, because two keepalives fail in a row or a command hits a connection error such as `EOF`, Preflight reconnects once and retries the failed command. It never retries command failures (a non-zero exit code or a script error) or a cancelled context.

## WinRM target

WinRM is the alternative for hosts where SSH is not an option. It runs the same PowerShell module implementations, though a few cannot finish their changes over a WinRM session (see below).

The quick WinRM setup (`winrm quickconfig` with HTTP and Basic auth) sends credentials and output unencrypted. Configure an HTTPS listener before using WinRM on an untrusted network.

### The network-logon ceiling

WinRM authenticates with Basic auth, so it works with local accounts only, and runs each operation under a non-interactive network logon. Some Windows operations need privileges or a user profile that a network logon cannot provide, so no PowerShell inside the session can perform them. The [built-in module reference](../reference/modules.md#winrm-session-limitations) lists the affected modules and workarounds.

### Would CredSSP help?

Preflight does not support CredSSP. CredSSP creates an interactive logon on the target instead of a network logon, but it would lift only one of the limitations:

- DISM online servicing (`windows_feature`): yes. The symlink restriction comes from the network-logon session class. An interactive logon can follow the WinSxS component-store symlinks DISM needs.
- `remove_appx_packages` with all-users scope: no. CredSSP gives the connecting user an interactive session, but `-AllUsers` needs AppX state from every profile on the machine, including profiles of users who are not signed in. Run the removal as SYSTEM, for example from a scheduled task, or remove packages per user.
- Incremental output streaming: no. WS-Management returns output through a `Receive` operation that drains a buffer inside the WinRM service, below the authentication layer. No authentication method changes that. Use SSH for incremental output.

## Unknown modules fail at plan time

Preflight never skips a task silently. A module name that is neither a built-in nor a discovered plugin fails during planning, before anything runs. Error messages state the facts without guessing at a fix. The [error reference](../reference/errors.md) lists the reason codes.

## Persistent PowerShell sessions

Starting a fresh `powershell.exe` for every `Check()` and `Apply()` costs 200 to 500 ms, and on WinRM each start also creates and tears down a remote shell, adding about five HTTP round trips. A 20-task playbook could spend 20 seconds on process startup alone.

The SSH and WinRM targets instead keep one PowerShell process alive for the whole run. On the first call, the target starts it:

- WinRM: creates one remote shell and runs `powershell.exe -NoProfile -NonInteractive -Command -` inside it.
- SSH: opens one channel and runs the same command.

`-Command -` makes PowerShell read commands from stdin until the run ends. Each script then follows a marker protocol:

1. The script is base64-encoded and sent to stdin as one line that decodes and runs it as a `ScriptBlock`.
2. When the script finishes, the wrapper writes a unique marker to stdout: `DONE` on success, `ERR:<base64-error>` on a thrown exception.
3. Preflight reads stdout until it sees the marker, then returns the output or the error.

Per-task overhead drops to one write and one read on an open pipe.

Tradeoffs:

- Each script runs in a fresh child scope, so variables from one task are invisible to the next. Tasks stay safe to re-run in isolation.
- A mutex serializes access to the session. Tasks already run serially within a target, so this costs nothing in practice.
- If the session breaks because of a transport failure, Preflight discards it and retries the call in a new `powershell.exe` process. Script errors do not reset the session.

The speedup grows with the number of tasks that reach a remote host. Runs where most tasks are already satisfied gain less, though their `Check()` calls also go through the session.

## Plugin modules

Plugin modules satisfy the same `Check()` then `Apply()` contract as built-ins, so dry runs and staging work unchanged and the target layer needs no second kind of task. A plugin process runs on the controller and reaches the target only through transport-backed handle operations, so one plugin binary runs against local, SSH, and WinRM targets. The [plugin reference](../reference/plugins.md#execution-model) covers discovery, the JSON-RPC protocol, process lifetime, and limitations.

## Safe target metadata

Templates can read `target.name`, `target.hostname`, `target.address`, `target.transport`, and `target.port`. Authentication fields never reach templates or output, because inventory entries may contain secret-backed credentials.

## Host orchestration above targets

The command layer selects inventory hosts and runs them concurrently before the runner sees a target. Each resolved host becomes:

- a concrete transport
- a host-specific variable map
- safe target metadata

The target abstraction stays small while one command fans out across many machines.
