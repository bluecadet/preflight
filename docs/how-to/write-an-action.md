# Write an action

Package a task sequence behind named inputs so several playbooks can reuse it. Keep logic that only one playbook needs inline in that playbook.

## 1. Create the action directory

Project-local actions live under `actions/` in the project root:

```text
actions/
  myorg/
    display-config/
      action.yml
```

The directory path under `actions/` is the ref: `myorg/display-config`.

## 2. Define `action.yml`

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
    file:
      dest: "{{ vars.content_root }}\\marker.txt"
      content: "{{ vars.label }}"
```

- Set `name` to the ref.
- `inputs` declares the values callers pass. Each has a `type` (`string`, `bool`, `int`, `path`, or `list`), and can be `required` or have a `default`.
- Tasks read inputs as `vars.<input>`.
- Tasks use the same schema as playbook tasks.

## 3. Call the action from a playbook

```yaml
tasks:
  - name: Prepare lobby content
    uses: myorg/display-config
    with:
      content_root: "C:\\Exhibits\\Lobby"
      label: lobby
```

During planning, Preflight renders the `with:` values, applies defaults, rejects missing required inputs, and expands the action's tasks into the plan.

## 4. Inspect the action

```bash
preflight action list
preflight action info myorg/display-config
```

`action info` shows the action's name, inputs, and task names.

## 5. Validate the calling playbook

```bash
preflight validate playbooks/lobby.yml
preflight plan playbooks/lobby.yml
```

`validate` checks the playbook and the action ref. `plan` shows the expanded tasks.

## Notes

- Preflight resolves refs from the embedded stdlib (`preflight/...`) first, then local `actions/`, then the user cache, then Git-backed refs.
- To share an action across projects, publish it in a Git repository and reference it remotely. See [Update and pin remote actions](./update-and-pin-actions.md).
