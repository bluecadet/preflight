# Deploy across restricted networks

On a secure network, the first question is which side may open the connection. Preflight's remote transports are controller-initiated: the machine running `preflight` opens SSH or WinRM to each target. Preflight has no resident agent, callback transport, or tunnel manager.

Several deployment patterns still work. The choice depends on whether you can run Preflight inside the secure network, whether targets can open outbound SSH connections, and whether you can run a staged bundle on each machine.

## Start with connection direction

- If the controller can open an approved connection to the target, use normal remote execution.
- If the target can dial out but cannot accept inbound management traffic, an externally managed reverse SSH tunnel can expose an SSH endpoint for Preflight.
- If no live remote session is acceptable, stage a bundle and run it on the target.

## Run Preflight inside the secure network

In many locked-down environments, moving the controller is simpler than opening a new path through the firewall. Run Preflight from:

- a jump host
- an admin workstation inside the target VLAN
- a VPN-connected machine that can reach the private address space

Inventory entries then use internal hostnames or addresses, and Preflight needs no tunnel-specific configuration. Use SSH by default, or WinRM where SSH is not available. SSH supports every built-in module; a few cannot finish over WinRM (see [WinRM session limitations](../reference/modules.md#winrm-session-limitations)).

An SSH-reachable bastion that cannot run Preflight itself still works through a [jump host](./targets-and-transports.md#bastion--jump-hosts) entry.

## Use reverse SSH tunnels when targets can dial out

When a target may open an outbound SSH connection to a bastion but the controller cannot reach the target directly, a reverse tunnel bridges the gap.

Preflight does not create or maintain the tunnel. Another tool or an operating-system service keeps it alive. Preflight just connects to a reachable SSH host and port:

```text
target --outbound ssh -R--> bastion
operator running preflight ----ssh----> bastion:forwarded-port -> target:22
```

The inventory entry points at the forwarded endpoint instead of the target's private address:

```yaml
- name: signage-host-01
  address: bastion.example.org
  transport: ssh
  port: 2201
  username: exhibit
  private_key: secret:signage-key
```

The target must provide Windows PowerShell over SSH. Every built-in and plugin module then works through the tunnel.

For hardening a shared bastion, scoping one key per target so machines cannot reach each other's tunnels, and opening tunnels only on demand, see [Set up a reverse-tunnel bastion](../how-to/set-up-a-tunnel-bastion.md) and [Onboard a target through a reverse-tunnel bastion](../how-to/onboard-a-target-through-a-bastion.md).

## Stage bundles when no inbound path exists

When the target network allows no inbound administration at all, use staged bundles. `preflight stage` renders a target-specific plan into a zip archive, and `preflight apply --bundle` runs it locally on the target with no SSH or WinRM connection.

An inventory host can declare its `platform`, so a controller can stage the bundle without contacting the host. The [inventory reference](../reference/inventory.md#platform-fields) defines that field.

Bundles fit when:

- you can move files into the environment through an approved channel
- a person, MDM or RMM tool, scheduled task, or remote-support session can launch the bundle on the target

Bundles are often easier to approve in secure environments: the execution artifact is explicit, and the target needs no listening management service.

Tradeoffs:

- each bundle is specific to one target
- you need a way to transfer and launch it
- secret references travel encrypted, so the target needs an `age` identity that matches a project recipient
- staging fails if a task carries a plaintext secret value; `--allow-plaintext-secrets-in-bundle` overrides that but writes every referenced secret into the bundle as plaintext
- a bundle that includes plugins must be staged on a controller with the target's OS and architecture

## Choosing a pattern

| Pattern | Best when | Strengths | Limits |
| --- | --- | --- | --- |
| Run Preflight inside the secure network | A controller or bastion can reach the Windows hosts | Normal remote model with every module | Requires a trusted machine inside the environment or another approved path |
| Reverse-tunneled SSH | Targets may open outbound SSH, but nothing can reach them | Every module without inbound access | Requires PowerShell over SSH and an externally managed tunnel |
| Staged bundles applied locally | No live management path can be approved | No remote transport during apply | Requires transferring and launching a bundle on each target |

Place the controller inside the network boundary when you can. Use reverse SSH tunnels when targets can only dial out. When no controller-initiated connection is acceptable, stage bundles.

## Related docs

- [Run a playbook against remote hosts](../how-to/remote-execution.md)
- [Stage bundles for air-gapped deployment](../how-to/air-gapped-deployment.md)
- [Set up a reverse-tunnel bastion](../how-to/set-up-a-tunnel-bastion.md)
- [Onboard a target through a reverse-tunnel bastion](../how-to/onboard-a-target-through-a-bastion.md)
- [Inventory reference](../reference/inventory.md)
- [Targets, transports, and plugins](./targets-and-transports.md)
