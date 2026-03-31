# Use Plugin Modules In Playbooks

Use this guide when you want to run an external plugin as part of a playbook or action.

## Prerequisites

- A plugin executable named `preflight-plugin-<name>` available in a discovered plugin directory
- A playbook or action that should call the plugin

See [CLI reference](../reference/cli.md) for the `plugin` commands and [Plugin Reference](../reference/plugins.md) for the execution contract.

## Check That Preflight Can See The Plugin

List discovered plugins:

```bash
preflight plugin list
```

Inspect one plugin:

```bash
preflight plugin info signage_sync
```

If initialization fails here, fix that first before wiring the plugin into a playbook.

## Call The Plugin From A Task

Use the explicit `module` and `params` task form:

```yaml
tasks:
  - name: Sync signage content
    module: signage_sync
    params:
      source: "\\\\nas01\\signage"
      destination: "C:\\Signage"
```

This works in both `playbook.yml` and `action.yml` tasks.

## Use Normal Task Controls

Plugin-backed tasks support the same task-level controls as built-in modules:

- `when`
- `depends_on`
- `ignore_errors`
- `tags`

Example:

```yaml
tasks:
  - name: Sync signage content
    module: signage_sync
    params:
      source: "\\\\nas01\\signage"
      destination: "C:\\Signage"
    when: "{{ vars.sync_enabled }}"
    tags: ["content"]
```

## Stage Plugin Tasks Into Offline Bundles

If a staged plan references a plugin task, Preflight includes that plugin executable in the per-target bundle automatically.

```bash
preflight stage playbooks/lobby.yml
```

This only succeeds when the plugin can be initialized during staging.

## Troubleshooting

### The task says the module is unknown

Check the plugin name reported by:

```bash
preflight plugin info <name>
```

Use that logical name in `module:`.

### The plugin name conflicts with a built-in module

Rename the plugin. Built-in module names are reserved and cannot be shadowed by plugins.

### The plugin works locally but not in a staged bundle

Make sure the plugin is referenced by the plan you staged and that the staged bundle is the one generated for that target.
