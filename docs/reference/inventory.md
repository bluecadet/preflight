# Inventory Reference

This page describes `inventory.yml`, parsed by [`internal/inventory/`](/Users/clay/repos/preflight/internal/inventory).

## Purpose

Inventory groups hosts, assigns transports, carries host and group variables, and supports selector-based fan-out from CLI commands such as `plan`, `check`, `apply`, and `facts`.

## Top-Level Shape

```yaml
groups:
  all:
    vars:
      timezone: "America/New_York"

  lobby:
    vars:
      resolution: "3840x2160"
    hosts:
      - name: lobby-pc-01
        address: 192.168.1.10
        transport: winrm
        username: exhibit-admin
        password: secret:winrm-password
```

## Group Fields

| Field | Type | Meaning |
| --- | --- | --- |
| `vars` | object | Variables applied to every host in the group |
| `hosts` | host[] | Host entries in the group |

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
| `known_hosts_file` | string | Path to a known_hosts file for SSH host-key verification. When omitted, host-key checking is skipped (insecure; acceptable only on isolated networks). |
| `host_key_algorithms` | string[] | Restrict accepted SSH host-key algorithms (e.g. `[ssh-ed25519, ssh-rsa]`). When omitted, the SSH library's built-in default host-key algorithm list is used. This setting still applies even when `known_hosts_file` is omitted, in which case host-key verification is skipped but the negotiated host-key algorithm is still constrained if you set this field explicitly. |
| `https` | bool | Use HTTPS for WinRM |
| `vars` | object | Host-specific variable overrides |

## Variable Merge Order

When a host is resolved:

```text
all group vars
  -> selected group vars
    -> host vars
```

That merged host var map then feeds into the broader runtime precedence stack together with project vars, playbook vars, and CLI `--var` flags.

## Selector Resolution

Selectors passed through `--target` follow these rules:

- A selector may be a host name, a group name, or `all`.
- Repeating `--target` builds a union of matches.
- Hosts are deduplicated by name.
- The first occurrence wins when the same host is selected more than once.

When a command runs with inventory available and no `--target`, it resolves `all`. Use `--target local` to stay on the initiating machine instead.

## Derived Runtime Data

During command execution, each resolved host is turned into:

- a merged variable map
- a safe `target.*` metadata map for templates
- a concrete `Target` implementation
- a derived per-host state path

Inventory-backed applies default to `state/targets/<host>.json`.

## Transport Notes

- `winrm` is the full Windows-native transport and supports all built-in modules.
- For new WinRM hosts, validate the connection with a scratch inventory and `preflight facts` before you commit the entry to your real inventory. See [Validate a WinRM connection from macOS](../how-to/validate-winrm-from-macos.md) for a concrete validation flow from a Mac controller.
- The current WinRM path is easiest to use with a local Windows account. If an endpoint answers on `5985` but `preflight facts` still returns `401`, check the remote host's WinRM auth settings before changing inventory structure.
- `ssh` auto-detects either a Windows PowerShell runtime or a POSIX shell runtime. Windows-over-SSH supports the built-in Windows module set; POSIX-over-SSH supports `directory`, `file`, `shell`, `wait` (`file_exists`, `port_open`), and `powershell` when installed.
- `local` still participates in inventory selection, but execution happens on the initiating machine.
- For locked-down environments where targets cannot accept controller-initiated access, see [Deploy across restricted networks](../explanation/restricted-network-deployment.md).

## Related Docs

- [Run a playbook against remote hosts](../how-to/remote-execution.md)
- [Validate a WinRM connection from macOS](../how-to/validate-winrm-from-macos.md)
- [Targets, transports, and plugins](../explanation/targets-and-transports.md)
