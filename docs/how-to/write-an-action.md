# Write An Action

Use this guide when you want to package a reusable task sequence behind named inputs.

## When To Reach For An Action

Write an action when you have a task sequence that should be:

- reused across several playbooks
- parameterized with inputs
- shared inside a project or fetched remotely later

If the logic is only needed once, keep it inline in the playbook instead.

## 1. Create The Action Directory

Project-local actions live under `actions/`:

```text
actions/
  myorg/
    display-config/
      action.yml
```

The resulting ref is:

```text
myorg/display-config
```

## 2. Define `action.yml`

Example:

```yaml
name: myorg/display-config
version: "1.0.0"
description: Prepare a content directory and drop a marker file

inputs:
  content_root:
    type: path
    required: true
    description: Target content directory
  label:
    type: string
    default: default
    description: Marker text

tasks:
  - name: Ensure content directory exists
    directory:
      path: "{{ vars.content_root }}"
      ensure: present

  - name: Write content marker
    shell:
      cmd: /bin/sh
      args:
        - -c
        - printf '%s\n' "{{ vars.label }}" > "{{ vars.content_root }}/marker.txt"
      creates: "{{ vars.content_root }}/marker.txt"
```

Important details:

- `name` should match the ref you plan to use.
- `inputs` define the external API of the action.
- Inside the action, inputs become template variables under `vars.*`.
- Tasks inside an action use the same task schema as playbooks.

## 3. Call The Action From A Playbook

```yaml
tasks:
  - name: Prepare lobby content
    uses: myorg/display-config
    with:
      content_root: "./tmp/lobby"
      label: lobby
```

During planning, Preflight resolves the action, renders the `with:` values, applies any input defaults, verifies required inputs, and expands the action’s tasks into the final execution plan.

## 4. Inspect The Action

Use the built-in inspection commands:

```bash
preflight action list
preflight action info myorg/display-config
```

`action info` is the fastest way to confirm the action name, inputs, outputs, and task count are what you expect.

## 5. Validate The Calling Playbook

```bash
preflight validate playbooks/lobby.yml
preflight plan playbooks/lobby.yml
```

That verifies both the playbook and the action ref, then shows the expanded tasks after action resolution.

## Notes

- Resolution order is embedded stdlib, then local `actions/`, then the user cache, then Git-backed refs.
- The embedded stdlib in this repo currently ships `preflight/autologin`.
- Remote actions can be fetched and pinned into `preflight.lock`, but you do not need that machinery for project-local actions.
