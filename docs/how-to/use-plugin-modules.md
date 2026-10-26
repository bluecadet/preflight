# Use plugin modules in playbooks

Call an executable plugin from a playbook or action task.

## Prerequisites

- A plugin executable named `preflight-plugin-<name>` (`.exe` on Windows) in one of the plugin directories (see [Discovery](../reference/plugins.md#discovery))
- A playbook or action to call it from

The plugin runs on the controller, the machine running `preflight`, and reaches the target through Preflight. It works the same over local, SSH, and WinRM targets.

## 1. Confirm Preflight can start the plugin

From the project root:

```bash
preflight plugin list
preflight plugin info signage_sync
```

`plugin info` starts the plugin and shows `Initialize:  ok` or the startup error. Fix any error before you use the plugin in YAML.

## 2. Call the plugin from a task

Use `module` and `params`:

```yaml
tasks:
  - name: Sync signage content
    module: signage_sync
    params:
      source: "\\\\nas01\\signage"
      destination: "C:\\Signage"
```

This form works in playbooks and actions. Plugins are discovered at runtime, so the YAML schema has no named key for them.

Plugin tasks accept the usual task fields, including `when`, `depends_on`, `ignore_errors`, and `tags`:

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

## 3. Stage plugin tasks into offline bundles

```bash
preflight stage playbooks/lobby.yml
```

Preflight copies each plugin the plan uses into the target's bundle. The target runs that executable during offline apply, so the plugin build must match the target's OS and architecture. Preflight rejects the bundle when the staging controller's OS or architecture differs from the target's. Because targets run Windows, stage plugin bundles on a Windows controller with the target's architecture.

Staging also fails if the plugin cannot start or reports a name other than its filename suffix.

## Troubleshooting

### The module is unknown

The `module:` value must equal the `<name>` in `preflight-plugin-<name>`, and the plugin must be in a plugin directory for the project. Run `preflight plugin list` from the project root to check.

### `plugin "<name>" conflicts with built-in module name`

Built-in module names are reserved. Rename the plugin executable and its reported name.

### `plugin "<name>" discovered more than once`

The same plugin name exists in two plugin directories. Remove one copy.

### The plugin works online but not in a staged bundle

Apply the bundle staged for that target. Offline apply sees only the plugins inside the bundle, and only plugins the plan uses are bundled.

## Related docs

- [Write a plugin](./write-a-plugin.md)
- [Plugin reference](../reference/plugins.md)
- [Stage bundles for air-gapped deployment](./air-gapped-deployment.md)
