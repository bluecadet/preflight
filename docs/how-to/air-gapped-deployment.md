# Stage Bundles For Air-Gapped Deployment

Use this guide when you want to prepare a run on one machine, transfer the result to an isolated environment, and apply it there without re-reading the original playbook or re-fetching actions.

## Prerequisites

- A working `preflight` binary on the staging machine
- A working `preflight` binary on each offline target that will run `preflight apply --bundle`
- A playbook that passes `preflight validate`
- Any referenced plugin executables discoverable during staging and built for
  the destination OS and architecture
- No tasks that would require embedding decrypted secret values into the bundle

## 1. Prepare Encrypted Secrets For Target-Side Apply

Skip this section if the staged plan does not reference `secret:<name>` values.

If the staged plan references secrets, the bundle includes the encrypted secret files. The offline machine still needs an identity that can decrypt those files when it runs `preflight apply --bundle`.

Use this pattern when the target should decrypt the bundle locally:

1. Keep or create a developer identity on the staging machine:

   ```bash
   preflight secret identity generate --out .age/keys.txt
   ```

2. Generate a separate identity on the target machine:

   ```bash
   preflight secret identity generate --out .age/target-keys.txt
   ```

3. On the target, print the target's public recipient:

   ```bash
   preflight secret identity recipient .age/target-keys.txt
   ```

4. Copy only the public recipient string back to the project and add it beside the developer recipient in `preflight.yml`:

   ```yaml
   secrets:
     identity: ".age/keys.txt"
     recipients:
       - "age1developerrecipient..."
       - "age1targetrecipient..."
     entries:
       autologin-password:
         file: "secrets/autologin-password.age"
   ```

5. Re-encrypt the configured secrets to the updated recipient list:

   ```bash
   preflight secret rekey
   ```

Adding a recipient to `preflight.yml` does not change existing encrypted files by itself. Run `preflight secret rekey` after recipient changes so the staged encrypted secret payloads can be decrypted by the new target identity.

## 2. Stage The Bundles

Declare each offline host's destination platform in `preflight.yml`. The
transport may describe how the host is normally managed; staging does not
connect through it when `platform` is present:

```yaml
inventory:
  hosts:
    - name: lobby-pc-01
      address: 192.168.1.10
      transport: winrm
      platform:
        os: windows
        arch: amd64
```

Run:

```bash
preflight stage playbooks/lobby.yml --target lobby-pc-01
```

Preflight validates the plan for `windows-powershell` and creates the bundle
without connecting to `lobby-pc-01`. This allows a macOS or Linux controller
to stage Windows playbooks for an unreachable host. If `platform` is omitted,
Preflight connects to the selected host to discover its OS and architecture.

By default bundles are written under `dist/bundles/`.

Choose another output directory if needed:

```bash
preflight stage playbooks/lobby.yml --bundle-output-dir ./out/bundles
```

Preflight creates one bundle per resolved target, not one site-wide archive.

## 3. Understand What The Bundle Contains

Each bundle is a zip archive that contains:

- `manifest.json`
- `plan.json`
- any referenced plugin executables under `plugins/`
- any bundled secret payloads under `secrets/` when the staged plan references `secret:<name>` values

The manifest records:

- playbook name
- target name
- target OS and architecture
- build metadata for the staging binary
- referenced modules
- checksums
- lockfile entries for fetched remote actions

This design keeps staged execution reproducible. The offline machine runs the exact task DAG and module structure that was staged. Expressions in `when`, task name templates, and parameters that reference `facts`, `env`, or `target.*` values are rendered at apply time against the target.

## 4. Transfer The Correct Bundle

Copy the target-specific zip to the isolated machine using your normal transfer method.

If you staged more than one target, make sure each machine receives its own bundle. The plan inside the bundle is already target-specific.

## 5. Apply The Bundle Offline

Run:

```bash
preflight apply --bundle dist/bundles/<bundle>.zip
```

Choose a custom state file if you want:

```bash
preflight apply --bundle ./bundle.zip --state-file ./state/offline.json
```

If the bundle includes encrypted secrets, pass the target identity:

```bash
preflight apply --bundle ./bundle.zip --secret-identity .age/target-keys.txt
```

Bundle apply reads `plan.json` directly from the archive, extracts the payload to a temporary directory, builds a module registry from the bundled plugins, and then executes the plan locally with the installed `preflight` binary on that machine.

## 6. Know The Important Limits

Staging fails when:

- a task would require embedding a decrypted secret value
- the plan references an unknown module
- a referenced plugin cannot be initialized, reports the wrong logical name,
  or cannot be copied into the bundle
- a referenced plugin is being staged for an OS or architecture different
  from the controller

Offline apply does not:

- re-fetch actions
- re-read the source playbook
- rediscover plugins from your normal global plugin directories

That isolation is a feature. It keeps the staged artifact predictable and independent from the original project checkout.

## Troubleshooting

### Staging fails because of secrets

The runner refuses to embed secret values into a staged bundle. Use encrypted bundle secrets with a target identity, or refactor the playbook so the staged run does not depend on those secret-bearing parameters.

### A plugin works normally but not when staged

Confirm the plugin is discoverable before staging:

```bash
preflight plugin list
```

Only plugins actually referenced by the staged plan are copied into the bundle, and each referenced plugin is initialized during staging to verify that its reported logical name matches the module name used by the plan.

Cross-platform staging currently supports built-in modules only. A plugin
bundle must be staged on the same OS and architecture as the destination
because plugin discovery provides the controller-native executable.

### I am not sure which bundle belongs to which host

The filename includes the playbook name, target name, target OS, and target architecture. The same values also appear in `manifest.json`.
