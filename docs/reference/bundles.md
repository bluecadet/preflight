# Bundle reference

A bundle is a zip archive that holds one staged execution plan for one target. `preflight stage` writes bundles; `preflight apply --bundle` runs them on the local machine without the original playbook, inventory, or action sources.

## Archive contents

| Path | Contents |
| --- | --- |
| `manifest.json` | Bundle metadata and checksums |
| `plan.json` | Execution plan with resolved tasks and module names |
| `plugins/<file>` | Plugin executables referenced by the plan |
| `secrets/<name>.age` | Encrypted secret payloads, when `secret_mode` is `encrypted` |
| `secrets/<name>` | Decrypted secret payloads, when `secret_mode` is `plaintext` |

`plan.json` keeps template expressions that reference `target`, `facts`, or `env`. They render at apply time against the local machine, including `when` conditions and task names.

`plan.json` and secret payloads use file mode `0600` when the bundle contains plaintext secrets.

## Bundle file name

```text
<playbook>-<target>-<os>-<arch>.zip
```

Each segment is sanitized. `<os>` and `<arch>` are the `target_os` and `target_arch` values. Bundles are written to `--bundle-output-dir`, which defaults to `dist/bundles` under the project directory.

## `manifest.json`

| Field | Type | Description |
| --- | --- | --- |
| `format_version` | integer | Bundle format version. Stage writes `2`; apply reads `1` and `2`. A missing value reads as `1`. |
| `created_at` | timestamp | Creation time in UTC |
| `playbook_name` | string | Source playbook name |
| `target_name` | string | Target name used during staging |
| `target_os` | string | Declared `platform.os`, or the OS version the target reported |
| `target_arch` | string | Declared `platform.arch`, or the architecture the target reported |
| `build` | object | `version`, `commit`, and `date` of the staging binary |
| `modules` | array | Modules the plan references |
| `checksums` | object | SHA-256 hex digest for every archive entry except `manifest.json`, keyed by path |
| `lock_entries` | array | Entries from `preflight.lock`, sorted by ref |
| `secret_mode` | string | `encrypted`, `plaintext`, or omitted when the bundle carries no secrets |
| `secret_entries` | array | `{name, path}` for each bundled secret |

Each `modules[]` entry:

| Field | Type | Description |
| --- | --- | --- |
| `name` | string | Module name |
| `kind` | string | `builtin` or `plugin` |
| `path` | string | Archive path of the plugin executable; plugins only |
| `version` | string | Version the plugin reported; plugins only |

## Staging

Staging fails when:

- a task references an unknown module
- a referenced plugin cannot be read, fails `initialize`, or reports a name that differs from its file name
- a referenced plugin is built for a different OS or architecture than the target; cross-platform bundles support built-in modules only
- a parameter whose key contains `password`, `secret`, `token`, `private_key`, or `credential` holds a literal value instead of a `secret:` reference, and `--allow-plaintext-secrets-in-bundle` is not set
- a referenced secret is not defined in `preflight.yml`

Secrets referenced as `secret:<name>` are bundled as the encrypted `.age` files from the project. With `--allow-plaintext-secrets-in-bundle`, stage decrypts them into the bundle instead and prints a warning.

A host with a declared `platform` is not contacted during staging. Otherwise Preflight connects to the host to read its OS and architecture. See [platform fields](./inventory.md#platform-fields).

## Bundle apply

`preflight apply --bundle <bundle.zip>`:

1. extracts the archive to a temporary directory
2. reads `manifest.json` and rejects unsupported format versions
3. verifies every entry against `checksums`; an extra, missing, or altered entry fails the apply
4. builds the module registry from built-ins and the bundled `plugins/` directory only
5. runs `plan.json` on the local machine

Encrypted bundle secrets require `--secret-identity <path>`. A bundle with plaintext secrets prints a warning before the run. The `--target`, `--var`, and tag flags have no effect on bundle apply.

## Related docs

- [Stage bundles for air-gapped deployment](../how-to/air-gapped-deployment.md)
- [Plugin reference](./plugins.md)
