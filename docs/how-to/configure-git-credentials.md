# Configure Git credentials for a target

Authenticate `preflight/git-sync` against a private repository with an HTTPS token or an SSH deploy key.

## Prerequisites

- A `preflight/git-sync` task (see [Sync a Git repo on a target](./sync-a-git-repo.md))
- An `age` identity configured for the project (see [Manage secrets](./manage-secrets.md))

## 1. Store the credential as a secret

Encrypt the token or private key:

```bash
preflight secret encrypt github-deploy-token
```

## 2. Authenticate over HTTPS with a token

Pass the token in `http_password`:

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
      http_password: "{{ vars.github_token }}"
```

`http_username` defaults to `x-access-token`, which GitHub accepts for tokens. Set it if your Git host expects a different username.

## 3. Authenticate over SSH with a deploy key

Pass the private key in `ssh_private_key` and the host key in `ssh_known_hosts`:

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
```

`ssh_strict_host_key_checking` defaults to `true`. Setting it to `false` removes protection against a spoofed Git host; do so only in throwaway environments where you cannot supply `ssh_known_hosts`.

The action passes credentials to the sync script as environment variables, not script text. HTTPS credentials reach Git through `GIT_ASKPASS`. The SSH key goes to a temporary file, readable only by the connecting account, that `GIT_SSH_COMMAND` uses and the action deletes after the sync.

The inventory `private_key` authenticates only the controller's connection to the target. Preflight does not forward an SSH agent, so Git on the target never sees that key. Use `ssh_private_key`, or place a deploy key in the transport account's `%USERPROFILE%\.ssh` with `file` tasks.

## Troubleshooting

### HTTPS sync fails with an authentication error

Confirm the secret in `http_password` holds a valid, unexpired token and that `http_username` matches what your Git host expects.

### SSH sync fails with a host key verification error

`ssh_known_hosts` has no entry for the remote host, or is empty while `ssh_strict_host_key_checking` is `true`. Add the correct host key to `ssh_known_hosts`.

### Credentials fail only after `apply --bundle`

The bundle carries the encrypted secrets, and `preflight apply --bundle <bundle.zip> --secret-identity <identity-file>` decrypts them on the target. The identity must match one of the recipients the secrets were encrypted to when you staged the bundle.

## Related docs

- [Sync a Git repo on a target](./sync-a-git-repo.md)
- [Manage secrets](./manage-secrets.md)
- [`preflight/git-sync` input reference](../reference/stdlib-actions.md#preflightgit-sync)
