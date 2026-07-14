# Inventory Reference

This page describes the `inventory:` block inside `preflight.yml`, parsed by [`internal/inventory/`](/Users/clay/repos/preflight/internal/inventory).

## Purpose

A **host** is one entry in the inventory's `hosts:` list — the docs use the word in exactly that sense. Inventory defines hosts, assigns transports, carries inventory, group, and host variables, and supports selector-based fan-out from CLI commands such as `plan`, `check`, `apply`, and `facts`.

## Top-Level Shape

```yaml
project: museum
environment: production

vars:
  site: main-gallery

inventory:
  vars:
    timezone: America/New_York

  groups:
    lobby:
      vars:
        area: lobby
    windows:
      vars:
        shell: powershell

  hosts:
    - name: lobby-pc-01
      address: 192.168.1.10
      transport: winrm
      platform:
        os: windows
        arch: amd64
      username: exhibit-admin
      password: secret:winrm-password
      groups: [lobby, windows]
      vars:
        display: primary
```

`hosts` is the primary inventory list. `groups` is optional metadata referenced by each host's `groups` list.

## Inventory Fields

| Field | Type | Meaning |
| --- | --- | --- |
| `vars` | object | Variables applied to every host before group and host variables |
| `groups` | object | Optional map of group names to shared metadata |
| `hosts` | host[] | Host entries |

## Group Fields

| Field | Type | Meaning |
| --- | --- | --- |
| `vars` | object | Variables applied to hosts that list this group |

