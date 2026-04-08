# Project Config Reference

This page describes `preflight.yml`, the project-level configuration file parsed by [`internal/config/`](/Users/clay/repos/preflight/internal/config).

## Purpose

`preflight.yml` holds:

- project metadata
- shared variables
- repo-backed secret configuration

It is optional. If the file is missing, Preflight loads an empty config with empty `vars` and `secrets.entries`.

## Top-Level Fields

| Field | Type | Meaning |
| --- | --- | --- |
| `project` | string | Project identifier, available as `vars.preflight.project` in templates |
| `environment` | string | Environment label such as `production` or `staging`, available as `vars.preflight.environment` in templates |
| `vars` | object | Project-level variables available to playbooks |
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

`vars` is the project-wide baseline layer. During a normal inventory-backed run, it sits below inventory vars, playbook vars, and CLI `--var` overrides.

## Secret Resolution Model

Secret files are not decrypted during planning unless a code path explicitly resolves them. At execution time, string fields such as `password` or `private_key` can use inline `secret:<name>` references that resolve through the built-in provider.

## Related Docs

- [Manage secrets](../how-to/manage-secrets.md)
- [Templating and facts reference](./templating.md)
- [Secrets and `age`](../explanation/secrets-and-age.md)
