# Secrets And `age`

Preflight uses repo-backed `age` encryption so you can keep secret material close to your configuration without storing it in plaintext.

## What Is `age`?

[`age`](https://age-encryption.org/) is a modern file encryption tool and format designed to be simple to use correctly.

At a high level:

- you encrypt data to one or more **recipients**
- you decrypt data with the matching **identity** file

In Preflight terms:

- `secrets.recipients` tells Preflight who can decrypt newly encrypted secrets
- `secrets.identity` tells Preflight which private key to use when it needs to decrypt a secret locally

If you are new to `age`, the smallest useful setup looks like this:

```bash
mkdir -p .age
age-keygen -o .age/keys.txt
```

That creates:

- a private identity file in `.age/keys.txt`
- a public recipient string you can copy into `preflight.yml`

> [!WARNING]
> The identity file is the secret. Treat it like a private key, keep it out of Git, and distribute it only to people or systems that should be able to decrypt project secrets.

## Why Encrypt Variables At All?

Configuration often contains values that are operationally sensitive even if they are not code:

- local admin passwords
- autologin credentials
- SSH private keys
- API tokens
- file share credentials

Encrypting those values changes the failure mode of the repo.

Without encryption:

- anyone with repo access can read the secret immediately
- accidental pushes, screenshots, logs, and copy-paste mistakes expose the real value
- rotating leaked secrets becomes a routine cleanup task

With repo-backed encryption:

- the encrypted file can live in the repo without revealing the plaintext
- only holders of the matching identity can decrypt it
- reviews can still capture that a secret changed, without exposing its contents

That is especially useful for Preflight's deployment model because the repo often needs to travel with the configuration.

## Why This Fits Preflight

Preflight is designed around portable, self-contained configuration. Repo-backed encrypted secrets fit that model well because they:

- work offline
- keep secret references next to the playbooks that use them
- avoid introducing a mandatory external secret service just to get started
- let the runner resolve `secret:<name>` references at execution time

The tradeoff is that your key management discipline matters. Encryption helps only if the identity files are stored and shared carefully.

## Where Decryption Happens

The key question is not just "what is encrypted?" but "which machine actually decrypts it?"

In the current Preflight implementation, decryption happens on the machine running `preflight`, using the identity file referenced by `secrets.identity` in `preflight.yml`.

That means:

- if you run `preflight apply` on your laptop, your laptop needs the identity
- if you copy the project to a target PC and run `preflight apply` there, the target PC needs the identity

The encrypted files and the public recipient strings are safe to move around with the project. The private identity file is the sensitive part that must be distributed intentionally.

## What You Need To Move To A Target PC

If you prepare the project on one machine and then apply it locally on the target machine, copy:

- the `preflight` binary
- the project files
- the encrypted secret files
- one private age identity that matches one of the configured recipients

You do **not** copy a "recipient" in the secret sense. A recipient is the public key you encrypt **to**. What you actually need for decryption is the matching private identity.

So the usual pattern is:

1. Generate identities for the humans or machines that should be allowed to decrypt.
2. Put their public recipient strings in `secrets.recipients`.
3. Commit the encrypted `.age` files.
4. Provision the corresponding private identity only onto the machines that should be able to run with those secrets.

## Who Should Own Keys?

A useful default is:

- recipients belong to **principals that need decryption**
- a principal can be a developer, operator, CI job, or target machine

That is usually a better mental model than "one key per project" or "one key per repo."

## Should Coworkers Share One Private Key?

Usually no.

For teams, the better default is:

- each developer or operator has their own identity
- each person's public recipient is added to `secrets.recipients`
- the same encrypted files are readable by all listed recipients

Why this is better:

- you do not have to share one private key out-of-band
- you can remove one person's access without replacing every other person's key
- compromise of one private key is easier to contain

The tradeoff is that when recipients change, you need to re-encrypt secrets so the new recipient set is reflected in the encrypted files.

## Do Keys Belong To Developers Or Targets?

They can belong to either. The right answer depends on where decryption happens in your workflow.

### Developer Or Operator Identities

Use these when a human-controlled machine runs Preflight and needs to decrypt secrets before applying changes.

This is the best fit for:

- development
- testing
- operator-driven deployments

### Target Identities

Use these when the target machine itself runs Preflight locally and must decrypt secrets on its own.

This is the best fit for:

- self-contained target-side execution
- offline deployment workflows
- machines that need to re-run configuration without a separate operator machine

### Shared Service Or Automation Identities

Use these for CI, packaging systems, or deployment automation that must decrypt project secrets non-interactively.

Treat these like machine credentials, not like human credentials.

## Should You Use One Identity Per Project?

Sometimes, but it is rarely the best default.

A single shared project identity is simple, but it has real downsides:

- everyone with the key has the same access
- you cannot revoke one person cleanly
- rotation is more painful
- it blurs the difference between human access and machine access

It can still be acceptable in a very small or tightly controlled environment, especially for short-lived experiments or air-gapped setups, but it scales poorly.

## A Practical Recommendation

For most teams, a good starting model is:

- one identity per developer or operator
- optional additional identities for specific deployment machines or automation
- all of their public recipients listed in `secrets.recipients`
- encrypted secret files committed to the repo
- private identities stored separately from the repo

That gives you:

- shared access to the same encrypted project
- no need to share one private key
- a clean path to add or remove people and machines later

## Is This More Secure Than Other Approaches?

Sometimes yes, sometimes no. The better question is:

```text
More secure than which approach, for which threat model?
```

## Compared With Plaintext In Git

Yes, this is much safer than committing passwords or tokens directly in YAML.

Why:

- the repo no longer contains the plaintext value
- accidental disclosure of the repository alone is not enough to reveal the secret
- you can share configuration more broadly than private key material

If your current alternative is "put the password in the playbook," `age` is a clear upgrade.

## Compared With `.env` Files Or Local Unencrypted Files

Usually yes, if those files are stored on disk in plaintext.

Unencrypted variable files are easy to:

- commit by mistake
- back up insecurely
- leave behind on build agents or operator laptops

Encrypted files reduce that exposure, although once a secret is decrypted for use, the surrounding machine still needs to be trusted.

## Compared With Environment Variables

It depends.

Environment variables can be a good delivery mechanism, but they are not automatically a secure storage system. In some environments they are exposed through:

- process inspection
- crash dumps
- shell history or wrapper scripts
- CI logs or debug output

Encrypted files are usually better for storing secrets at rest in a repo. Environment variables can still be useful for injecting secrets at runtime.

## Compared With An External Secret Manager

Not necessarily.

A dedicated secret manager can be stronger when you need:

- centralized access control
- audit trails
- short-lived credentials
- automatic rotation
- machine-to-machine secret delivery at scale

In those cases, an external system may be more secure overall because it reduces long-lived secret distribution and gives you better operational controls.

But external managers also add:

- infrastructure dependencies
- connectivity requirements
- more setup and maintenance

For offline-friendly deployments or smaller teams, repo-backed `age` encryption is often a practical middle ground.

## A Good Rule Of Thumb

Use repo-backed `age` secrets when you want:

- a simple default
- encrypted-at-rest secrets in the repo
- offline-friendly operation
- low operational overhead

Reach for an external secret manager when you need:

- centralized policy and auditing
- stronger rotation workflows
- dynamic or short-lived credentials
- large-scale multi-environment access control

## The Main Security Boundary To Remember

`age` protects the secret **at rest** in the repository.

It does not remove the need to secure:

- developer laptops
- CI runners
- deployment machines
- decrypted temp files
- private key distribution

That is why the right comparison is not "encrypted or not," but "where do secrets live, who can decrypt them, and how much control do we need around that process?"
