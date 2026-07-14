# Preflight Docs

Preflight is a Windows-first configuration management CLI for dedicated
systems such as kiosks, signage, and exhibit PCs, with POSIX hosts
supported over SSH. You run `preflight` on a **controller** (your
workstation or a CI machine) against **hosts** defined in a project's
inventory.

Three reader paths below cover most visits; the full catalog follows.

## New To Preflight

Work through these in order:

1. [Install Preflight](./how-to/install-preflight.md)
2. [Quickstart](./tutorials/quickstart.md) — a minimal project,
   validated, planned, dry-run, and applied
3. [Run a playbook](./how-to/run-a-playbook.md) — the
   validate → plan → check → apply loop you will use every day
4. [Run a playbook against remote hosts](./how-to/remote-execution.md) —
   inventory, host selection, and fan-out

Then read [Why use Preflight (and when not to)](./explanation/why-preflight.md)
and [Architecture](./explanation/architecture.md) to decide how it fits
your fleet.

## Operating A Fleet

- [Troubleshoot remote connections](./how-to/troubleshoot-remote-connections.md)
- [Enable remote access on a Windows target](./how-to/enable-remote-access.md)
- [Manage secrets](./how-to/manage-secrets.md)
- [Run tasks as another user](./how-to/run-tasks-as-another-user.md)
- [Sync a git repo on a target](./how-to/sync-a-git-repo.md) and
  [configure git credentials](./how-to/configure-git-credentials.md)
- [Schedule a Windows reboot](./how-to/schedule-a-windows-reboot.md)
- [Inspect state and diffs](./how-to/inspect-state-and-diff.md)
- [Update and pin remote actions](./how-to/update-and-pin-actions.md)
- Locked-down networks:
  [choose a deployment pattern](./explanation/restricted-network-deployment.md),
  [stage bundles for air-gapped deployment](./how-to/air-gapped-deployment.md),
  or [set up a reverse-tunnel bastion](./how-to/set-up-a-tunnel-bastion.md)
  and [onboard targets through it](./how-to/onboard-a-target-through-a-bastion.md)

## Extending Preflight

- [Write an action](./how-to/write-an-action.md) — package reusable task
  sequences with typed inputs
- [Write a plugin](./how-to/write-a-plugin.md) — add a new module as a
  standalone executable
- [Use plugin modules in playbooks](./how-to/use-plugin-modules.md)
- Contributing to Preflight itself: [CONTRIBUTING](../CONTRIBUTING.md),
  plus the integration-test guides under
  [docs/development](./development/winrm-integration-testing.md)

## Reference

- [CLI reference](./reference/cli.md)
- [Project config reference](./reference/config.md)
- [Inventory reference](./reference/inventory.md)
- [Playbook and action YAML reference](./reference/playbooks.md)
- [Built-in module reference](./reference/modules.md)
- [Embedded stdlib action reference](./reference/stdlib-actions.md)
- [Templating and facts reference](./reference/templating.md)
- [Error reference](./reference/errors.md)
- [Plugin reference](./reference/plugins.md)
- [Bundle reference](./reference/bundles.md)
- [State reference](./reference/state.md)

## Explanation

- [Why use Preflight (and when not to)](./explanation/why-preflight.md) —
  scope, strengths, and current limits
- [Architecture](./explanation/architecture.md) — the
  modules → actions → playbooks layering and where each piece lives
- [Execution model](./explanation/execution-model.md) — the
  Fetch/Plan/Stage/Apply phases and why planning stays pure
- [Targets, transports, and plugins](./explanation/targets-and-transports.md)
- [How `become` works](./explanation/become.md)
- [Secrets and `age`](./explanation/secrets-and-age.md)
- [Deploy across restricted networks](./explanation/restricted-network-deployment.md)

## Development

Contributor-facing guides for working on Preflight itself:

- [Run the Windows/WinRM integration suite](./development/winrm-integration-testing.md)
- [Run the POSIX/SSH integration suite](./development/posix-integration-testing.md)
