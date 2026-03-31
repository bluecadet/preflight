# Preflight

Preflight is a Windows-first configuration management CLI for exhibit PCs in museum and gallery environments. It is built around a single Go binary, idempotent modules, and a pipeline that separates planning from execution.

## In This Docs Set

| If you want to... | Read this |
| --- | --- |
| Get your first playbook running locally | [Quickstart](./tutorials/quickstart.md) |
| Install the CLI | [Install Preflight](./how-to/install-preflight.md) |
| Apply or dry-run a playbook | [Run a playbook](./how-to/run-a-playbook.md) |
| Manage repo-backed secrets | [Manage secrets](./how-to/manage-secrets.md) |
| Understand `age` and why secrets are encrypted | [Secrets and age](./explanation/secrets-and-age.md) |
| Look up commands and flags | [CLI reference](./reference/cli.md) |
| Look up YAML file shapes | [YAML reference](./reference/yaml.md) |
| Understand how Preflight is structured | [Architecture](./explanation/architecture.md) |

> [!IMPORTANT]
> These docs are written against the current repository state. A few concepts in `README.md` are ahead of the implementation, so this docs set calls out planned behavior separately from what works today.

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
- **Stage** is intended for offline artifact bundling.
- **Apply** executes tasks against a `Target`.

## What Works Today

- Local playbook parsing, planning, and execution
- Embedded and local action resolution
- Repo-backed `age` secrets
- Inventory parsing and listing
- Plugin discovery
- Structured output in `text`, `json`, and `jsonl`

## Still Planned Or Stubbed

- Remote action fetch into cache
- Stage bundle assembly
- Remote target execution from CLI commands
- Windows-native module registration beyond the currently implemented cross-platform set

## Start Here

The fastest path is the [Quickstart](./tutorials/quickstart.md), then the [CLI reference](./reference/cli.md) for day-to-day lookup.
