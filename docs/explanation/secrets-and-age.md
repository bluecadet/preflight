# Secrets And `age`

Preflight uses repo-backed [`age`](https://age-encryption.org/)
encryption so secret material can travel with a project's
configuration without ever being stored in plaintext. This page
explains why that model was chosen and what it does and does not
protect. For the commands that create identities, encrypt values, and
reference them from YAML, see
[Manage Secrets](../how-to/manage-secrets.md).

## Why `age`

`age` encrypts data to one or more public **recipients** and decrypts
it with a matching private **identity** file. Preflight maps that
directly onto its own configuration: `secrets.recipients` lists who
can encrypt new secrets, and `secrets.identity` names the private key
a given machine uses to decrypt them.

That model fits Preflight's deployment style well:

- it works offline, with no external service to reach at apply time
- encrypted secrets sit next to the playbooks that use them
- a project can be copied wholesale to another machine and still
  resolve its own secrets, given the right identity file
- the runner resolves secret references at execution time, so
  playbooks and inventory never carry plaintext

The tradeoff is that `age` does not manage key distribution for you.
Encryption only helps if identity files are stored and shared
carefully.

## How It Compares

| Alternative | When it wins | What Preflight loses by not using it |
|---|---|---|
| Plaintext in the repo | Never, for anything sensitive | N/A — this is the baseline `age` improves on |
| Local `.env` / unencrypted files | Quick local experiments | Files are easy to commit, back up, or leave on disk by mistake |
| Environment variables | Injecting a secret at run time | Not secure at rest; exposed via process listings, crash dumps, or CI logs |
| External secret manager (KMS, vault service) | Centralized access control, audit trails, short-lived credentials, machine-to-machine delivery at scale | Requires connectivity and infrastructure Preflight's offline-friendly model doesn't assume |

Repo-backed `age` secrets are a practical middle ground: better than
anything stored in plaintext, and simpler to operate than a dedicated
secret manager, at the cost of the centralized controls that a
managed service provides.

## Threat Model

`age` protects a secret **at rest** in the repository. Anyone who
obtains the repo alone — through a clone, a backup, or a leaked
archive — sees only ciphertext.

It does not protect:

- a stolen laptop or target machine that also holds a private
  identity file
- a compromised controller, which can decrypt anything its configured
  identity has access to
- decrypted temporary files or in-memory values on a machine that is
  itself untrusted
- careless distribution of the identity file itself

The right question is not "encrypted or not," but where secrets live,
who can decrypt them, and how much control that requires. Securing
developer laptops, CI runners, and deployment machines remains your
responsibility regardless of encryption.

## Key Ownership Model

Recipients belong to whichever principals need to decrypt: a
developer, an operator, a CI job, or a target machine. Sharing one
private identity across a team is usually a worse default than giving
each principal its own identity and listing every public recipient in
`secrets.recipients`. The encrypted files stay identical; only the
list of who can open them changes. That way one compromised or
departing identity can be dropped without re-issuing everyone else's
key, and a human's access is never conflated with a machine's.

Because recipients are additive, changing that list requires
re-encrypting existing secrets so the new recipient set can actually
decrypt them — Preflight calls this rekeying. A single project-wide
identity is still workable for small or air-gapped setups, but it
blurs the distinction between human and machine access and makes
rotation more painful as the project grows.

## Where Secrets Appear At Runtime

Decryption happens on whichever machine is running `preflight`, using
the identity referenced by `secrets.identity`. If you run `apply` on
your laptop, your laptop needs the identity; if a target machine runs
`preflight` against itself, the target needs it; if you `apply` from a
staged bundle, the identity on the applying machine must match a
recipient the bundle's secrets were encrypted to. The encrypted files
and public recipients are safe to move around with the project — only
the private identity is sensitive enough to distribute deliberately.

## Related Docs

- [Manage Secrets](../how-to/manage-secrets.md)
- [Config Reference](../reference/config.md)
- [Configure Git Credentials](../how-to/configure-git-credentials.md)
