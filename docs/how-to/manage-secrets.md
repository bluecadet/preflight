# Manage Secrets

Use this guide to store project secrets in repo-backed `age` files and reference them from playbooks or actions.

If you want the background first, read [Secrets and age](../explanation/secrets-and-age.md).

## Before You Start

You need:

- The `age` CLI installed so you can generate a keypair
- An `age` identity file for decryption
- One or more `age` recipients for encryption
- A project root where `preflight.yml` lives

If you are starting from zero, create an identity file first:

```bash
mkdir -p .age
age-keygen -o .age/keys.txt
```

The generated file includes a public recipient string that starts with `age1...`. Add that recipient to `preflight.yml` so Preflight can encrypt new secrets for you.

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

> [!TIP]
> A common pattern is to commit the encrypted `.age` files to the repo, but keep `.age/keys.txt` out of version control.

If multiple people or machines need to decrypt the same project secrets, add multiple recipients:

```yaml
secrets:
  identity: ".age/keys.txt"
  recipients:
    - "age1developerrecipient..."
    - "age1operatorrecipient..."
    - "age1targetrecipient..."
```

Any matching private identity can decrypt the same encrypted secret file.

## 2. Encrypt A Secret From A File

Run:

```bash
preflight secret encrypt autologin-password \
  --from-file ./secrets/autologin-password.txt
```

If the secret entry does not already exist, Preflight creates one in `preflight.yml` under `secrets.entries`.

You can override configured recipients or identity:

```bash
preflight secret encrypt autologin-password \
  --from-file ./secrets/autologin-password.txt \
  --recipient age1example... \
  --identity .age/keys.txt
```

## 3. List Configured Secrets

Run:

```bash
preflight secret list
```

This prints the logical secret name and its encrypted file path.

## 4. Edit A Secret Safely

Run:

```bash
EDITOR=nvim preflight secret edit autologin-password
```

Preflight decrypts the secret to a temporary file, opens your editor, then re-encrypts the updated content.

> [!WARNING]
> If `EDITOR` is not set, Preflight falls back to `vi`.

## 5. Reference A Secret In YAML

Some task and action inputs support the `_from` pattern. For example:

```yaml
tasks:
  - name: Configure auto-login
    uses: preflight/autologin
    with:
      username: exhibituser
      password_from: secret:autologin-password
```

During execution, the runner resolves secret-backed values before invoking the module.

## 6. Move A Project To A Target PC

If you build out the repo on one machine, then want to run `preflight apply` locally on a target PC, the target needs the files required for local decryption.

Move these to the target:

- the `preflight` binary
- the project repo or exported project directory
- `preflight.yml`
- the encrypted secret files referenced under `secrets.entries`
- an age identity file that matches one of the configured recipients

The target does **not** need every recipient. It only needs one private identity that can decrypt the secrets.

In practice, that usually means:

- commit `preflight.yml` and encrypted `secrets/*.age` files to the repo
- do **not** commit the private identity file
- copy or provision the private identity onto the target separately
- make sure the identity exists at the path named by `secrets.identity`

## Troubleshooting

### `no secrets.identity configured in preflight.yml`

Set `secrets.identity` to a readable age identity file.

### `no secrets.recipients configured in preflight.yml`

Add at least one `age` recipient or pass `--recipient`.

### `secret "<name>" is not defined in preflight.yml`

Create the entry under `secrets.entries`, or run `secret encrypt` so Preflight can create one for you.
