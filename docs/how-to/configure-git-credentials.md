# Configure Git Credentials For A Target

Use this guide when `preflight/git-sync` needs to authenticate against
a private repository — over HTTPS with a token, or over SSH with a
deploy key — such as a museum kiosk pulling exhibit content from a
repository the exhibit team keeps private. For the sync task itself,
see [Sync A Git Repo On A Target](./sync-a-git-repo.md).

## Prerequisites

- A `preflight/git-sync` task already in place (see
  [Sync A Git Repo On A Target](./sync-a-git-repo.md))
- An `age` identity configured for the project, so credential values can
  be stored as secrets rather than plaintext (see
  [Manage Secrets](./manage-secrets.md))

## 1. Store The Credential As A Secret

Encrypt the token, password, or private key content as a named secret
before referencing it from a playbook:

```bash
preflight secret encrypt github-deploy-token
```

`preflight secret encrypt` reads from `--from-file`, `--from-stdin`, or
an interactive prompt. See
[Manage Secrets](./manage-secrets.md) for the full secret workflow,
including how `secret:<name>` references resolve in playbooks.

## 2. Authenticate Over HTTPS With A Token

Pass the token through `http_username` and `http_password`:

```yaml
vars:
  github_token: secret:github-deploy-token

tasks:
  - name: Sync private exhibit content
    uses: preflight/git-sync
    with:
      repo: https://github.com/example/private-content.git
      dest: C:\Exhibits\Content
      ref: main
      http_username: x-access-token
      http_password: "{{ vars.github_token }}"
```

`http_username` is optional for token-based providers that accept any
username; check your Git host's documentation for what it expects.

## 3. Authenticate Over SSH With A Deploy Key

Pass the private key content through `ssh_private_key`, and pin the
expected host key with `ssh_known_hosts`:

```yaml
vars:
  deploy_key: secret:github-deploy-key
  github_known_hosts: secret:github-known-hosts

tasks:
  - name: Sync private exhibit content
    uses: preflight/git-sync
    with:
      repo: git@github.com:example/private-content.git
      dest: C:\Exhibits\Content
      ref: main
      ssh_private_key: "{{ vars.deploy_key }}"
      ssh_known_hosts: "{{ vars.github_known_hosts }}"
      ssh_strict_host_key_checking: true
```

`ssh_strict_host_key_checking` defaults to requiring host key
verification. Only set it to `false` for a throwaway environment where
you cannot pre-populate `ssh_known_hosts`, since disabling it removes
protection against a spoofed Git host.

## Why Credentials Go Through Environment Variables

`preflight/git-sync` passes `http_username`, `http_password`,
`ssh_private_key`, and `ssh_known_hosts` to the target through
environment variables consumed by a Git credential helper, rather than
interpolating them into script text. This keeps token and key material
out of the rendered PowerShell script and out of any transcript of the
command line that gets logged.

## No SSH Agent Forwarding

Preflight's SSH transport does not forward your agent to the target.
The `private_key` field on an inventory host authenticates the
connection from the controller to the target only — it is never made
available to `git` or other tools running on the target. To
authenticate git over SSH on a target, pass the key through
`ssh_private_key` as shown above, or place a dedicated deploy key in
the target account's `~/.ssh` with `file` tasks run under `become`.

## Troubleshooting

**HTTPS sync fails with an authentication error.** Confirm the secret
referenced by `http_password` still holds a valid, unexpired token, and
that `http_username` matches what your Git host expects for token
authentication.

**SSH sync fails with a host key verification error.** This means
`ssh_known_hosts` does not contain an entry matching the host presented
by the remote, or `ssh_strict_host_key_checking` is enabled without any
`ssh_known_hosts` content supplied. Populate `ssh_known_hosts` with the
correct host key before re-running.

**Credentials work when staged locally but fail after
`apply --bundle`.** Encrypted secrets travel with the staged bundle and
are decrypted on the target with
`preflight apply --bundle <bundle.zip> --secret-identity <identity-file>`.
Confirm the `--secret-identity` file matches the identity the bundle
was staged against.

## Related Docs

- [Sync A Git Repo On A Target](./sync-a-git-repo.md) — the
  `preflight/git-sync` task these credentials authenticate
- [Manage Secrets](./manage-secrets.md) — the `preflight secret`
  workflow and `secret:<name>` references
- [Embedded Stdlib Action Reference](../reference/stdlib-actions.md#preflightgit-sync) —
  full `preflight/git-sync` input table
- [Run Tasks As Another User](./run-tasks-as-another-user.md) — running
  `preflight/git-sync` under a dedicated kiosk account
