# Secrets and `age`

Preflight encrypts secrets with [`age`](https://age-encryption.org/) and stores them in the project repo, so secrets travel with the configuration without ever sitting in plaintext. This page explains the model and its limits. For the commands, see [Manage secrets](../how-to/manage-secrets.md).

## The `age` model

`age` encrypts data to one or more public recipients and decrypts it with a matching private identity file. Preflight maps that onto its config: `secrets.recipients` lists the public keys each secret is encrypted to, and `secrets.identity` names the private key a machine uses to decrypt.

The model suits Preflight's deployments:

- no external service is needed at apply time, so it works offline
- encrypted secrets sit next to the playbooks that use them
- a project copied to another machine still resolves its secrets, given a matching identity file
- playbooks and inventory reference secrets by name and never hold plaintext

`age` does not distribute keys. Encryption helps only if identity files are stored and shared carefully.

## Alternatives

| Alternative | When it wins | Drawback |
|---|---|---|
| Plaintext in the repo | Never, for anything sensitive | Anyone with the repo reads the secret |
| Local `.env` or other unencrypted files | Quick local experiments | Easy to commit, back up, or leave on disk by mistake |
| Environment variables | Injecting a secret at run time | Unprotected at rest; exposed through process listings, crash dumps, and CI logs |
| External secret manager (KMS, vault service) | Centralized access control, audit trails, short-lived credentials, delivery at scale | Requires connectivity and infrastructure that Preflight's offline model does not assume |

Repo-backed `age` secrets sit between plaintext files and a dedicated secret manager: safer than the first and simpler to run than the second, without the central controls a managed service provides.

## Threat model

`age` protects a secret at rest. Anyone who obtains only the repo, through a clone, a backup, or a leaked archive, sees ciphertext.

It does not protect against:

- a stolen laptop or target machine that also holds a private identity file
- a compromised controller, which can decrypt anything its identity can
- decrypted temporary files or in-memory values on an untrusted machine
- careless distribution of the identity file
- secrets shorter than 4 characters, or transformed before printing (re-encoded, split across lines, truncated), which output does not mask

Preflight replaces each resolved secret with `***` in terminal output, JSON output, and run logs. It matches the exact value only.

You remain responsible for securing developer laptops, CI runners, and deployment machines.

## Key ownership

Give each principal that needs to decrypt (a developer, an operator, a CI job, a target machine) its own identity, and list every public recipient in `secrets.recipients`. Sharing one identity across a team is a worse default. With separate identities, you can drop one compromised or departing identity without reissuing everyone else's key, and human access stays distinct from machine access.

Changing the recipient list requires re-encrypting existing secrets so the new set can decrypt them. Preflight calls this rekeying. A single project-wide identity still works for small or air-gapped setups, but it blurs human and machine access and makes rotation harder as the project grows.

## Decryption

The machine running `preflight` decrypts secrets with the identity named in `secrets.identity`. If you run `apply` from your laptop, your laptop needs the identity. If a target runs `preflight` against itself, the target needs it. For a staged bundle, pass an identity that matches one of the project's recipients with `apply --bundle <path> --secret-identity <file>`. Encrypted files and public recipients are safe to move with the project; only private identities need deliberate distribution.

## Related docs

- [Manage secrets](../how-to/manage-secrets.md)
- [Config reference](../reference/config.md)
- [Configure Git credentials](../how-to/configure-git-credentials.md)