## Host Fields

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string | Required unique host identifier |
| `address` | string | Hostname or IP address. Defaults to `name` when omitted. |
| `transport` | enum | `winrm`, `ssh`, or `local`. Defaults to `ssh` when omitted. |
| `platform` | object | Optional destination OS and architecture used for connection-free bundle staging. See [Platform Fields](#platform-fields). |
| `port` | integer | Explicit port override. Defaults to 22 for `ssh` (including the implicit default transport), 5985/5986 for `winrm`. |
| `username` | string | Username for transport authentication |
| `password` | string | Password or secret reference such as `secret:winrm-password` |
| `private_key` | string | SSH private key value, path, or secret reference |
| `private_key_passphrase` | string | Passphrase for an encrypted SSH `private_key`, or a secret reference |
| `known_hosts_file` | string | Path to a known_hosts file for SSH host-key verification. Defaults to `known_hosts` under the default SSH key directory (normally `~/.ssh/known_hosts`) when omitted. Verification behavior is governed by `host_key_policy` (default `accept-new`). |
| `host_key_policy` | enum | SSH host-key verification policy: `accept-new` (default, trust-on-first-use), `strict` (known_hosts entry required), or `insecure` (verification disabled). See [SSH Host-Key Verification](../explanation/targets-and-transports.md#ssh-host-key-verification). |
| `host_key_algorithms` | string[] | Restrict accepted SSH host-key algorithms, such as `[ssh-ed25519, ssh-rsa]`. |
| `timeout` | duration | Connection/handshake timeout for SSH and WinRM, as a Go duration string such as `30s` or `1m`. Defaults to 30s for SSH when omitted. |
| `https` | bool | Use HTTPS for WinRM |
| `groups` | string[] | Group names applied in order for selector membership and variable precedence |
| `vars` | object | Host-specific variable overrides |
| `jump` | object | Optional single-hop SSH bastion to dial through before reaching this host. See [Jump Host Fields](#jump-host-fields) below. |

### Platform Fields

`platform` declares the host OS and architecture used by `preflight stage`.
Both fields are required when the block is present:

| Field | Type | Meaning |
| --- | --- | --- |
| `os` | enum | `windows`, `linux`, or `darwin` |
| `arch` | enum | `amd64` or `arm64` |

When a host declares `platform`, staging validates modules against that
platform and writes it into the bundle without connecting to the host.
Windows maps to the `windows-powershell` runtime; Linux and Darwin map to
`posix-shell`.

Without `platform`, staging preserves the live-discovery behavior: it probes
the selected host for its OS and architecture. The declaration affects only
staging. Commands such as `facts`, `check`, and `apply` continue to use the
configured transport and the live host. In particular, `transport: local`
still means the controller even when `platform` describes another OS.

### Jump Host Fields

Set on a host's `jump` block to reach it through a bastion (an SSH ProxyJump). Only a single hop is supported; the jump host cannot itself specify a `jump` block. The jump host has its own independent authentication and host-key policy — it does not inherit any of these fields from the host it fronts.

| Field | Type | Meaning |
| --- | --- | --- |
| `address` | string | Required. Hostname or IP address of the jump host. |
| `port` | integer | Jump host SSH port. Defaults to 22. |
| `username` | string | Username for jump host authentication |
| `password` | string | Password or secret reference, such as `secret:bastion-password` |
| `private_key` | string | Jump host SSH private key value, path, or secret reference |
| `private_key_passphrase` | string | Passphrase for an encrypted jump host `private_key`, or a secret reference |
| `known_hosts_file` | string | Path to a known_hosts file for jump host key verification. Defaults to `known_hosts` under the default SSH key directory (normally `~/.ssh/known_hosts`) when omitted. Verification behavior is governed by the jump `host_key_policy` (default `accept-new`). |
| `host_key_policy` | enum | SSH host-key verification policy for the jump host: `accept-new` (default), `strict`, or `insecure`. See [SSH Host-Key Verification](../explanation/targets-and-transports.md#ssh-host-key-verification). |

The `jump` block does not expose a `timeout` field; the bastion hop always uses the default 30s connection timeout.

```yaml
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

Hosts must not reference undefined groups. Preflight fails early when a host lists a missing group so group variables cannot be skipped silently.

For SSH hosts, when `private_key` and `password` are both omitted, Preflight also tries an SSH agent (via `SSH_AUTH_SOCK`) and default keys under `~/.ssh` (`id_ed25519`, `id_ecdsa`, `id_rsa`) on the machine running Preflight. See [SSH Authentication](../explanation/targets-and-transports.md#ssh-authentication) for the full method order.

## Variable Merge Order

This section is the authoritative statement of variable precedence; other
pages link here instead of restating it. Later layers win. When a host is
resolved:

```text
preflight.yml vars
  -> inventory.vars
    -> group vars in each host's group order
      -> host vars
        -> built-in vars.preflight.* metadata
          -> playbook vars
            -> --var CLI flags
```

All variable merges are deep merges, so nested maps keep keys from earlier
layers unless a later layer overwrites the same key.

The built-in `vars.preflight.*` map (see the
[templating reference](./templating.md#built-in-preflight-variables))
carries `project` and `environment` from `preflight.yml`. It sits above
host vars, so playbook vars and CLI `--var` flags can override it but
inventory, group, and host vars cannot.

Action input defaults are separate machinery, not a layer in this chain:
when a task invokes an action with `with:`, the action's declared input
defaults and the task's `with:` values form a scope local to that action's
tasks (a shallow overlay — `with:` beats input defaults).

## Selector Resolution

Selectors passed through `--target` follow these rules:

- A selector may be a host name, a group name, or `all`.
- Repeating `--target` builds a union of matches.
- Hosts are deduplicated by name.
- The first occurrence wins when the same host is selected more than once.

When `preflight.yml` contains inventory and a command runs with no `--target`, Preflight selects all `inventory.hosts`. Use `--target local` to stay on the initiating machine instead.

## Derived Runtime Data

During command execution, each resolved host is turned into:

- a merged variable map
- a safe `target.*` metadata map for templates
- a concrete `Target` implementation
- a derived per-host state path

Inventory-backed applies default to `state/targets/<host>.json`.

## Transport Notes

- A host with no `transport` field connects over `ssh` on port 22, with `host_key_policy: accept-new` pinning the remote key on first connect. Inventories that relied on the old implicit WinRM default must now set `transport: winrm` explicitly.
- `winrm` is the full Windows-native transport and supports all built-in modules.
- For new WinRM hosts, validate the connection with a temporary `preflight.yml` before you commit the entry to your project config. See [Troubleshoot remote connections](../how-to/troubleshoot-remote-connections.md) for a concrete validation flow.
- The current WinRM path is easiest to use with a local Windows account. If a host answers on `5985` but `preflight facts` still returns `401`, check the remote host's WinRM auth settings before changing inventory structure.
- `ssh` auto-detects either a Windows PowerShell runtime or a POSIX shell runtime. Windows-over-SSH supports the built-in Windows module set; POSIX-over-SSH supports `directory`, `file`, `shell`, `wait` (`file_exists`, `port_open`), and `powershell` when installed.
- `local` still participates in inventory selection, but execution happens on the initiating machine.
- For locked-down environments where targets cannot accept controller-initiated access, see [Deploy across restricted networks](../explanation/restricted-network-deployment.md).

## Related Docs

- [Run a playbook against remote hosts](../how-to/remote-execution.md)
- [Troubleshoot remote connections](../how-to/troubleshoot-remote-connections.md)
- [Targets, transports, and plugins](../explanation/targets-and-transports.md)
