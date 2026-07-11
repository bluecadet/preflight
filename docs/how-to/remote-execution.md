# Run A Playbook Against Remote Hosts

Use this guide when you want to select hosts from the `inventory:` block in `preflight.yml` and run Preflight over WinRM or SSH.

## Prerequisites

- An installed `preflight` binary on the machine initiating the run
- A playbook
- A `preflight.yml` file with an `inventory:` block

If you want the local flow first, use [Run a playbook](./run-a-playbook.md).

If the machine running Preflight cannot open controller-initiated connections to the targets, read [Deploy across restricted networks](../explanation/restricted-network-deployment.md) before choosing a transport.

If you are on macOS and want to validate WinRM before you commit a host to your real inventory, use [Validate a WinRM connection from macOS](./validate-winrm-from-macos.md).

## 1. Define Inventory Entries

Example `preflight.yml`:

```yaml
inventory:
  groups:
    lobby:
      vars:
        content_root: "C:\\Exhibits\\Lobby"
    signage-lab: {}
  hosts:
    - name: lobby-pc-01
      address: 192.168.1.10
      transport: winrm
      username: exhibit-admin
      password: secret:winrm-password
      groups: [lobby]

    - name: lobby-pc-02
      address: 192.168.1.11
      transport: winrm
      username: exhibit-admin
      password: secret:winrm-password
      groups: [lobby]

    - name: signage-host-01
      address: 192.168.1.50
      transport: ssh
      username: exhibit
      private_key: secret:signage-key
      groups: [signage-lab]
```

Transport guidance:

