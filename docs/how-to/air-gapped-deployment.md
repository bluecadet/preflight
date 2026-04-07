# Stage Bundles For Air-Gapped Deployment

Use this guide when you want to prepare a run on one machine, transfer the result to an isolated environment, and apply it there without re-reading the original playbook or re-fetching actions.

## Prerequisites

- A working `preflight` binary on the staging machine
- A playbook that already plans or applies successfully in a connected environment
- Any referenced plugin executables discoverable during staging
- No tasks that would require embedding decrypted secret values into the bundle

## 1. Stage The Bundles

Run:

```bash
preflight stage playbooks/lobby.yml
```

By default bundles are written under `dist/bundles/`.

Choose another output directory if needed:

```bash
preflight stage playbooks/lobby.yml --bundle-output-dir ./out/bundles
```

Preflight creates one bundle per resolved target, not one site-wide archive.

## 2. Understand What The Bundle Contains

Each bundle is a zip archive that contains:

- `manifest.json`
- `plan.json`
- the runtime binary under `runtime/`
- any referenced plugin executables under `plugins/`

The manifest records:

- playbook name
- target name
- target OS and architecture
- build metadata for the staging binary
- referenced modules
- checksums
- lockfile entries for fetched remote actions

This design keeps staged execution reproducible. The offline machine runs the exact task DAG and module structure that was staged. Expressions in `when`, task name templates, and parameters that reference `facts`, `env`, or `target.*` values are rendered at apply time against the target.

## 3. Transfer The Correct Bundle

Copy the target-specific zip to the isolated machine using your normal transfer method.

If you staged more than one target, make sure each machine receives its own bundle. The plan inside the bundle is already target-specific.

## 4. Apply The Bundle Offline

Run:

```bash
preflight apply --bundle dist/bundles/<bundle>.zip
```

Choose a custom state file if you want:

```bash
preflight apply --bundle ./bundle.zip --state-file ./state/offline.json
```

Bundle apply reads `plan.json` directly from the archive, extracts the payload to a temporary directory, builds a module registry from the bundled plugins, and then executes the plan locally.

## 5. Know The Important Limits

Staging fails when:

- a task would require embedding a decrypted secret value
- the plan references an unknown module
- a referenced plugin cannot be initialized or copied into the bundle

Offline apply does not:

- re-fetch actions
- re-read the source playbook
- rediscover plugins from your normal global plugin directories

That isolation is a feature. It keeps the staged artifact self-contained and predictable.

## Troubleshooting

### Staging fails because of secrets

The runner refuses to embed secret values into a staged bundle. Move decryption to the offline machine, or refactor the playbook so the staged run does not depend on those secret-bearing parameters.

### A plugin works normally but not when staged

Confirm the plugin is discoverable before staging:

```bash
preflight plugin list
```

Only plugins actually referenced by the staged plan are copied into the bundle.

### I am not sure which bundle belongs to which host

The filename includes the playbook name, target name, target OS, and target architecture. The same values also appear in `manifest.json`.
