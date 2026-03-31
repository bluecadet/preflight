# Preflight

Preflight is a Windows-first configuration management CLI for exhibit PCs in museum and gallery environments. It is built around a single Go binary, idempotent modules, and a pipeline that separates planning from execution.

## In This Docs Set

| If you want to... | Read this |
| --- | --- |
| Get your first playbook running locally | [Quickstart](./tutorials/quickstart.md) |
| Install the CLI | [Install Preflight](./how-to/install-preflight.md) |
| Apply or dry-run a playbook | [Run a playbook](./how-to/run-a-playbook.md) |
| Run against inventory-backed hosts | [Run a playbook against remote hosts](./how-to/remote-execution.md) |
| Manage repo-backed secrets | [Manage secrets](./how-to/manage-secrets.md) |
| Understand `age` and why secrets are encrypted | [Secrets and age](./explanation/secrets-and-age.md) |
| Look up commands and flags | [CLI reference](./reference/cli.md) |
| Look up YAML file shapes | [YAML reference](./reference/yaml.md) |
| Understand how Preflight is structured | [Architecture](./explanation/architecture.md) |

## Core Ideas

Preflight uses three layers:

- **Modules** are built-in primitives implemented in Go.
- **Actions** are reusable YAML bundles of tasks.
- **Playbooks** are top-level declarations describing what to run.

Execution flows through four phases:

```text
Plan -> Fetch -> Stage -> Apply
```

- **Plan** expands actions, resolves templates, and builds the task graph.
- **Fetch** is intended for remote action retrieval.
- **Stage** assembles offline artifact bundles.
- **Apply** executes tasks against a `Target`.

## What Works Today

- Local and inventory-backed playbook parsing, planning, and execution
- Embedded and local action resolution
- Repo-backed `age` secrets
- Inventory parsing, selection, and listing
- Plugin discovery
- Plugin execution through module tasks
- Structured output in `text`, `json`, and `jsonl`
- WinRM and SSH target transports

## Still Planned Or Stubbed

- Broader transport parity across every module and platform combination

## Start Here

The fastest path is the [Quickstart](./tutorials/quickstart.md), then [Run a playbook](./how-to/run-a-playbook.md) for local usage or [Run a playbook against remote hosts](./how-to/remote-execution.md) for inventory-backed execution.
