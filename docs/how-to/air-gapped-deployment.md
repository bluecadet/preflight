# Stage Bundles For Air-Gapped Deployment

Use this guide when you want to prepare a playbook on one machine, transfer the result to an isolated environment, and apply it there without re-resolving actions or plugins.

## Prerequisites

- A working `preflight` checkout or installed binary on the staging machine
- A playbook that already runs successfully in a connected environment
- Any plugin executables required by the playbook available through normal plugin discovery
- No tasks that require persisting decrypted secret values into the staged bundle

## Create The Bundles

Run `stage` from the project directory:

```bash
preflight stage playbooks/lobby.yml
```

By default Preflight writes bundles under `dist/bundles/`.

You can choose another output directory:

```bash
preflight stage playbooks/lobby.yml --bundle-output-dir ./out/bundles
```

## Understand What Gets Written

Preflight writes one zip per resolved target. Each bundle includes:

- a rendered execution plan for one target
- a bundle manifest with playbook, target, version, module, checksum, and lockfile metadata
- the Preflight runtime binary used for staging
- any plugin executables referenced by the plan

Bundle filenames include the playbook name, target name, target OS, and target architecture.

## Transfer A Bundle

Copy the target-specific zip to the offline machine using your normal transfer process.

If you staged more than one target, make sure each machine receives its own bundle.

## Apply The Bundle Offline

Run:

```bash
preflight apply --bundle dist/bundles/lobby-baseline-localhost-linux-amd64.zip
```

You can still choose a custom state file:

```bash
preflight apply --bundle ./bundle.zip --state-file ./state/offline.json
```

## Know The Current Limits

Bundle staging fails when:

- a task would require embedding decrypted secret values in the bundle
- a referenced plugin module does not have a compatible executable available during staging
- the plan references a module that cannot be resolved at staging time

Offline apply executes the bundled plan directly. It does not fetch actions, re-read the playbook, or rediscover staged plugins from your normal global plugin directories.

## Troubleshooting

### Staging fails because of secrets

Refactor the playbook so the isolated machine can resolve secrets locally at apply time, or remove the secret-dependent task from the staged run.

### Staging fails because of a plugin

Confirm the plugin executable is discoverable before running `stage`:

```bash
preflight plugin list
preflight plugin info <name>
```

### You are not sure which bundle belongs to which machine

Use the bundle filename and the target name in the manifest metadata. Preflight stages one bundle per resolved target, not one site-wide archive.
