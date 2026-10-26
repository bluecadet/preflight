# Sync a Git repo on a target

Clone or update a Git repository on a target with the `preflight/git-sync` action, for example to pull exhibit content onto a kiosk before it opens.

## Prerequisites

- A playbook and a Windows target connection
- `git` installed on the target (use `winget_package` or `package`; see the [module reference](../reference/modules.md))
- For a private repository, credentials as described in [Configure Git credentials for a target](./configure-git-credentials.md)

## 1. Sync a repository

```yaml
tasks:
  - name: Sync exhibit content
    uses: preflight/git-sync
    with:
      repo: https://github.com/example/content.git
      dest: C:\Exhibits\Content
      ref: main
```

If `dest` has no repository, the action clones `repo` into it, creating the parent directory. Otherwise it fetches, checks out `ref`, and hard-resets the working tree to it. It also points the remote at `repo` if the URL changed.

## 2. Pin to a ref

`ref` accepts a branch, tag, or commit. Pin to a tag so the kiosk never picks up unreviewed commits:

```yaml
tasks:
  - name: Sync exhibit content at a known-good tag
    uses: preflight/git-sync
    with:
      repo: https://github.com/example/content.git
      dest: C:\Exhibits\Content
      ref: v2026.03.01
      detach: true
```

`detach: true` checks out `ref` without a local branch. To check it out on a named branch instead, set `local_branch`.

## 3. Clean the working tree

Kiosk apps can leave cache files and logs in the content directory. By default the action discards changes to tracked files but keeps untracked ones. To remove untracked files too, set `clean`:

```yaml
tasks:
  - name: Sync and clean exhibit content
    uses: preflight/git-sync
    with:
      repo: https://github.com/example/content.git
      dest: C:\Exhibits\Content
      ref: main
      clean: true
      clean_ignored: true
```

`clean_ignored: true` also removes files matched by `.gitignore`, such as generated thumbnails.

Other defaults you can change:

- `reset: false` keeps local changes to tracked files.
- `fetch: false` skips fetching from the remote for an existing repository.
- `prune: false` keeps local refs to branches deleted upstream.
- `depth: <n>` limits clone and fetch to `n` commits of history.

## 4. Choose the account that owns the checkout

The action runs as the account your transport authenticates with, and that account owns the checkout. To sync as a kiosk account, run the sync from a `scheduled_task` with `run_as`. See [Run a command as another user](./configure-another-users-settings.md#run-a-command-as-another-user).

The action adds `dest` to the global Git `safe.directory` list (`safe_directory: true` by default). This prevents Git for Windows "dubious ownership" errors when one account provisions the checkout and another runs the exhibit app. Set `safe_directory: false` only if you manage `safe.directory` another way.

## Troubleshooting

### "Dubious ownership" error with `safe_directory` enabled

Confirm the task does not set `safe_directory: false`, and that `dest` matches the path Git reports. A trailing slash or different drive-letter case makes Git treat the paths as distinct.

### The sync removed files the kiosk wrote

`reset` and `clean` return the working tree to the contents of `ref`. Store runtime state outside `dest`.

### The task succeeds but the target stays on the old commit

Check that `fetch` is not `false` and that `ref` resolves to the commit you expect on the remote.

## Related docs

- [Configure Git credentials for a target](./configure-git-credentials.md)
- [`preflight/git-sync` input reference](../reference/stdlib-actions.md#preflightgit-sync)
- [Configure another user's settings](./configure-another-users-settings.md)