- Use `winrm` for Windows-native configuration work. `transport` must be set explicitly to `winrm`; a host with no `transport` field connects over `ssh` instead.
- Use `ssh` when the target is best reached over SSH and the tasks only require SSH-supported modules. `ssh` is also the default when `transport` is omitted. On POSIX targets, the recommended posture is an **unprivileged SSH user plus `become`** for any task needing root (see [Run tasks as another user](./run-tasks-as-another-user.md#posix-become-to-root-from-an-unprivileged-ssh-user)); root SSH login is a stated working alternative.
- Use `local` if you want inventory-driven selection but execution should still happen on the initiating machine.
- For a brand-new WinRM target, validate the endpoint and credentials with a temporary `preflight.yml` plus `preflight facts` before you wire the host into your project inventory.

## 2. Verify Host Resolution

List the hosts before running a playbook:

```bash
preflight inventory list
```

This catches misspelled selectors and inventory shape problems early.

## 3. Preview The Host-Scoped Plan

Inspect the plan for a group:

```bash
preflight plan playbooks/lobby.yml
preflight plan playbooks/lobby.yml --target lobby
```

Omitting `--target` resolves the full inventory. For multiple resolved hosts, `plan` prints one section per host. It still stays pure, so target facts are not gathered yet.

## 4. Dry-Run Real Execution

Use `check` before you apply:

```bash
preflight check playbooks/lobby.yml
preflight check playbooks/lobby.yml --target lobby
```

This is the safest place to verify:

- `when:` conditions
- execution-time template rendering
- transport credentials
- host selection and concurrency behavior

## 5. Apply To Selected Hosts

Run every host in the inventory:

```bash
preflight apply playbooks/lobby.yml
```

Run one group:

```bash
preflight apply playbooks/lobby.yml --target lobby
```

Run one host:

```bash
preflight apply playbooks/lobby.yml --target lobby-pc-01
```

Combine selectors:

```bash
preflight apply playbooks/lobby.yml \
  --target lobby \
  --target signage-lab
```

Selectors are resolved in order, merged into a union, then deduplicated by host name.

## 6. Limit Host Parallelism

Control how many hosts execute at once:

```bash
preflight apply playbooks/lobby.yml \
  --target all \
  --concurrency 5
```

`0` means unlimited host concurrency.

This is useful when you want to avoid rebooting or updating an entire fleet at the same moment.

## 7. Gather Facts Explicitly

Facts for one host:

```bash
preflight facts lobby-pc-01
```

Facts for the full inventory:

```bash
preflight facts
```

Facts for a group:

```bash
preflight facts --target lobby
```

For several hosts, the command prints a JSON object keyed by host name.

## 8. Inspect Per-Host State

Inventory-backed applies write a separate state file per host:

```bash
preflight state show --state-file state/targets/lobby-pc-01.json
preflight state diff playbooks/lobby.yml --target lobby-pc-01 --state-file state/targets/lobby-pc-01.json
```

That split is deliberate. It keeps state comparisons meaningful even when one playbook is applied to many machines with different facts or variable layers.

## Troubleshooting

### WinRM authentication fails

Check the host entry first:

- `address`
- `port`
- `username`
- `password`
- `https`

If the password is a secret reference, make sure the initiating machine can decrypt it through the project’s `age` identity.

If you are validating from a Mac, work through [Validate a WinRM connection from macOS](./validate-winrm-from-macos.md). The short version is:

- `nc` only proves that something is listening on the port
- `curl http://<host>:5985/wsman` returning `405` with `Allow: POST` is a good sign that the endpoint is really WinRM
- `preflight facts <host> --output json` is the first command that proves authentication plus remote PowerShell execution
- the current Preflight WinRM path is easiest to validate with a dedicated local Windows account

### SSH connects but a task still fails

That usually means the playbook is hitting a runtime-specific limit. SSH now auto-detects either a Windows PowerShell runtime or a POSIX shell runtime:

- Windows-over-SSH supports the built-in Windows module set.
- POSIX-over-SSH supports `directory`, `file`, `shell`, `wait` (`file_exists`, `port_open`, `service_running`), `reboot`, `powershell` when `pwsh` or `powershell` is installed, `user` (requires root), `system_package` on targets with apt or dnf, and `service` over systemd (requires root). The full per-module matrix lives in the [built-in module reference](../reference/modules.md).
- Plugin modules run over SSH the same way they run locally and over WinRM — the plugin process runs controller-side.
- Using the `file` module with `ensure: absent` on a path that resolves to a directory returns an error. Use the `directory` module with `ensure: absent` instead.

POSIX-over-SSH support is **capability-based, not a distro allowlist**: a host is supported when it provides the baseline (strict POSIX `sh`, core utilities plus `base64`, systemd for service management, `apt`/`dnf` for `system_package`, `sudo` only when `become` is used). See [Targets, transports, and plugins](../explanation/targets-and-transports.md#posix-capability-baseline-and-tiers) for the full baseline and the official Linux / best-effort macOS-BSD tiers.

On POSIX, a task that needs root fails **before `Check()`** with a typed reason code rather than a generic sudo error:

- `requires-root-violation` — a `requires_root` module (`service`, `user`, `system_package`, `reboot`) ran as a non-root effective user. Fix it by running as root or setting `become: {enabled: true}`.
- `sudo-missing` — `become` is enabled but the target has no `sudo` binary.
- `sudo-password-required` — a no-password `sudo -n` run needed a password. Supply `become.password` (secret-backed) or configure `NOPASSWD`.
- `sudo-auth-failed` — the supplied `become.password` was rejected.

See [How `become` works](../explanation/become.md) for the full POSIX privilege model.

If the target is Windows but does not expose a usable PowerShell runtime over SSH, use WinRM or a staged bundle instead.

### A WinRM task fails with a symbolic-link or 0x80073D19 error

A few Windows operations cannot complete over a basic WinRM session because it runs under a non-interactive network logon:

- `windows_feature` enabling/disabling fails with *"The symbolic link cannot be followed because its type is disabled"* (DISM cannot follow component-store symlinks).
- `remove_appx_packages` with all-users scope fails with HRESULT `0x80073D19` (*"a user was logged off"*).
- `powershell` output is delivered all at once at completion rather than streamed line-by-line.

These are WinRM session limitations, not module bugs, and there is no CredSSP option in the transport. An interactive logon (for example CredSSP) would fix the DISM restriction but would **not** fix the AppX all-users restriction (which needs SYSTEM-level access to other user profiles) or the streaming limitation (WS-Man buffers output regardless of auth). Run these operations with the local target, a staged bundle executed on the box, or an interactive context (for example a scheduled task); live streaming works over Windows-over-SSH. See [WinRM Session Limitations](../explanation/targets-and-transports.md#winrm-session-limitations) for details.

### I expected one shared state file

Inventory-backed applies write `state/targets/<host>.json` so each host has its own recorded task snapshot. Local runs still default to `state/provision.json`.
