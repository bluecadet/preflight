# Update And Pin Remote Actions

Use this guide when a playbook references a remote action with `uses:`
(for example `github.com/acme/actions/signage@v1`) and you need to fetch
it into the local cache, understand what `preflight.lock` records for
it, deliberately move it to a newer revision, and keep every controller
that runs the playbook resolving the exact same commit.

## Prerequisites

- A playbook task that already uses (or will use) a remote action ref in
  `host/org/repo[/path/to/action]@revision` form. See
  [Remote Action Refs](../reference/playbooks.md#remote-action-refs) for
  the exact shape.
- Network access to the remote Git host for the first fetch of a given
  revision. Preflight fetches over plain HTTPS clone URLs; no `git`
  binary is required on the controller.
- A shell in your project root — the directory that holds `preflight.yml`
  and the playbook. `preflight action fetch` reads and writes
  `preflight.lock` relative to the current working directory, so run it
  from the same place you run `apply`/`plan`/`validate`.

## 1. Reference A Remote Action

Add the `uses:` reference to a task:

```yaml
tasks:
  - name: Apply signage configuration
    uses: github.com/acme/actions/signage@v1
    with:
      display_name: lobby-01
```

The ref before `@` is `host/org/repo[/path/to/action]`; the part after
`@` is a tag, branch, or commit SHA. See
[Actions, stdlib, and lockfiles](../explanation/actions-and-lockfiles.md#how-remote-refs-work)
for why this shape exists and how it fits alongside stdlib and local
actions.

## 2. Fetch And Pin It

Fetch the ref into the local cache and record it in `preflight.lock`:

```bash
preflight action fetch github.com/acme/actions/signage@v1
```

On success Preflight prints the resolved commit for each ref it fetched
(a fetch recurses through any nested remote `uses:` the action itself
declares, so you may see more than one line):

```text
Fetched github.com/acme/actions/signage@v1 -> 0123456789abcdef0123456789abcdef01234567
```

That downloads the action (and any sibling files next to `action.yml`)
into the user-wide cache at:

```text
~/.preflight/actions/github.com/acme/actions/signage@0123456789abcdef0123456789abcdef01234567/
```

and adds an entry to `preflight.lock` in the project root, keyed by the
literal ref you fetched:

```json
{
  "actions": {
    "github.com/acme/actions/signage@v1": {
      "ref": "github.com/acme/actions/signage@v1",
      "sha": "0123456789abcdef0123456789abcdef01234567",
      "pinned": "github.com/acme/actions/signage@0123456789abcdef0123456789abcdef01234567"
    }
  }
}
```

> [!NOTE]
> `preflight apply`, `preflight check`, and `preflight stage` also fetch
> any remote refs a playbook needs that are not yet in `preflight.lock`,
> before they run. `preflight validate` and `preflight plan` do not —
> they only resolve from the existing cache and lockfile, and fail if a
> ref isn't there yet. Run `action fetch` (or one prior `apply`/`check`/
> `stage`) before relying on `validate`/`plan` against a fresh cache.

## 3. Inspect What Resolved

Confirm the resolved action's identity, inputs, and tasks:

```bash
preflight action info github.com/acme/actions/signage@v1
```

This prints the action's name, version, description, author, its input
definitions (with required/default markers), and the task names it
expands to. It resolves through the same cache and lockfile as a
playbook run, so it fails the same way a missing fetch would.

## 4. Update A Pinned Ref

Preflight has no `action update` subcommand and `action fetch` has no
force flag. Once a ref is in `preflight.lock`, fetching that exact same
ref string again reuses the pinned commit already recorded — it does not
re-check the remote for a newer commit. There are two supported ways to
move forward, depending on what "update" means for you:

**Move to a new version.** Change the ref in the playbook to a new tag
or SHA (for example `@v1` to `@v2`), then fetch it:

```bash
preflight action fetch github.com/acme/actions/signage@v2
```

Because the ref string is different, this adds a new entry to
`preflight.lock` alongside the old one; it does not touch the `@v1`
entry.

**Force re-resolution of the same ref** (for example, a floating branch
ref whose tip moved upstream). Remove that ref's entry from the
`actions` object in `preflight.lock` — delete the key by hand, or delete
the whole file if it only contains that one entry — then fetch the same
ref again:

```bash
preflight action fetch github.com/acme/actions/signage@main
```

With no lockfile entry to reuse, the fetch re-resolves the ref against
the remote and writes a fresh pinned entry.

## 5. Commit The Lockfile

Commit `preflight.lock` alongside the playbook change:

```bash
git add preflight.lock
git commit -m "Pin signage action to v2"
```

Every controller that checks out this commit and runs `apply`/`check`/
`stage` resolves the same pinned commit from its own local cache (or
fetches it once, identically, if its cache doesn't have it yet). Without
a committed lockfile, two controllers fetching a floating ref like
`@main` at different times could pin to different commits.

## Troubleshooting

| Symptom | Cause / fix |
|---|---|
| `remote action "..." is not cached; run 'preflight action fetch ...'` | The ref has never been fetched on this controller, or `~/.preflight/actions` was cleared while `preflight.lock` still references it. Run `preflight action fetch <ref>` to (re)populate the cache. |
| `remote ref "..." must include @<revision>` | You passed `action fetch` a ref with no `@revision`, or a local/embedded ref (`myorg/x`, `preflight/x`). `action fetch` only handles Git-backed remote refs. |
| `remote ref "..." must start with a hostname` | The ref's first path segment has no dot (for example `acme/actions/signage@v1` instead of `github.com/acme/actions/signage@v1`). |
| `git resolver: action "..." does not contain action.yml` | The `path/to/action` portion of the ref points at a directory in the repository that has no `action.yml`. Check the path segment against the actual repository layout. |
| `git resolver: fetch "...": ...` (clone error) | The repository, tag, branch, or SHA doesn't exist or isn't reachable over HTTPS from this controller. Confirm the ref and network access. |

> [!NOTE]
> If your cache was wiped and you re-fetch using a floating ref (a tag
> or branch, not a SHA) rather than the SHA `preflight.lock` already
> recorded, Preflight re-resolves that floating ref against the remote
> right then. For an immutable release tag this reproduces the same
> commit. If the tag or branch was moved upstream in the meantime, the
> re-fetch pins whatever it currently points to, not the original SHA.

## Related Docs

- [Actions, stdlib, and lockfiles](../explanation/actions-and-lockfiles.md)
- [Write an action](./write-an-action.md)
- [Stage bundles for air-gapped deployment](./air-gapped-deployment.md)
- [Playbook and action YAML reference](../reference/playbooks.md)
