# Run a playbook against remote hosts

Select hosts from the `inventory:` block in `preflight.yml` and run a playbook on them over WinRM or SSH. This guide assumes you know the validate, plan, check, and apply loop from [Run a playbook](./run-a-playbook.md).

## Prerequisites

- `preflight` installed on the controller
- A playbook
- Windows targets with WinRM or OpenSSH enabled ([Enable remote access on a Windows target](./enable-remote-access.md))

If the controller cannot open connections to the targets, read [Deploy across restricted networks](../explanation/restricted-network-deployment.md) first.

## 1. Define inventory hosts

Add hosts to `preflight.yml`:

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

    - name: signage-pc-01
      address: 192.168.1.50
      transport: ssh
      username: exhibit
      private_key: secret:signage-key
      groups: [signage]
```

See the [Inventory reference](../reference/inventory.md) for every host, group, and jump-host field.

## 2. Check host resolution

```bash
preflight inventory list
```

The output lists each host's address, transport, port, and groups.

## 3. Test connectivity with facts

```bash
preflight facts lobby-pc-01
preflight facts --target lobby
```

A successful `facts` run proves that authentication and remote command execution work. If it fails, see [Troubleshoot remote connections](./troubleshoot-remote-connections.md).

## 4. Select hosts and apply

```bash
preflight apply playbooks/lobby.yml --target lobby
preflight apply playbooks/lobby.yml --target lobby-pc-01
preflight apply playbooks/lobby.yml --target lobby --target signage
preflight apply playbooks/lobby.yml --target local
```

A selector is a host name, a group name, or `all`. Repeated `--target` flags select the union of their hosts. Without `--target`, a project with an inventory runs on every host; `--target local` runs on the controller instead. See [Selector resolution](../reference/inventory.md#selector-resolution).

`plan`, `check`, and `apply` run once per selected host.

## 5. Limit concurrency

```bash
preflight apply playbooks/lobby.yml --target all --concurrency 5
```

`--concurrency` caps how many hosts run at once. The default, `0`, means no limit. Task order within a host still follows the playbook.

## Known limitations

Some Windows operations fail over WinRM: changing `windows_feature` state, `remove_appx_packages` with all-users scope, and line-by-line `powershell` output. See [WinRM session limitations](../reference/modules.md#winrm-session-limitations) for workarounds.

## Related docs

- [Troubleshoot remote connections](./troubleshoot-remote-connections.md)
- [Error reference](../reference/errors.md)
- [Inventory reference](../reference/inventory.md)
- [Configure another user's settings](./configure-another-users-settings.md)
