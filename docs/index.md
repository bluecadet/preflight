# Preflight Docs

Preflight is a Windows-first configuration management CLI for exhibit PCs. The docs set is organized with Diataxis so readers can move quickly between learning, task execution, exact lookup, and architectural reasoning.

## Start Here

| If you want to... | Read this |
| --- | --- |
| Get a first project working end to end | [Quickstart](./tutorials/quickstart.md) |
| Install the CLI safely | [Install Preflight](./how-to/install-preflight.md) |
| Run, dry-run, or inspect a playbook | [Run a playbook](./how-to/run-a-playbook.md) |
| Run against inventory-backed hosts | [Run a playbook against remote hosts](./how-to/remote-execution.md) |
| Validate a Windows host from a Mac before adding it to inventory | [Validate a WinRM connection from macOS](./how-to/validate-winrm-from-macos.md) |
| Choose a deployment pattern for locked-down networks | [Deploy across restricted networks](./explanation/restricted-network-deployment.md) |
| Stage bundles for offline rollout | [Stage bundles for air-gapped deployment](./how-to/air-gapped-deployment.md) |
| Manage encrypted repo-backed secrets | [Manage secrets](./how-to/manage-secrets.md) |
| Use external plugin modules | [Use plugin modules in playbooks](./how-to/use-plugin-modules.md) |
| Reuse tasks as a custom action | [Write an action](./how-to/write-an-action.md) |
| Browse the embedded `preflight/` action library | [Embedded stdlib action reference](./reference/stdlib-actions.md) |
| Compare desired state to recorded state | [Inspect state and diffs](./how-to/inspect-state-and-diff.md) |
| Provision a user and run tasks as that user | [Run tasks as another user](./how-to/run-tasks-as-another-user.md) |
| Clone and update git repositories on targets | [Run git operations on a target](./how-to/git-operations.md) |

## Tutorials

- [Quickstart](./tutorials/quickstart.md): create a minimal project, validate it, plan it, dry-run it, and apply it.

## How-To Guides

- [Install Preflight](./how-to/install-preflight.md)
- [Run a playbook](./how-to/run-a-playbook.md)
- [Run a playbook against remote hosts](./how-to/remote-execution.md)
- [Validate a WinRM connection from macOS](./how-to/validate-winrm-from-macos.md)
- [Manage secrets](./how-to/manage-secrets.md)
- [Stage bundles for air-gapped deployment](./how-to/air-gapped-deployment.md)
- [Use plugin modules in playbooks](./how-to/use-plugin-modules.md)
- [Write an action](./how-to/write-an-action.md)
- [Inspect state and diffs](./how-to/inspect-state-and-diff.md)
- [Run tasks as another user](./how-to/run-tasks-as-another-user.md)
- [Run git operations on a target](./how-to/git-operations.md)

## Reference

- [CLI reference](./reference/cli.md)
- [Project config reference](./reference/config.md)
- [Inventory reference](./reference/inventory.md)
- [Playbook and action YAML reference](./reference/yaml.md)
- [Embedded stdlib action reference](./reference/stdlib-actions.md)
- [Built-in module reference](./reference/modules.md)
- [Templating and facts reference](./reference/templating.md)
- [Plugin reference](./reference/plugins.md)
- [Bundle reference](./reference/bundles.md)
- [State reference](./reference/state.md)

## Explanation

- [Architecture](./explanation/architecture.md)
- [Why use Preflight (and when not to)](./explanation/why-preflight.md)
- [Execution model](./explanation/execution-model.md)
- [Actions, stdlib, and lockfiles](./explanation/actions-and-lockfiles.md)
- [Deploy across restricted networks](./explanation/restricted-network-deployment.md)
- [Targets, transports, and plugins](./explanation/targets-and-transports.md)
- [Secrets and `age`](./explanation/secrets-and-age.md)
- [How `become` works](./explanation/become.md)

## Core Ideas

Preflight is built around three layers:

- **Modules** are the executable primitives. Built-ins are compiled into the binary, and plugins can add more.
- **Actions** are reusable YAML bundles of tasks with typed inputs.
- **Playbooks** are the per-machine or per-environment declarations you actually run.

Execution flows through four explicit phases:

```text
Fetch -> Plan -> Stage -> Apply
```

- **Fetch** acquires remote action refs into the cache and records their pinned SHAs in `preflight.lock`.
- **Plan** loads playbooks, merges imports, resolves actions, expands tasks, and builds a DAG without contacting targets.
- **Stage** creates a per-target offline bundle that includes the staged plan/task DAG, manifest, referenced plugins, and any bundled secrets needed for offline apply.
- **Apply** gathers facts, renders execution-time templates against the live execution context (including `when`, task names, and params), runs `Check()` first for every task, and only calls `Apply()` when change is required.

## Current Scope

The codebase already supports:

- Local execution and inventory-backed host selection
- WinRM and SSH target transports
- Embedded, local, cached, and Git-backed action resolution
- Embedded Windows baseline stdlib actions for machine, shell, input, quiet mode, updates, power, and apps
- Repo-backed `age` secrets
- Staged bundle creation and bundle apply
- Structured output in `text`, `tui`, and `json`, including streamed task output during apply
- Plugin discovery plus plugin-backed module execution

Important current limits:

- SSH auto-detects a Windows PowerShell or POSIX shell runtime. Windows-over-SSH supports the built-in Windows module set; POSIX-over-SSH stays focused on `directory`, `file`, `shell`, `wait` (`file_exists`, `port_open`), and `powershell` when installed. Plugin modules are not yet supported over SSH.
