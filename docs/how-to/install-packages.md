# Install packages with a package manager

Install software on a Windows target with WinGet, Chocolatey, or Scoop, including targets where the package manager isn't installed yet.

## Prerequisites

- A playbook and a Windows target connection
- An administrator transport account
- Internet access from the target

## 1. Bootstrap WinGet

Add a `preflight/winget` task before any `winget_package` task:

```yaml
tasks:
  - name: WinGet
    uses: preflight/winget
```

The action provisions App Installer and the `winget` source machine-wide, then registers them for the transport account. On a target that already has App Installer provisioned machine-wide and a current `winget` source, its tasks report `ok`. A target with a user-installed but unprovisioned WinGet still gets provisioned on the first run, and the first run may also provision the `winget` source. See [`preflight/winget`](../reference/stdlib-actions.md#preflightwinget) for what each task checks.

Without the bootstrap, `winget_package` fails on Windows IoT Enterprise LTSC and for accounts that have never signed in interactively. The error names the task to add.

## 2. Install packages

```yaml
  - name: Tools
    winget_package:
      packages:
        - id: 7zip.7zip
        - id: Microsoft.VisualStudioCode
          version: "1.85.0"
```

`scope` defaults to `machine`. `scope: user` installs for the transport account, not for another user. See [`winget_package`](../reference/modules.md#winget_package) for every field.

## 3. Keep the source fresh

The `winget` source is a package index. The bootstrap re-provisions it once it is older than `source_max_age`, so new package versions become visible:

```yaml
  - name: WinGet
    uses: preflight/winget
    with:
      source_max_age: 7d
```

Set `source_max_age: "0"` to provision the source once and never refresh it.

## Use Chocolatey instead

Bootstrap Chocolatey with `preflight/chocolatey`, then manage packages with `choco_package`:

```yaml
tasks:
  - name: Chocolatey
    uses: preflight/chocolatey
  - name: Tools
    choco_package:
      packages:
        - id: 7zip
        - id: jq
          version: "1.7.1"
```

Chocolatey installs machine-wide for every account. To install from an internal feed, list it in the bootstrap's `sources` and set `source` on the package entry. See [`preflight/chocolatey`](../reference/stdlib-actions.md#preflightchocolatey) and [`choco_package`](../reference/modules.md#choco_package).

## Use Scoop instead

Scoop installs apps per account. Bootstrap it with `preflight/scoop`, then manage apps with `scoop_package`:

```yaml
tasks:
  - name: Scoop
    uses: preflight/scoop
    with:
      buckets: [versions]
  - name: Tools
    scoop_package:
      packages:
        - id: jq
        - id: versions/python310
```

Apps go to the transport account's profile. To keep two versions of one app, see [`scoop_package`](../reference/modules.md#scoop_package).

## Related docs

- [Built-in module reference](../reference/modules.md)
- [Stdlib action reference](../reference/stdlib-actions.md)
