# Project config reference

`preflight.yml` is the optional project configuration file. When it is missing, Preflight uses an empty config.

## Location

Preflight looks for `preflight.yml` in the project directory. The project directory is the nearest directory, starting at the playbook's directory and walking up, that contains `preflight.yml`, `preflight.lock`, `actions/`, or `.git`. If none is found, the playbook's directory is used.

## Top-level fields

| Field | Type | Description |
| --- | --- | --- |
| `project` | string | Project name; available as `vars.preflight.project` |
| `environment` | string | Environment label, such as `production`; available as `vars.preflight.environment` |
| `vars` | object | Project variables, the lowest-precedence layer in the [variable merge order](./inventory.md#variable-merge-order) |
| `inventory` | object | Hosts and groups; see [Inventory reference](./inventory.md) |
| `secrets` | object | `age`-encrypted secret configuration |

Unknown fields fail schema validation.

## `secrets`

| Field | Type | Description |
| --- | --- | --- |
| `identity` | string | Path to the `age` identity file used for decryption, relative to the project directory unless absolute |
| `recipients` | string[] | `age` recipients used for encryption |
| `entries` | object | Secret names mapped to entry objects |

Each `entries.<name>` object:

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `file` | string | yes | Path to the `.age` file, relative to the project directory unless absolute |
| `type` | string | no | Free-form hint; Preflight does not read it |

## Secret references

A string value that is exactly `secret:<name>` resolves to the decrypted contents of `entries.<name>` when the value is used. It works in:

- task and module parameters
- inventory `password`, `private_key`, and `private_key_passphrase`, including in `jump` blocks

A file is decrypted only when a reference to it is resolved. Decryption fails with `no secrets.identity configured in preflight.yml` when `identity` is unset. See [Manage secrets](../how-to/manage-secrets.md).

## Example

```yaml
project: natural-history-museum
environment: production

vars:
  content_root: "C:\\Exhibits\\content"

inventory:
  groups:
    lobby:
      vars:
        area: lobby
  hosts:
    - name: lobby-pc-01
      address: 192.168.1.10
      transport: winrm
      username: exhibit-admin
      password: secret:winrm-password
      groups: [lobby]

secrets:
  identity: .age/keys.txt
  recipients:
    - age1example...
  entries:
    winrm-password:
      file: secrets/winrm-password.age
```

## Related docs

- [Manage secrets](../how-to/manage-secrets.md)
- [Templating and facts reference](./templating.md)
- [Secrets and `age`](../explanation/secrets-and-age.md)
