# Manage Secrets

Use this guide to store sensitive values in repo-backed `age` files and reference them from playbooks, actions, or inventory.

If you want the design rationale first, read [Secrets and `age`](../explanation/secrets-and-age.md).

## Prerequisites

- A project root that contains `preflight.yml`
- At least one `age` identity that can decrypt the project secrets

If you are starting from zero, generate an identity:

```bash
preflight secret identity generate --out .age/keys.txt
```

The output includes a public recipient string beginning with `age1...`. That public value goes into `preflight.yml`; the private identity file stays out of version control.

To print the public recipient for an existing identity file later:

```bash
preflight secret identity recipient .age/keys.txt
```

> [!WARNING]
> The identity file is the secret. Treat it like a private key, keep
> it out of Git, and distribute it only to people or systems that
> should be able to decrypt project secrets.

## 1. Configure Secrets In `preflight.yml`

Example:

```yaml
project: museum-kiosk

secrets:
  identity: ".age/keys.txt"
  recipients:
    - "age1ql3z7hjy54pw5k8kr0jsjrl4f8yl0v0l7x7y9h8n5v9s0k4m5qkq9v9abc"
  entries:
    autologin-password:
      file: "secrets/autologin-password.age"
```

What each field means:

- `identity` is the private key file Preflight uses to decrypt locally.
- `recipients` are the public keys used for encryption.
- `entries` maps logical secret names to encrypted files in the repo.

If multiple people or machines need access, add multiple recipients. They can all decrypt the same encrypted file with their own private identities.

## 2. Encrypt A Secret

Preflight can read the plaintext from a file, from standard input, or from an
interactive prompt. Pick the source that keeps the plaintext closest to memory
and farthest from disk and shell history.

### From An Interactive Prompt (Recommended For One-Offs)

Run with no source flag in a terminal:

```bash
preflight secret encrypt autologin-password
```

Preflight prompts twice without echoing, then encrypts the entered value. The
plaintext never touches disk and never appears in your shell history.

### From Standard Input (Recommended For Scripts And Password Managers)

```bash
op read "op://Vault/Item/password" | preflight secret encrypt autologin-password --from-stdin
```

```bash
printf '%s' "$LOOKED_UP_PASSWORD" | preflight secret encrypt autologin-password --from-stdin
```

A single trailing `\n` or `\r\n` is trimmed, so `echo "value" | preflight secret encrypt ... --from-stdin` works as expected. Embedded newlines (for multi-line secrets like PEM blocks) are preserved.

Avoid `preflight secret encrypt foo --from-stdin <<<"$secret"` patterns that
expand the secret on the command line — that exposes it to process listings.
Prefer piping from a tool that emits the secret directly.

### From A File

```bash
preflight secret encrypt autologin-password \
  --from-file ./secrets/autologin-password.txt
```

Useful when the plaintext is already on disk (for example, a downloaded PEM).
Delete the plaintext file afterwards.

### Common Notes

If the named entry does not already exist, Preflight creates it in `preflight.yml` and defaults the encrypted path to `secrets/<name>.age`.

Override recipients or identity when needed:

```bash
preflight secret encrypt autologin-password \
  --recipient age1example... \
  --identity .age/keys.txt
```

`--from-file` and `--from-stdin` are mutually exclusive. If neither is set and
stdin is not a terminal, Preflight refuses to run rather than silently consume
whatever happens to be piped in.

## 3. List Configured Secrets

```bash
preflight secret list
```

This prints the logical name plus the encrypted file path recorded in `preflight.yml`.

## 4. Edit A Secret Safely

```bash
EDITOR=nvim preflight secret edit autologin-password
```

Preflight decrypts the secret to a temporary file, opens your editor, then re-encrypts the updated contents.

Notes:

- If `EDITOR` is not set, Preflight falls back to `vi`.
- The temporary plaintext file is created outside the repo and cleaned up after the edit flow.

## 5. Reference Secrets In YAML

Use inline `secret:<name>` references anywhere a string field accepts a secret value:

```yaml
tasks:
  - name: Configure auto-login
    uses: preflight/autologin
    with:
      username: exhibituser
      password: secret:autologin-password
```

Inventory can do the same thing:

```yaml
hosts:
  - name: lobby-pc-01
    transport: winrm
    username: exhibit-admin
    password: secret:winrm-password
```

For file payloads, use the `file` module's `content` field:

```yaml
tasks:
  - name: Write license file
    file:
      dest: "C:\\Exhibits\\license.txt"
      content: secret:license-file
```

When only part of a file is secret, use `content_template` and reference
secrets with `secret("name")`:

```yaml
tasks:
  - name: Write app config
    file:
      dest: "C:\\Exhibits\\app.ini"
      content_template: |
        username={{ vars.app_user }}
        password={{ secret("app-password") }}
```

The built-in provider name is `secret`, so repo-backed references use `secret:<name>`.
Use `secret:<name>` when a whole string field should resolve to one secret.
Use `secret("name")` only inside `content_template`, where the secret is
interpolated into a larger file body.

## 6. Rekey Secrets After Recipient Changes

When `secrets.recipients` changes, re-encrypt the secret files so the new recipient set can decrypt them:

```bash
preflight secret rekey
```

To rekey only specific secrets:

```bash
preflight secret rekey autologin-password winrm-password
```

`secret rekey` uses the configured `secrets.identity` to decrypt existing files and the configured `secrets.recipients` to write updated files. You can override either value for the command:

```bash
preflight secret rekey \
  --identity .age/keys.txt \
  --recipient age1example...
```

When you pass an identity or recipient override, Preflight saves the updated setting back to `preflight.yml`. Overrides are only allowed when rekeying all configured secrets, because those settings apply project-wide.

## 7. Move The Project To Another Machine

If a different machine will run `preflight`, it needs everything required for local decryption:

- the `preflight` binary
- the repo or exported project directory
- `preflight.yml`
- the encrypted `.age` files referenced by `secrets.entries`
- one private identity matching one of the configured recipients

This is the important rule: decryption happens on whichever machine is actually running Preflight.

For staged offline bundles, that usually means adding a target machine recipient, rekeying the secrets, staging the bundle, and applying it on the target with that target's identity. See [Stage bundles for air-gapped deployment](./air-gapped-deployment.md#1-prepare-encrypted-secrets-for-target-side-apply).

## Troubleshooting

### `no recipients configured`

Add `secrets.recipients` to `preflight.yml` or pass one or more `--recipient` flags to `preflight secret encrypt`.

### `no identity configured`

Set `secrets.identity` in `preflight.yml` or pass `--identity` for the current command.

### `secret "<name>" is not defined`

Create the entry under `secrets.entries`, or run `preflight secret encrypt <name> --from-file ...` so Preflight can create it for you.
