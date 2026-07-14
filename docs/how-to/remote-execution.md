# Run A Playbook Against Remote Hosts

Use this guide when you want to select hosts from the `inventory:`
block in `preflight.yml` and fan a playbook out over WinRM or SSH. It
assumes you already know the validate, plan, check, and apply loop; see
[Run a playbook](./run-a-playbook.md) if you do not, and use that guide
directly if you only ever run against the controller or a single
default host.

## Prerequisites

- An installed `preflight` binary on the controller
- A playbook
- A `preflight.yml` file with an `inventory:` block

If the controller cannot open connections to the targets, read
[Deploy across restricted networks](../explanation/restricted-network-deployment.md)
before choosing a transport. If you are validating a new host
before committing it to inventory, use
[Troubleshoot remote connections](./troubleshoot-remote-connections.md).

## 1. Define Inventory Entries

```yaml
inventory:
  groups:
    lobby:
      vars:
        content_root: "C:\\Exhibits\\Lobby"
  hosts:
    - name: lobby-pc-01
      address: 192.168.1.10
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

See the [Inventory Reference](../reference/inventory.md) for every
host, group, and jump-host field, transport defaults, and the
`vars`/group/host precedence chain. On POSIX targets over SSH, prefer
an unprivileged SSH user plus `become` for root-only tasks; see
[Run tasks as another user](./run-tasks-as-another-user.md#posix-become-to-root-from-an-unprivileged-ssh-user).

## 2. Verify Host Resolution

```bash
preflight inventory list
```

This lists every host with its address, transport, port, and groups,
which catches misspelled selectors and inventory shape problems before
you run anything.

## 3. Select Hosts With `--target`

```bash
preflight plan playbooks/lobby.yml --target lobby
preflight apply playbooks/lobby.yml --target lobby-pc-01
preflight apply playbooks/lobby.yml --target lobby --target signage-lab
preflight apply playbooks/lobby.yml --target local
```

A selector is a host name, a group name, or `all`. Repeating
`--target` builds a union, deduplicated by host name. With no
`--target`, an inventory-backed run selects all hosts; use
`--target local` to force a run on the controller instead. See
[Selector Resolution](../reference/inventory.md#selector-resolution)
for the full resolution rules.

## 4. Gather Facts As A First Smoke Test

Before running a playbook against a new host, confirm the transport
and credentials work:

```bash
preflight facts lobby-pc-01
preflight facts --target lobby
preflight facts
```

For more than one resolved target, `facts` prints a JSON object keyed
by host name. A successful result proves both authentication and
remote command execution, which makes it the cheapest way to confirm a
host is reachable before you `plan` or `check` a real playbook against
it.

## 5. Apply To Selected Hosts

```bash
preflight apply playbooks/lobby.yml --target lobby
```

Applies go through the same validate, plan, check, apply loop as a
local run — see [Run a playbook](./run-a-playbook.md) for what each
verb does — except the plan and execution are repeated once per
resolved host.

## 6. Limit Host Concurrency

```bash
preflight apply playbooks/lobby.yml --target all --concurrency 5
```

`--concurrency` caps how many hosts run at once (`0`, the default,
means unlimited). Use it to avoid rebooting or updating an entire
fleet of kiosks at the same moment. It only affects fan-out across
hosts; task order within a single host still follows the playbook's
dependency graph.

## Per-Host State

Inventory-backed applies write a separate state file per host under
`state/targets/<host>.json`, instead of the single
`state/provision.json` a local run uses. See the
[State Reference](../reference/state.md) for the file shape and
[Inspect state and diffs](./inspect-state-and-diff.md) for comparing a
plan against recorded state per host.

## Troubleshooting

This page only covers inventory and fan-out. For connection failures
(WinRM auth, SSH host-key errors, timeouts), see
[Troubleshoot remote connections](./troubleshoot-remote-connections.md).
For the full list of typed error reason codes (including `become`/sudo
failures on POSIX), see the [Error Reference](../reference/errors.md).

A handful of Windows operations (`windows_feature`, all-users
`remove_appx_packages`, streamed `powershell` output) do not work over
a plain WinRM session; see
[WinRM session limitations](../reference/modules.md#winrm-session-limitations)
for why and for the workarounds. POSIX-over-SSH support is
capability-based rather than a distro allowlist; see the
[POSIX capability baseline](../reference/modules.md#posix-capability-baseline-and-tiers)
for what a host needs to provide.

## Related Docs

- [Run a playbook](./run-a-playbook.md)
- [Troubleshoot remote connections](./troubleshoot-remote-connections.md)
- [Inventory Reference](../reference/inventory.md)
- [State Reference](../reference/state.md)
- [Run tasks as another user](./run-tasks-as-another-user.md)
