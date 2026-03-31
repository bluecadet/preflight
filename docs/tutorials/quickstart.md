# Quickstart

This tutorial walks you through a minimal local Preflight project so you can validate, plan, and apply a playbook in one sitting.

> [!NOTE]
> This path uses the current local execution flow. Inventory-backed remote execution is part of the design, but the CLI currently builds a local target for playbook runs.

## Before You Start

You need:

- A `preflight` binary installed
- A terminal in a working directory where you want to create a project

If you have not installed it yet, follow [Install Preflight](../how-to/install-preflight.md).

Confirm the CLI is available:

```bash
preflight --version
```

## 1. Create A Project Config

Create a new project directory and add `preflight.yml`:

```bash
mkdir -p preflight-quickstart/playbooks
cd preflight-quickstart
```

```yaml
project: docs-demo
environment: development

vars:
  demo_dir: "./tmp/demo"
  demo_file: "./tmp/demo/hello.txt"
```

This file provides project-wide variables that are available to playbooks.

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

  - name: Write demo file from shell
    shell:
      cmd: /bin/sh
      args:
        - -c
        - echo "Hello from Preflight" > "{{ vars.demo_file }}"
      creates: "{{ vars.demo_file }}"
```

This uses two inline modules:

- `directory` to ensure the folder exists
- `shell` to create a file only if it is missing

## 3. Validate The Playbook

Run:

```bash
preflight validate playbooks/quickstart.yml
```

Expected result:

```text
OK
```

`validate` parses the playbook and resolves any `uses:` references without executing tasks.

## 4. Inspect The Plan

Run:

```bash
preflight plan playbooks/quickstart.yml
```

You should see the playbook name and the flattened task list.

## 5. Dry-Run The Changes

Run:

```bash
preflight check playbooks/quickstart.yml
```

This exercises each module's `Check()` path without applying changes.

## 6. Apply The Playbook

Run:

```bash
preflight apply playbooks/quickstart.yml
```

After the run completes, inspect the results:

```bash
cat ./tmp/demo/hello.txt
preflight state show
```

## 7. Re-Run To Confirm Idempotency

Run the same command again:

```bash
preflight apply playbooks/quickstart.yml
```

The second run should report fewer or no changes because the desired state already exists.

## What You Learned

You created:

- A project config in `preflight.yml`
- A playbook with inline modules
- A local run using `validate`, `plan`, `check`, and `apply`

## Next Step

Move on to [Run a playbook](../how-to/run-a-playbook.md) for common execution patterns, or use the [YAML reference](../reference/yaml.md) when you need exact field names.
