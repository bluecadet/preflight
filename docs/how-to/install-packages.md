# Install packages with a package manager

Install software on a Windows target with WinGet, including editions and accounts where WinGet isn't ready yet.

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

## Related docs

- [Built-in module reference](../reference/modules.md)
- [Stdlib action reference](../reference/stdlib-actions.md)
