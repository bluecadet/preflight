# Run Git Operations On A Target

Use this guide when you need to clone a repository, pull the latest commits, or configure git on a remote target. Preflight does not have a built-in `git` module — git operations run through the `shell` module on POSIX targets or the `powershell` module on Windows targets.

For Windows targets, prefer the embedded `preflight/git-sync` action when you need a reusable clone-or-update task. It handles fetch, checkout, reset, clean, submodules, Git LFS, and temporary HTTPS or SSH credential wiring.

`preflight/git-sync` also adds the destination path to Git's global `safe.directory` list by default before it runs sync checks. This avoids Git for Windows "dubious ownership" failures when the checkout is owned by a different Windows account than the one running Preflight.

## Prerequisites

- A working playbook and target connection
- `git` installed on the target machine (use `winget_package` or `package` on Windows, `shell` on POSIX)
- Git authentication configured on the target (see [Credentials](#credentials) below)

## Clone A Repository

Use `creates:` to make the clone idempotent — the task is skipped if the destination directory already exists:

**Windows (PowerShell):**

```yaml
- name: Clone content repository
  powershell:
    script: git clone https://github.com/example/content.git C:\Exhibits\Content
    creates: C:\Exhibits\Content\.git
```

**POSIX (SSH target):**

```yaml
- name: Clone content repository
  shell:
    cmd: git
    args:
      - clone
      - https://github.com/example/content.git
      - /opt/exhibits/content
    creates: /opt/exhibits/content/.git
```

## Pull Latest Commits

Pull does not have a natural idempotency guard, so use `powershell` with a `check_script` to avoid unnecessary network calls. The check script returns `true` (change needed) when the local HEAD differs from the remote:

**Windows:**

```yaml
- name: Pull content repository
  powershell:
    check_script: |
      Set-Location C:\Exhibits\Content
      $local = git rev-parse HEAD
      $remote = git rev-parse '@{u}'
      Write-Output ($local -ne $remote)
    script: |
      Set-Location C:\Exhibits\Content
      git pull --ff-only
```

**POSIX:**

```yaml
- name: Pull content repository
  powershell:
    check_script: |
      cd /opt/exhibits/content
      local=$(git rev-parse HEAD)
      remote=$(git rev-parse '@{u}')
      [ "$local" != "$remote" ] && echo true || echo false
    script: |
      cd /opt/exhibits/content && git pull --ff-only
```

Or use a plain `shell` task when idempotency is not required:

```yaml
- name: Pull content repository
  shell:
    cmd: git
    args: ["-C", "/opt/exhibits/content", "pull", "--ff-only"]
```

## Configure Git Identity

Set user name and email on the target before committing:

```yaml
- name: Configure git identity
  shell:
    cmd: git
    args:
      - config
      - --global
      - user.email
      - deploy@example.com

- name: Configure git name
  shell:
    cmd: git
    args:
      - config
      - --global
      - user.name
      - Preflight Deploy
```

## Credentials

### Stdlib Git Sync Action

Use `preflight/git-sync` when the target should sync a private repository during a playbook run:

```yaml
vars:
  github_token: secret:github-deploy-token

tasks:
  - name: Sync exhibit content
    uses: preflight/git-sync
    with:
      repo: https://github.com/example/private-repo.git
      dest: C:\Exhibits\App
      ref: main
      http_password: "{{ vars.github_token }}"
      clean: true
```

For SSH, pass the private key as a secret and optionally provide known-hosts content:

```yaml
vars:
  deploy_key: secret:github-deploy-key
  github_known_hosts: secret:github-known-hosts

tasks:
  - name: Sync exhibit content
    uses: preflight/git-sync
    with:
      repo: git@github.com:example/private-repo.git
      dest: C:\Exhibits\App
      ref: main
      ssh_private_key: "{{ vars.deploy_key }}"
      ssh_known_hosts: "{{ vars.github_known_hosts }}"
```

In bundle flow, encrypted secrets are included in the staged bundle and decrypted on the target with `preflight apply --bundle <bundle.zip> --secret-identity <identity-file>`.

### HTTPS With A Personal Access Token

Store the access token as a secret and use it in the clone URL:

```yaml
vars:
  git_token: secret:github-deploy-token

tasks:
  - name: Clone private repository
    powershell:
      script: |
        git clone https://x-access-token:{{ vars.git_token }}@github.com/example/private-repo.git C:\Exhibits\App
      creates: C:\Exhibits\App\.git
```

To avoid the token appearing in git's stored remote URL, configure a credential helper after cloning:

```yaml
  - name: Update remote URL without token
    powershell:
      script: |
        Set-Location C:\Exhibits\App
        git remote set-url origin https://github.com/example/private-repo.git
```

### SSH Key Authentication

Place an SSH private key on the target machine and configure the git remote to use SSH:

```yaml
  - name: Write deploy key
    file:
      src: "{{ vars.deploy_key_path }}"
      dest: C:\Users\exhibit\.ssh\id_ed25519
      ensure: present

  - name: Set key permissions
    powershell:
      script: |
        $keyPath = "C:\Users\exhibit\.ssh\id_ed25519"
        icacls $keyPath /inheritance:r /grant:r "${env:USERNAME}:F"
      creates: C:\Users\exhibit\.ssh\id_ed25519
```

Then use the SSH remote URL in your clone task:

```yaml
  - name: Clone via SSH
    become:
      user: exhibit
      password: secret:exhibit-password
      load_profile: true
    powershell:
      script: git clone git@github.com:example/content.git C:\Exhibits\Content
      creates: C:\Exhibits\Content\.git
```

Run the task with `become` so the key is read from the exhibit user's home directory rather than the transport account's.

### Windows Credential Manager

On Windows, git can delegate to the built-in credential manager (`manager` or `manager-core`). Configure it once, then git handles token refresh automatically:

```yaml
  - name: Set git credential helper
    powershell:
      script: git config --global credential.helper manager
```

Preflight cannot pre-populate the credential manager interactively. Use a personal access token for non-interactive automation, or pre-seed the credential store via `cmdkey`:

```yaml
  - name: Store git credentials
    powershell:
      script: |
        $token = "{{ vars.git_token }}"
        cmdkey /generic:git:https://github.com /user:x-access-token /pass:$token
```

## SSH Agent Forwarding

Preflight's SSH target currently does not support agent forwarding. The `private_key` field in the inventory entry authenticates the connection from the Preflight controller to the target — it is not forwarded to the target session for use by git or other tools running on the target.

To authenticate git over SSH on a remote target, configure an SSH key directly on the target machine as shown in [SSH Key Authentication](#ssh-key-authentication) above, rather than relying on a forwarded agent.

## Run Git As A Specific User

When your target machine has a dedicated account for running exhibit software, use `become` so git operations and the resulting files are owned by the correct user:

```yaml
defaults:
  become:
    user: exhibit
    password: secret:exhibit-password
    load_profile: true

tasks:
  - name: Clone content repository
    powershell:
      script: git clone https://github.com/example/content.git C:\Exhibits\Content
      creates: C:\Exhibits\Content\.git

  - name: Pull latest content
    powershell:
      check_script: |
        Set-Location C:\Exhibits\Content
        $local = git rev-parse HEAD
        $remote = git rev-parse '@{u}'
        Write-Output ($local -ne $remote)
      script: |
        Set-Location C:\Exhibits\Content
        git pull --ff-only
```

See [Run tasks as another user](./run-tasks-as-another-user.md) for the full provisioning pattern.

## Related Docs

- [Built-in module reference](../reference/modules.md) — `shell`, `powershell`, `file` modules
- [Run tasks as another user](./run-tasks-as-another-user.md) — using `become` with git tasks
- [Manage secrets](./manage-secrets.md) — storing access tokens
- [Playbook and action YAML reference](../reference/playbooks.md) — `creates:`, `check_script`
