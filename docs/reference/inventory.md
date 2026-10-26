# Inventory reference

The `inventory` block in `preflight.yml` defines hosts, groups, transports, and inventory variables. A host is one entry in `inventory.hosts`.

## Example

```yaml
inventory:
  vars:
    timezone: Eastern Standard Time

  groups:
    lobby:
      vars:
        area: lobby

  hosts:
    - name: lobby-pc-01
      address: 192.168.1.10
      transport: winrm
      platform:
        os: windows
        arch: amd64
      username: exhibit-admin
      password: secret:winrm-password
      groups: [lobby]
      vars:
        display: primary
```

## Inventory fields

| Field | Type | Description |
| --- | --- | --- |
| `vars` | object | Variables for every host |
| `groups` | object | Group names mapped to group objects |
| `hosts` | host[] | Host entries |

## Group fields

| Field | Type | Description |
| --- | --- | --- |
| `vars` | object | Variables for hosts that list the group |

## Host fields

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `name` | string | required | Unique host name |
| `address` | string | `name` | Host name or IP address |
| `transport` | enum | `ssh` | `ssh`, `winrm`, or `local` |
| `port` | integer | `22` for `ssh`; `5985` for `winrm`, `5986` with `https` | Connection port |
| `username` | string | | Login user |
| `password` | string | | Password; accepts `secret:<name>` |
| `private_key` | string | | SSH private key as PEM content or a file path; accepts `secret:<name>` |
| `private_key_passphrase` | string | | Passphrase for an encrypted `private_key`; accepts `secret:<name>` |
| `known_hosts_file` | string | `~/.ssh/known_hosts` | SSH known hosts file |
| `host_key_policy` | enum | `accept-new` | `accept-new`, `strict`, or `insecure`; see [SSH host key verification](../explanation/targets-and-transports.md#ssh-host-key-verification) |
| `host_key_algorithms` | string[] | | Accepted SSH host key algorithms, such as `[ssh-ed25519, ssh-rsa]` |
| `timeout` | string | `30s` for SSH | Positive Go duration for the SSH or WinRM connection, such as `30s` or `1m` |
| `https` | bool | `false` | Use HTTPS for WinRM |
| `groups` | string[] | | Group names, applied in order for variable precedence |
| `vars` | object | | Host variables |
| `platform` | object | | OS and architecture for staging without a connection; see [Platform fields](#platform-fields) |
| `jump` | object | | SSH bastion; see [Jump host fields](#jump-host-fields) |

Parsing fails when:

- two hosts share a `name`
- a host lists a group that `groups` does not define
- `timeout` is not a positive duration
- `host_key_policy` is not one of the listed values
- `jump.address` is missing

SSH tries an agent through `SSH_AUTH_SOCK` and the default keys in `~/.ssh` (`id_ed25519`, `id_ecdsa`, `id_rsa`) when `private_key` and `password` are both unset. See [SSH authentication](../explanation/targets-and-transports.md#ssh-authentication).

WinRM uses Basic authentication with a local Windows account.

`transport: local` runs on the machine running Preflight; `address` and the connection fields are ignored.

### Platform fields

`platform` declares the host OS and architecture for `preflight stage`. Both fields are required when the block is present.

| Field | Type | Values |
| --- | --- | --- |
| `os` | enum | `windows` |
| `arch` | enum | `amd64`, `arm64` |

With `platform`, staging records these values in the bundle and does not connect to the host. Without it, staging connects to the host to read them. Other commands always connect through the configured transport.

### Jump host fields

`jump` routes an SSH connection through one bastion. A jump host cannot have its own `jump`, and it inherits no fields from the host it fronts.

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `address` | string | required | Bastion host name or IP address |
| `port` | integer | `22` | Bastion SSH port |
| `username` | string | | Bastion login user |
| `password` | string | | Password; accepts `secret:<name>` |
| `private_key` | string | | SSH private key as PEM content or a file path; accepts `secret:<name>` |
| `private_key_passphrase` | string | | Passphrase for an encrypted `private_key`; accepts `secret:<name>` |
| `known_hosts_file` | string | `~/.ssh/known_hosts` | Known hosts file for the bastion |
| `host_key_policy` | enum | `accept-new` | `accept-new`, `strict`, or `insecure` |

The bastion connection uses the 30-second default timeout.

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

## Variable merge order

Later layers win. Maps merge deeply, so nested keys from earlier layers survive unless a later layer sets the same key.

```text
preflight.yml vars
  -> inventory.vars
  -> group vars, in the host's groups order
  -> host vars
  -> vars.preflight (project, environment)
  -> playbook vars
  -> --var flags
```

Inventory, group, and host vars cannot override `vars.preflight`; playbook vars and `--var` can. See [built-in preflight variables](./templating.md#built-in-preflight-variables).

Action inputs are not a layer in this chain. A task's `with` values and the action's input defaults form a scope for that action's tasks, and `with` wins.

## Selector resolution

`--target` accepts host names, group names, `all`, `local`, and `localhost`.

- With no `--target`, every inventory host is selected. Without an inventory, the run targets the local machine.
- When every selector is `local` or `localhost` (case-insensitive), the run targets the local machine and the inventory is not read.
- `all` selects every host.
- A selector that matches a group selects the group's hosts, even when a host has the same name.
- A selector that matches neither fails with `inventory: target "<name>" not found (no group or host with that name)`.
- Multiple selectors select the union. A host selected twice runs once.
- A selector without an inventory fails with `no inventory configured in preflight.yml`.

## Related docs

- [Run a playbook against remote hosts](../how-to/remote-execution.md)
- [Troubleshoot remote connections](../how-to/troubleshoot-remote-connections.md)
- [Targets, transports, and plugins](../explanation/targets-and-transports.md)
- [Deploy across restricted networks](../explanation/restricted-network-deployment.md)
