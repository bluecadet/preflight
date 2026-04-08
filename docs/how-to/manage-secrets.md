# Manage Secrets

Use this guide to store sensitive values in repo-backed `age` files and reference them from playbooks, actions, or inventory.

If you want the design rationale first, read [Secrets and `age`](../explanation/secrets-and-age.md).

## Prerequisites

- The `age` CLI installed locally
- A project root that contains `preflight.yml`
- At least one `age` identity that can decrypt the project secrets

If you are starting from zero, generate an identity:

```bash
mkdir -p .age
age-keygen -o .age/keys.txt
```

The output includes a public recipient string beginning with `age1...`. That public value goes into `preflight.yml`; the private identity file stays out of version control.

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

## 2. Encrypt A Secret From A File

Run:

```bash
preflight secret encrypt autologin-password \
  --from-file ./secrets/autologin-password.txt
```

If the named entry does not already exist, Preflight creates it in `preflight.yml` and defaults the encrypted path to `secrets/<name>.age`.

Override recipients or identity when needed:

```bash
preflight secret encrypt autologin-password \
  --from-file ./secrets/autologin-password.txt \
  --recipient age1example... \
  --identity .age/keys.txt
```

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

The built-in provider name is `secret`, so repo-backed references use `secret:<name>`.

## 6. Move The Project To Another Machine

If a different machine will run `preflight`, it needs everything required for local decryption:

- the `preflight` binary
- the repo or exported project directory
- `preflight.yml`
- the encrypted `.age` files referenced by `secrets.entries`
- one private identity matching one of the configured recipients

This is the important rule: decryption happens on whichever machine is actually running Preflight.

## Troubleshooting

### `no recipients configured`

Add `secrets.recipients` to `preflight.yml` or pass one or more `--recipient` flags to `preflight secret encrypt`.

### `no identity configured`

Set `secrets.identity` in `preflight.yml` or pass `--identity` for the current command.

### `secret "<name>" is not defined`

Create the entry under `secrets.entries`, or run `preflight secret encrypt <name> --from-file ...` so Preflight can create it for you.
