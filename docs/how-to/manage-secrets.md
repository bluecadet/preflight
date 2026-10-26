# Manage secrets

Store sensitive values in `age`-encrypted files in the repo and reference them from playbooks, actions, and inventory. For the design, see [Secrets and `age`](../explanation/secrets-and-age.md).

## Prerequisites

- A project root containing `preflight.yml`
- An `age` identity that can decrypt the project secrets

To create an identity:

```bash
preflight secret identity generate --out .age/keys.txt
```

The command prints a public recipient beginning with `age1`. Add the recipient to `preflight.yml` and keep the identity file out of Git.

To print the recipient for an existing identity file:

```bash
preflight secret identity recipient .age/keys.txt
```

> [!WARNING]
> The identity file is a private key. Keep it out of Git and give it
> only to people and machines that should decrypt project secrets.

## 1. Configure secrets in `preflight.yml`

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

- `identity` is the private key file Preflight decrypts with.
- `recipients` are the public keys Preflight encrypts to. Add one per person or machine that needs access; each decrypts with its own identity.
- `entries` maps secret names to encrypted files in the repo.

## 2. Encrypt a secret

Choose the plaintext source that keeps the value off disk and out of shell history.

### From an interactive prompt

```bash
preflight secret encrypt autologin-password
```

Preflight prompts twice without echoing. Use this for one-off values.

### From standard input

Use this for scripts and password managers:

```bash
op read "op://Vault/Item/password" | preflight secret encrypt autologin-password --from-stdin
```

Preflight trims one trailing `\n` or `\r\n` and keeps embedded newlines, so multi-line values such as PEM blocks survive.

Pipe from a tool that emits the secret directly. Expanding the secret into the command line, as in `<<<"$secret"`, exposes it to process listings.

### From a file

```bash
preflight secret encrypt autologin-password \
  --from-file ./secrets/autologin-password.txt
```

Delete the plaintext file afterwards.

### Encrypt options

If the entry does not exist, Preflight adds it to `preflight.yml` with the file path `secrets/<name>.age`.

`--from-file` and `--from-stdin` are mutually exclusive. With neither flag, Preflight prompts only when stdin is a terminal; otherwise it exits with an error.

`--recipient` and `--identity` replace `secrets.recipients` and `secrets.identity` and save the new values to `preflight.yml`:

```bash
preflight secret encrypt autologin-password \
  --recipient age1example... \
  --identity .age/keys.txt
```

## 3. List secrets

```bash
preflight secret list
```

The output shows each secret name and its encrypted file path.

## 4. Edit a secret

```bash
EDITOR=nvim preflight secret edit autologin-password
```

Preflight decrypts the secret to a temporary file outside the repo, opens `EDITOR` (default `notepad` on Windows, `vi` elsewhere), re-encrypts the result, and deletes the temporary file.

## 5. Reference secrets in YAML

Use `secret:<name>` as the whole value of a task parameter or action input:

```yaml
tasks:
  - name: Configure auto-login
    uses: preflight/autologin
    with:
      username: exhibituser
      password: secret:autologin-password
```

Inventory credentials accept the same form:

```yaml
hosts:
  - name: lobby-pc-01
    address: 10.0.10.21
    transport: winrm
    username: exhibit-admin
    password: secret:winrm-password
```

To write a secret to a file, set the `file` module's `content`:

```yaml
tasks:
  - name: Write license file
    file:
      dest: "C:\\Exhibits\\license.txt"
      content: secret:license-file
```

To embed a secret inside a larger file, use `content_template` with `secret("name")`:

```yaml
tasks:
  - name: Write app config
    file:
      dest: "C:\\Exhibits\\app.ini"
      content_template: |
        username={{ vars.app_user }}
        password={{ secret("app-password") }}
```

`secret("name")` works only inside `content_template`.

## 6. Rekey after changing recipients

After you change `secrets.recipients`, re-encrypt the secret files so the new recipients can decrypt them:

```bash
preflight secret rekey
```

To rekey specific secrets:

```bash
preflight secret rekey autologin-password winrm-password
```

`rekey` decrypts with `secrets.identity` and encrypts to `secrets.recipients`. To override either, rekey all secrets; Preflight saves the override to `preflight.yml`:

```bash
preflight secret rekey \
  --identity .age/keys.txt \
  --recipient age1example...
```

Preflight rejects `--identity` and `--recipient` when you name specific secrets.

## 7. Run from another machine

Preflight decrypts on the machine that runs it. That machine needs:

- the `preflight` binary
- the project directory, including `preflight.yml` and the encrypted `.age` files
- an identity matching one of the configured recipients

For offline bundles, add the target's recipient, rekey, stage, and apply on the target with the target's identity. See [Stage bundles for air-gapped deployment](./air-gapped-deployment.md#1-prepare-encrypted-secrets-for-target-side-apply).

## Troubleshooting

### `no recipients configured`

Add `secrets.recipients` to `preflight.yml`, or pass `--recipient` to `preflight secret encrypt`.

### `no secrets.identity configured in preflight.yml`

Set `secrets.identity` in `preflight.yml`, or pass `--identity`.

### `secret "<name>" is not defined`

Add the entry under `secrets.entries`, or run `preflight secret encrypt <name>` to create it.
