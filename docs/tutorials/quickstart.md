# Quickstart

This tutorial gets a minimal Preflight project running in one sitting. You will create a project, validate it, inspect the plan, dry-run it, apply it, and confirm idempotency.

## Before You Start

You need:

- A `preflight` binary installed and on your `PATH`
- A terminal in an empty working directory

If you still need the CLI, follow [Install Preflight](../how-to/install-preflight.md).

Confirm the binary is available:

```bash
preflight --version
```

## 1. Create A Project Directory

```bash
mkdir -p preflight-quickstart/playbooks
cd preflight-quickstart
```

Create `preflight.yml`:

```yaml
project: docs-demo
environment: development

vars:
  demo_dir: "./tmp/demo"
  demo_file: "./tmp/demo/hello.txt"
```

`preflight.yml` is the project-level config file. In this example it only carries shared variables, but the same file also holds repo-backed secret settings when you need them later.

## 2. Create A Playbook

Create `playbooks/quickstart.yml`:

```yaml
name: quickstart
description: Minimal local run

tasks:
  - name: Create demo directory
    directory:
      path: "{{ vars.demo_dir }}"
      ensure: present

  - name: Write demo file
    shell:
      cmd: /bin/sh
      args:
        - -c
        - printf 'Hello from Preflight\n' > "{{ vars.demo_file }}"
      creates: "{{ vars.demo_file }}"
```

This playbook uses two built-in modules:

- `directory` ensures the directory exists.
- `shell` runs a command, but only when `creates` is missing, which makes the task naturally idempotent.

## 3. Validate The Input Files

Run:

```bash
preflight validate playbooks/quickstart.yml
```

Expected result:

```text
OK
```

Why this matters:

- It proves the YAML parses.
- It catches direct `uses:` resolution errors before you contact any targets.
- It is the fastest way to catch structural mistakes early.

## 4. Inspect The Plan

Run:

```bash
preflight plan playbooks/quickstart.yml
```

You should see a flattened task list. `plan` is intentionally pure: it expands tasks and renders whatever it can from known variables, but it does not gather facts or mutate the machine.

## 5. Dry-Run The Changes

Run:

```bash
preflight check playbooks/quickstart.yml
```

This drives every task through the `Check()` side of the module contract. Nothing is changed, but you get the same planning, templating, dependency ordering, and task filtering behavior that a real run would use.

## 6. Apply The Playbook

Run:

```bash
preflight apply playbooks/quickstart.yml
```

Inspect the results:

```bash
cat ./tmp/demo/hello.txt
preflight state show
```

At this point you have both a concrete machine change and a persisted state snapshot under `state/provision.json`.

## 7. Run It Again

Run the same command a second time:

```bash
preflight apply playbooks/quickstart.yml
```

The second run should report fewer or no changes. That is the core promise of Preflight: modules report whether work is needed first, then apply only when necessary.

## What You Learned

You now have a working mental model for the normal flow:

1. Define shared project configuration in `preflight.yml`.
2. Describe desired state in a playbook.
3. Use `validate` to catch structural issues.
4. Use `plan` to inspect what will run.
5. Use `check` to dry-run real execution logic.
6. Use `apply` to converge the machine and record state.

## Next Step

Move on to [Run a playbook](../how-to/run-a-playbook.md) for everyday execution patterns, or jump to [Playbook and action YAML reference](../reference/playbooks.md) when you need exact field names and task shapes.
