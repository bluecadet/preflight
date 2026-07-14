# Project Config Reference

This page describes `preflight.yml`, the project-level configuration file parsed by [`internal/config/`](/Users/clay/repos/preflight/internal/config).

## Purpose

`preflight.yml` holds:

- project metadata
- shared variables
- embedded inventory
- repo-backed secret configuration

It is optional. If the file is missing, Preflight loads an empty config with empty `vars` and `secrets.entries`.

## Top-Level Fields

| Field | Type | Meaning |
| --- | --- | --- |
| `project` | string | Project identifier, available as `vars.preflight.project` in templates |
| `environment` | string | Environment label such as `production` or `staging`, available as `vars.preflight.environment` in templates |
| `vars` | object | Project-level variables available to playbooks |
| `inventory` | object | Host-first inventory for remote or multi-host runs |
| `secrets` | object | Repo-backed `age` secret configuration |

## `secrets`

| Field | Type | Meaning |
| --- | --- | --- |
| `identity` | string | Path to the `age` private identity used for decryption |
| `recipients` | string[] | Public `age` recipients used when encrypting secrets |
| `entries` | object | Logical secret names mapped to encrypted files |

Each `entries.<name>` object supports:

| Field | Type | Meaning |
| --- | --- | --- |
| `file` | string | Path to the encrypted `.age` file, relative to the project root unless absolute |
| `type` | string | Optional secret kind hint |

## Example

```yaml
project: natural-history-museum
environment: production

vars:
  content_root: "C:\\Exhibits\\content"
  fileserver: "\\\\nas01\\exhibits"

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
  identity: ".age/keys.txt"
  recipients:
    - "age1example..."
  entries:
    autologin-password:
      file: "secrets/autologin-password.age"
    gallery-key:
      file: "secrets/gallery-key.age"
      type: "file"
```

## Variable Role

`vars` is the project-wide baseline layer — the lowest-precedence layer in
the [variable merge order](./inventory.md#variable-merge-order); every
other variable source can override it.

## Secret Resolution Model

Secret files are not decrypted during planning unless a code path explicitly resolves them. At execution time, string fields such as `password` or `private_key` can use inline `secret:<name>` references that resolve through the built-in provider.

## Related Docs

- [Manage secrets](../how-to/manage-secrets.md)
- [Templating and facts reference](./templating.md)
- [Secrets and `age`](../explanation/secrets-and-age.md)
