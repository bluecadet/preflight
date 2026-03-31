# Run A Playbook Against Remote Hosts

Use this guide when you already have a playbook and inventory and want to run Preflight against one or more remote machines.

## Prerequisites

- An installed `preflight` binary on the machine initiating the run
- A `playbooks/*.yml` file
- An `inventory.yml` file with `winrm`, `ssh`, or `local` hosts
- A `preflight.yml` file if the inventory uses secret references such as `password_from` or `private_key_from`

If you need the CLI first, follow [Install Preflight](./install-preflight.md).

## 1. Define The Target Hosts

Create or update `inventory.yml`:

```yaml
groups:
  lobby:
    hosts:
      - name: lobby-pc-01
        address: 192.168.1.10
        transport: winrm
        username: exhibit-admin
        password_from: secret:autologin-password

      - name: lobby-pc-02
        address: 192.168.1.11
        transport: winrm
        username: exhibit-admin
        password_from: secret:autologin-password

  gallery:
    hosts:
      - name: gallery-pi-01
        address: 192.168.1.50
        transport: ssh
        username: exhibit
        private_key_from: secret:gallery-key
```

Notes:

- Use `winrm` for Windows hosts.
- Use `ssh` for mixed-environment hosts that should be reached over SSH.
- Use `local` in inventory when you want the inventory workflow but still execute on the current machine.

## 2. Verify Inventory Resolution

List the hosts before running anything:

```bash
preflight inventory list --inventory inventory.yml
```

This helps catch selector mistakes and missing inventory entries early.

## 3. Inspect The Host-Scoped Plan

Run `plan` first:

```bash
preflight plan playbooks/lobby.yml --target lobby --inventory inventory.yml
```

`plan` prints one section per resolved host. It does not contact the targets, so fact-dependent expressions may stay unresolved in the output.

## 4. Dry-Run The Real Execution

Use `check` to gather facts and evaluate conditions without applying changes:

```bash
preflight check playbooks/lobby.yml --target lobby --inventory inventory.yml
```

This is the best way to verify `when:` conditions and host-specific rendering before an apply.

## 5. Apply The Playbook

Run the apply:

```bash
preflight apply playbooks/lobby.yml --target lobby --inventory inventory.yml
```

You can target a single host instead:

```bash
preflight apply playbooks/lobby.yml --target lobby-pc-01 --inventory inventory.yml
```

You can combine selectors:

```bash
preflight apply playbooks/lobby.yml \
  --target lobby \
  --target gallery \
  --inventory inventory.yml
```

Selectors are resolved in order and deduplicated by host name.

## 6. Limit Parallel Host Execution

Use `--concurrency` when you do not want every host to run at once:

```bash
preflight apply playbooks/lobby.yml \
  --target all \
  --inventory inventory.yml \
  --concurrency 5
```

`0` means unlimited host concurrency.

## 7. Inspect Per-Host Facts And State

Gather facts for one host:

```bash
preflight facts lobby-pc-01 --inventory inventory.yml
```

Gather facts for a whole group:

```bash
preflight facts --target lobby --inventory inventory.yml
```

For multiple hosts, the output is a JSON object keyed by host name.

Remote runs also write per-host state files:

```bash
preflight state show --state-file state/targets/lobby-pc-01.json
preflight state diff playbooks/lobby.yml --state-file state/targets/lobby-pc-01.json
```

## Troubleshooting

### WinRM authentication fails

Check the host entry first:

- `address`
- `username`
- `password` or `password_from`
- `https`
- `port`

If you use a secret reference, verify that `preflight.yml` configures the `secret` provider and that the initiating machine can decrypt it.

### SSH connects but a task still fails

The current SSH target is intended for mixed-environment support, but Windows-specific modules still belong on WinRM targets. If a playbook uses Windows-native modules, point those hosts at `transport: winrm`.

### A remote host writes to a different state file than expected

Inventory-backed apply runs write state to `state/targets/<host>.json` by default. The local default `state/provision.json` is still used for local runs.
