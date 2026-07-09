# Inventory Reference

This page describes the `inventory:` block inside `preflight.yml`, parsed by [`internal/inventory/`](/Users/clay/repos/preflight/internal/inventory).

## Purpose

Inventory defines target hosts, assigns transports, carries inventory, group, and host variables, and supports selector-based fan-out from CLI commands such as `plan`, `check`, `apply`, and `facts`.

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
| `transport` | enum | `winrm`, `ssh`, or `local` |
| `port` | integer | Explicit port override |
| `username` | string | Username for transport authentication |
| `password` | string | Password or secret reference such as `secret:winrm-password` |
| `private_key` | string | SSH private key value, path, or secret reference |
| `private_key_passphrase` | string | Passphrase for an encrypted SSH `private_key`, or a secret reference |
| `known_hosts_file` | string | Path to a known_hosts file for SSH host-key verification. Defaults to `known_hosts` under the default SSH key directory (normally `~/.ssh/known_hosts`) when omitted. |
| `host_key_policy` | enum | SSH host-key verification policy: `accept-new` (default, trust-on-first-use), `strict` (known_hosts entry required), or `insecure` (verification disabled). See [SSH Host-Key Verification](../explanation/targets-and-transports.md#ssh-host-key-verification). |
| `host_key_algorithms` | string[] | Restrict accepted SSH host-key algorithms, such as `[ssh-ed25519, ssh-rsa]`. |
| `timeout` | duration | Connection/handshake timeout for SSH and WinRM, as a Go duration string such as `30s` or `1m`. Defaults to 30s for SSH when omitted. |
| `https` | bool | Use HTTPS for WinRM |
| `groups` | string[] | Group names applied in order for selector membership and variable precedence |
| `vars` | object | Host-specific variable overrides |

Hosts must not reference undefined groups. Preflight fails early when a host lists a missing group so group variables cannot be skipped silently.

For SSH hosts, when `private_key` and `password` are both omitted, Preflight also tries an SSH agent (via `SSH_AUTH_SOCK`) and default keys under `~/.ssh` (`id_ed25519`, `id_ecdsa`, `id_rsa`) on the machine running Preflight. See [SSH Authentication](../explanation/targets-and-transports.md#ssh-authentication) for the full method order.

## Variable Merge Order

When a host is resolved:

```text
preflight.yml vars
  -> inventory.vars
    -> group vars in each host's group order
      -> host vars
        -> playbook vars
          -> --var CLI flags
```

All variable merges are deep merges, so nested maps keep keys from earlier layers unless a later layer overwrites the same key.

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

- `winrm` is the full Windows-native transport and supports all built-in modules.
- For new WinRM hosts, validate the connection with a temporary `preflight.yml` before you commit the entry to your project config. See [Validate a WinRM connection from macOS](../how-to/validate-winrm-from-macos.md) for a concrete validation flow from a Mac controller.
- The current WinRM path is easiest to use with a local Windows account. If an endpoint answers on `5985` but `preflight facts` still returns `401`, check the remote host's WinRM auth settings before changing inventory structure.
- `ssh` auto-detects either a Windows PowerShell runtime or a POSIX shell runtime. Windows-over-SSH supports the built-in Windows module set; POSIX-over-SSH supports `directory`, `file`, `shell`, `wait` (`file_exists`, `port_open`), and `powershell` when installed.
- `local` still participates in inventory selection, but execution happens on the initiating machine.
- For locked-down environments where targets cannot accept controller-initiated access, see [Deploy across restricted networks](../explanation/restricted-network-deployment.md).

## Related Docs

- [Run a playbook against remote hosts](../how-to/remote-execution.md)
- [Validate a WinRM connection from macOS](../how-to/validate-winrm-from-macos.md)
- [Targets, transports, and plugins](../explanation/targets-and-transports.md)
