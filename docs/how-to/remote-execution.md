# Run A Playbook Against Remote Hosts

Use this guide when you want to select hosts from `inventory.yml` and run Preflight over WinRM or SSH.

## Prerequisites

- An installed `preflight` binary on the machine initiating the run
- A playbook
- An inventory file
- A `preflight.yml` file if the inventory uses secret references such as `password_from` or `private_key_from`

If you want the local flow first, use [Run a playbook](./run-a-playbook.md).

## 1. Define Inventory Entries

Example `inventory.yml`:

```yaml
groups:
  lobby:
    vars:
      content_root: "C:\\Exhibits\\Lobby"
    hosts:
      - name: lobby-pc-01
        address: 192.168.1.10
        transport: winrm
        username: exhibit-admin
        password_from: secret:winrm-password

      - name: lobby-pc-02
        address: 192.168.1.11
        transport: winrm
        username: exhibit-admin
        password_from: secret:winrm-password

  signage-lab:
    hosts:
      - name: signage-host-01
        address: 192.168.1.50
        transport: ssh
        username: exhibit
        private_key_from: secret:signage-key
```

Transport guidance:

- Use `winrm` for Windows-native configuration work.
- Use `ssh` when the target is best reached over SSH and the tasks only require SSH-supported modules.
- Use `local` if you want inventory-driven selection but execution should still happen on the initiating machine.

## 2. Verify Host Resolution

List the hosts before running a playbook:

```bash
preflight inventory list --inventory inventory.yml
```

This catches misspelled selectors and inventory shape problems early.

## 3. Preview The Host-Scoped Plan

Inspect the plan for a group:

```bash
preflight plan playbooks/lobby.yml --target lobby --inventory inventory.yml
```

For multiple resolved hosts, `plan` prints one section per host. It still stays pure, so target facts are not gathered yet.

## 4. Dry-Run Real Execution

Use `check` before you apply:

```bash
preflight check playbooks/lobby.yml --target lobby --inventory inventory.yml
```

This is the safest place to verify:

- `when:` conditions
- execution-time template rendering
- transport credentials
- host selection and concurrency behavior

## 5. Apply To Selected Hosts

Run one group:

```bash
preflight apply playbooks/lobby.yml --target lobby --inventory inventory.yml
```

Run one host:

```bash
preflight apply playbooks/lobby.yml --target lobby-pc-01 --inventory inventory.yml
```

Combine selectors:

```bash
preflight apply playbooks/lobby.yml \
  --target lobby \
  --target signage-lab \
  --inventory inventory.yml
```

Selectors are resolved in order, merged into a union, then deduplicated by host name.

## 6. Limit Host Parallelism

Control how many hosts execute at once:

```bash
preflight apply playbooks/lobby.yml \
  --target all \
  --inventory inventory.yml \
  --concurrency 5
```

`0` means unlimited host concurrency.

This is useful when you want to avoid rebooting or updating an entire fleet at the same moment.

## 7. Gather Facts Explicitly

Facts for one host:

```bash
preflight facts lobby-pc-01 --inventory inventory.yml
```

Facts for a group:

```bash
preflight facts --target lobby --inventory inventory.yml
```

For several hosts, the command prints a JSON object keyed by host name.

## 8. Inspect Per-Host State

Inventory-backed applies write a separate state file per host:

```bash
preflight state show --state-file state/targets/lobby-pc-01.json
preflight state diff playbooks/lobby.yml --state-file state/targets/lobby-pc-01.json
```

That split is deliberate. It keeps state comparisons meaningful even when one playbook is applied to many machines with different facts or variable layers.

## Troubleshooting

### WinRM authentication fails

Check the host entry first:

- `address`
- `port`
- `username`
- `password` or `password_from`
- `https`

If the password is a secret reference, make sure the initiating machine can decrypt it through the project’s `age` identity.

### SSH connects but a task still fails

That usually means the playbook is using a module that SSH does not implement yet. The SSH target currently supports `directory`, `file`, and `shell`. Use WinRM for Windows-native modules such as `registry`, `service`, `user`, or `windows_feature`.

### I expected one shared state file

Inventory-backed applies write `state/targets/<host>.json` so each host has its own recorded task snapshot. Local runs still default to `state/provision.json`.
