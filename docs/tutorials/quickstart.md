# Quickstart

In this tutorial you create a minimal Preflight project, then validate, plan, dry-run, and apply it on your own machine. A second apply shows that a converged machine needs no changes.

## Before you start

You need:

- A Windows machine with `preflight` on its `PATH`. Without an inventory, Preflight runs against the local machine, and the local target requires Windows.
- A PowerShell window in an empty working directory

On a macOS or Linux controller, skip to [Manage your first Windows target](./first-windows-target.md), which applies a playbook to a remote Windows host.

If you still need the CLI, follow [Install Preflight](../how-to/install-preflight.md).

Confirm the binary runs:

```powershell
preflight --version
```

## 1. Create a project

```powershell
mkdir preflight-quickstart\playbooks
cd preflight-quickstart
```

Create `preflight.yml`:

```yaml
project: docs-demo
environment: development

vars:
  demo_dir: 'C:\PreflightQuickstart'
  demo_file: 'C:\PreflightQuickstart\hello.txt'
```

`preflight.yml` is the project config file. Here it holds shared variables. Later it can also hold an inventory and secret settings.

## 2. Create a playbook

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
    powershell:
      script: Set-Content -Path '{{ vars.demo_file }}' -Value 'Hello from Preflight'
      creates: "{{ vars.demo_file }}"
```

The playbook uses two built-in modules:

- `directory` creates the directory if it is missing.
- `powershell` runs the script only when the `creates` path does not exist, so the task is idempotent.

## 3. Validate the playbook

```powershell
preflight validate playbooks/quickstart.yml
```

Preflight reports the playbook name, its task count, and any `uses:` references it resolved. Validation parses the YAML and resolves action references without contacting a target.

## 4. Inspect the plan

```powershell
preflight plan playbooks/quickstart.yml
```

You see the flattened task list. `plan` does not gather facts or change the machine.

## 5. Dry-run the changes

```powershell
preflight check playbooks/quickstart.yml
```

`check` calls each module's `Check()` and reports the directory and file as pending changes. Nothing changes yet.

## 6. Apply the playbook

```powershell
preflight apply playbooks/quickstart.yml
Get-Content C:\PreflightQuickstart\hello.txt
```

The file contains `Hello from Preflight`.

## 7. Run it again

```powershell
preflight apply playbooks/quickstart.yml
```

This time both tasks report no change. Each module checks first and applies only when the machine differs from the playbook.

## Next steps

Continue with [Manage your first Windows target](./first-windows-target.md) to add an inventory, encrypt a credential, and apply a playbook over SSH. For everyday execution options, see [Run a playbook](../how-to/run-a-playbook.md).
