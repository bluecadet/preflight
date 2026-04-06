# Actions, Stdlib, And Lockfiles

Actions are where Preflight stops being just a collection of modules and becomes a reusable configuration system.

## Why Actions Exist

Modules are intentionally small. They are the primitive operations. Actions sit one level above them so operators can:

- package repeatable task sequences
- give those sequences named inputs
- share them across playbooks
- pin remote behavior when reproducibility matters

Without actions, every playbook would have to duplicate its own orchestration logic.

## Resolver Order Is A Policy Choice

Action resolution follows a fixed order:

1. embedded stdlib
2. local project `actions/`
3. user cache `~/.preflight/actions`
4. Git-backed remote refs

That order encodes a philosophy:

- the binary always has a dependable baseline
- project-local work stays easy to author and test
- cached remote content can be reused offline
- remote Git is available without becoming the only source of truth

## Why The Stdlib Is Embedded

The embedded stdlib is versioned with the binary. That means users can rely on `preflight/...` actions without setting up a registry or fetching anything first.

The cost is that stdlib actions do not have independent versioning. When users need a separately pinned action lifecycle, they should use a remote ref instead.

The current stdlib in this repository ships:

- `preflight/autologin`
- `preflight/computer-name`
- `preflight/timezone`
- `preflight/windows-machine`
- `preflight/windows-shell`
- `preflight/windows-input`
- `preflight/windows-quiet-mode`
- `preflight/windows-update-lockdown`
- `preflight/windows-power`
- `preflight/windows-apps`

## How Remote Refs Work

Remote refs use this shape:

```text
host/org/repo[/path/to/action]@revision
```

Examples:

```text
github.com/acme/actions/signage@v1.2.3
github.com/acme/actions/collections/autologin@0123456789abcdef
```

The resolver can fetch the repository, locate the action path, cache it locally, and pin the reference to the exact commit SHA.

Once fetched, later `plan` runs resolve that remote content from the cache plus `preflight.lock` instead of mutating dependency state again.

## Why `preflight.lock` Exists

Floating refs such as tags or branches are convenient to write but bad for reproducibility. `preflight.lock` solves that by recording the exact commit SHA used for each fetched remote ref.

That gives the project two important properties:

- repeatable future resolution
- a clear record of what remote content was actually used

Nested remote dependencies are fetched recursively and contribute lock entries as well.

## Why Cache And Lockfile Resolution Work Together

The Git resolver does not blindly hit the network every time. It uses the cache plus the lockfile to make remote actions behave more like pinned local dependencies.

That matters for:

- disconnected planning
- consistent staging
- controlled upgrades

The system is deliberately closer to a lockfile-based dependency model than to a "latest by default" package lookup.
