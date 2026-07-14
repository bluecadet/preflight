# Sync A Git Repo On A Target

Use this guide when a target needs to clone or update a Git repository
as part of a playbook run — for example, a museum kiosk pulling the
latest exhibit content before it opens for the day. On Windows targets,
the embedded `preflight/git-sync` action wraps the fetch, checkout,
reset, and clean steps in a single idempotent task; on POSIX targets,
run `git` through the `shell` module (see
[Sync On A POSIX Target](#sync-on-a-posix-target)).

## Prerequisites

- A working playbook and a Windows target connection
- `git` installed on the target (use `winget_package` or `package`; see
  the [built-in module reference](../reference/modules.md))
- If the repository is private, credentials configured as described in
  [Configure Git Credentials For A Target](./configure-git-credentials.md)

## 1. Sync A Repository

`preflight/git-sync` clones the repository into `dest` if it does not
exist yet, or fetches and checks out `ref` if it does:

```yaml
tasks:
  - name: Sync exhibit content
    uses: preflight/git-sync
    with:
      repo: https://github.com/example/content.git
      dest: C:\Exhibits\Content
      ref: main
```

Re-running the task leaves an up-to-date working tree at `dest` with
`ref` checked out. Set `create_parent: true` if the parent of `dest`
does not exist yet, and `set_remote_url: true` if `repo` may change
between runs and the existing remote should be updated to match.

## 2. Pin To A Ref

`ref` accepts a branch, tag, or commit. Pin exhibit content to a tag so
that a kiosk never picks up unreviewed commits on `main`:

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

`detach: true` checks out `ref` directly instead of creating or moving
a local branch. Use `local_branch` to name the branch that tracks `ref`
when you do want a branch checked out.

## 3. Reset And Clean The Working Tree

Exhibit content trees accumulate stray files over time: a kiosk
application may write cache files, logs, or scratch state into the
content directory, and a previous sync may leave behind files that no
longer exist upstream. Set `reset` and `clean` so every sync starts
from a pristine copy of `ref`:

```yaml
tasks:
  - name: Sync and reset exhibit content
    uses: preflight/git-sync
    with:
      repo: https://github.com/example/content.git
      dest: C:\Exhibits\Content
      ref: main
      reset: true
      clean: true
      clean_ignored: true
```

`reset: true` hard-resets the working tree to the resolved `ref`,
discarding local modifications. `clean: true` removes untracked files
after checkout; add `clean_ignored: true` to also remove files that
match `.gitignore` patterns, such as cached thumbnails an exhibit
viewer generates locally.

Set `fetch: true` (the default when the repository already exists) to
fetch remote updates before resolving `ref`, and `prune: true` alongside
it to drop local references to branches deleted upstream. Use `depth`
to limit fetch/clone to a shallow history when the full commit history
of an exhibit content repository is not needed.

## 4. Run As The Exhibit Account

By default, `preflight/git-sync` runs as whichever account executes
the task. If exhibit content should be owned by a dedicated kiosk
account rather than the transport account, add `become` to the task.
See [Run Tasks As Another User](./run-tasks-as-another-user.md) for the
full pattern; `preflight/git-sync` does not need any special handling
beyond the standard `become` fields.

## 5. Understand Safe Directory Handling

`preflight/git-sync` adds `dest` to Git's global `safe.directory` list
before running sync checks, because `safe_directory` defaults to
`true`. This avoids Git for Windows "dubious ownership" failures when
the checkout is owned by a different Windows account than the one
running the sync — a common situation when provisioning runs as an
administrator but the exhibit app runs under a kiosk account. Set
`safe_directory: false` only if your environment already manages the
`safe.directory` list another way.

## Sync On A POSIX Target

`preflight/git-sync` targets Windows. On a POSIX target over SSH, run
`git` directly with the `shell` module, using `creates:` to make the
clone idempotent:

```yaml
tasks:
  - name: Clone content repository
    shell:
      cmd: git
      args:
        - clone
        - https://github.com/example/content.git
        - /opt/exhibits/content
      creates: /opt/exhibits/content/.git

  - name: Pull latest content
    shell:
      cmd: git
      args: ["-C", "/opt/exhibits/content", "pull", "--ff-only"]
```

The pull task runs on every apply; that is a stated tradeoff of the
manual pattern, not a bug. Wrap it in `when:` or a `powershell`
`check_script` if the extra network call matters.

## Troubleshooting

**Sync fails with a "dubious ownership" error even though
`safe_directory` is enabled.** Confirm you did not explicitly set
`safe_directory: false` on the task, and that `dest` matches the exact
path git reports as unsafe — a trailing slash or a different casing of
a drive letter can produce a path git treats as distinct.

**`reset`/`clean` removed content the kiosk generated at runtime.**
This is expected: `reset` and `clean` return the working tree to
exactly what `ref` contains upstream. Store any state the kiosk
application needs to keep across syncs outside of `dest`, such as in a
sibling directory the action does not manage.

**Task reports success but the target is still on the old commit.**
Check that `fetch` is not disabled and that `ref` resolves to the
commit you expect on the remote; `preflight/git-sync` only fetches
remote updates when `fetch` is enabled for an existing repository.

## Related Docs

- [Configure Git Credentials For A Target](./configure-git-credentials.md) —
  authenticating `preflight/git-sync` against private repositories
- [Embedded Stdlib Action Reference](../reference/stdlib-actions.md#preflightgit-sync) —
  full `preflight/git-sync` input table
- [Run Tasks As Another User](./run-tasks-as-another-user.md) — using
  `become` so synced content is owned by the right account
- [Built-in module reference](../reference/modules.md) — `winget_package`,
  `package` for installing `git` itself
