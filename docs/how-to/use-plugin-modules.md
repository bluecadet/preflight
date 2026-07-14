# Use Plugin Modules In Playbooks

Use this guide when you want a playbook or action to call an external executable plugin.

## Prerequisites

- A plugin executable named `preflight-plugin-<name>` available in a discovered plugin directory
- A playbook or action that should call the plugin

For the protocol details, see [Plugin reference](../reference/plugins.md).

## How Plugins Run

Plugins execute **controller-side**: the plugin process runs on the machine running `preflight`, not on the target. A plugin's `Check` and `Apply` receive a target handle and all target effects flow through it — including when the target is the local machine. A plugin therefore works the same way over local, SSH, and WinRM; you do not pick a transport-specific plugin path. (For the protocol details, see [Plugin reference](../reference/plugins.md).)

## 1. Confirm Discovery And Initialization

List discovered plugins:

```bash
preflight plugin list
```

Inspect one plugin:

```bash
preflight plugin info signage_sync
```

Fix any initialization error here before you wire the plugin into YAML. Preflight discovers plugin executables lazily during normal startup, but `plugin list`, `plugin info`, staging, and first runtime use still initialize the plugin and will fail if startup is broken or the reported logical name does not match the discovered module name.

## 2. Call The Plugin From A Task

Use the explicit `module` plus `params` task form:

```yaml
tasks:
  - name: Sync signage content
    module: signage_sync
    params:
      source: "\\\\nas01\\signage"
      destination: "C:\\Signage"
```

This works in both `playbook.yml` and `action.yml`.

Why the explicit form matters:

- Plugin modules are discovered at runtime, so they are not part of the static YAML schema.
- `module: <name>` keeps plugin invocation aligned with the same `Check()` then `Apply()` contract used by built-ins.

## 3. Use Normal Task Controls

Plugin-backed tasks still support the normal task-level controls:

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

## 4. Stage Plugin Tasks Into Offline Bundles

If a staged plan references a plugin task, Preflight includes that plugin executable in the target-specific bundle automatically.

```bash
preflight stage playbooks/lobby.yml
```

Staging only succeeds if the plugin can be initialized, reports the expected logical name, and can be copied during the stage step.

The staging controller and declared or discovered destination must have the
same OS and architecture for plugin bundles. Cross-platform staging currently
supports built-in modules only; stage a plugin bundle on a matching platform.

## Troubleshooting

### The task says the module is unknown

Use the logical name reported by:

```bash
preflight plugin info <name>
```

That logical name must match the filename suffix, and it is what belongs in `module:`.

### The plugin conflicts with a built-in module

Rename the plugin. Built-in names are reserved.

### The plugin works locally but not in a staged bundle

Make sure the staged plan actually references the plugin task and that you are applying the bundle generated for that target. Offline apply only sees the plugin files embedded in the bundle.

Also compare the staging controller with the bundle destination. Preflight
does not select a foreign plugin build: it rejects the bundle when their OS or
architecture differs.
