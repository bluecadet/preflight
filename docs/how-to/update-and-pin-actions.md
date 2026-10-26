# Update and pin remote actions

Fetch a remote action into the local cache, pin it in `preflight.lock`, move it to a newer revision, and make every controller resolve the same commit.

## Prerequisites

- A playbook task that uses a remote action ref in `host/org/repo[/path/to/action]@revision` form (see [Remote action refs](../reference/playbooks.md#remote-action-refs))
- HTTPS access to the Git host for the first fetch of each revision. The controller does not need a `git` binary.
- A shell in the project root, the directory containing `preflight.yml`. `preflight action fetch` reads and writes `preflight.lock` in the current directory.

## 1. Reference a remote action

```yaml
tasks:
  - name: Apply signage configuration
    uses: github.com/acme/actions/signage@v1
    with:
      display_name: lobby-01
```

The part after `@` is a tag, branch, or commit SHA. For how remote refs fit with stdlib and local actions, see [Execution model](../explanation/execution-model.md#pinned-remote-refs).

## 2. Fetch and pin the action

```bash
preflight action fetch github.com/acme/actions/signage@v1
```

Preflight prints the resolved commit for each ref. A fetch also follows remote `uses:` refs inside the action, so you may see several lines:

```text
Fetched github.com/acme/actions/signage@v1 -> 0123456789abcdef0123456789abcdef01234567
```

The action directory, including files next to `action.yml`, goes into the user cache:

```text
~/.preflight/actions/github.com/acme/actions/signage@0123456789abcdef0123456789abcdef01234567/
```

`preflight.lock` gains an entry keyed by the ref you fetched:

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
> `apply`, `check`, and `stage` fetch any missing remote refs before they
> run. `validate` and `plan` resolve only from the existing cache and
> lockfile, and fail on a ref that has not been fetched.

## 3. Inspect the resolved action

```bash
preflight action info github.com/acme/actions/signage@v1
```

The output lists the action's name, version, description, author, inputs with required and default markers, and task names. It resolves through the same cache and lockfile as a playbook run.

## 4. Update a pinned ref

Once a ref is in `preflight.lock`, fetching the same ref again reuses the pinned commit without checking the remote.

To move to a new version:

1. Change the ref in the playbook, for example from `@v1` to `@v2`.
2. Fetch the new ref:

   ```bash
   preflight action fetch github.com/acme/actions/signage@v2
   ```

   Preflight adds a `@v2` entry to `preflight.lock` and leaves the `@v1` entry in place.

To re-resolve a branch whose tip moved upstream:

1. Delete the ref's key from the `actions` object in `preflight.lock`.
2. Fetch the ref again:

   ```bash
   preflight action fetch github.com/acme/actions/signage@main
   ```

   With no lock entry to reuse, Preflight resolves the ref against the remote and writes a new entry.

## 5. Commit the lockfile

```bash
git add preflight.lock
git commit -m "Pin signage action to v2"
```

Every controller that checks out this commit resolves the same pinned commit, fetching it once if its cache lacks it. Without a committed lockfile, two controllers fetching `@main` at different times can pin different commits.

## Troubleshooting

| Error | Fix |
|---|---|
| `remote action "..." is not cached; run 'preflight action fetch ...'` | The ref was never fetched on this controller, or `~/.preflight/actions` was cleared. Run `preflight action fetch <ref>`. |
| `remote ref "..." must include @<revision>` | `action fetch` accepts only Git-backed refs with `@revision`, not local (`myorg/x`) or stdlib (`preflight/x`) refs. |
| `remote ref "..." must start with a hostname` | The first path segment needs a dot, as in `github.com/acme/actions/signage@v1`. |
| `git resolver: action "..." does not contain action.yml` | The `path/to/action` part of the ref names a directory without `action.yml`. Check it against the repository layout. |
| `git resolver: fetch "...": ...` | The repository or revision does not exist, or the controller cannot reach it over HTTPS. |

> [!NOTE]
> If the cache is cleared and you fetch a tag or branch again, Preflight
> resolves that ref against the remote, not the SHA in `preflight.lock`.
> If the tag or branch moved upstream, the new pin points to its current
> commit.

## Related docs

- [Execution model](../explanation/execution-model.md)
- [Write an action](./write-an-action.md)
- [Stage bundles for air-gapped deployment](./air-gapped-deployment.md)
- [Playbook and action YAML reference](../reference/playbooks.md)
