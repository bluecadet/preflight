# Deploy Across Restricted Networks

When managed Windows hosts live on secure networks, the first question is not "WinRM or SSH?" It is "who is allowed to initiate the connection?" Preflight's remote transports are controller-initiated today: the machine running `preflight` opens WinRM or SSH to each target. Preflight does not currently include a resident agent, a callback transport, or a built-in tunnel manager.

That still leaves several workable deployment patterns. The right choice depends on whether you can run Preflight from inside the secure network, whether the targets can open outbound SSH connections, and whether you can execute a staged bundle locally on each machine.

## Start With Connection Direction

- If the machine running Preflight can open an approved connection to the target, use normal remote execution.
- If the target can dial out but cannot accept inbound management traffic, an externally managed reverse SSH tunnel can expose an SSH endpoint that Preflight can use.
- If no live remote session is acceptable, stage a bundle and run it locally on the target.

## Run Preflight From Inside The Secure Network

In many locked-down environments, the simplest answer is to move the controller instead of forcing a new path through the firewall. That usually means running Preflight from:

- a jump host
- an admin workstation already inside the target VLAN
- a VPN-connected machine that can reach the private addressing space

This works especially well for Windows fleets because it keeps the normal WinRM model intact. Inventory entries can use the internal hostnames or addresses, and Preflight does not need any tunnel-specific awareness.

## Use WinRM When The Controller Can Reach Windows Hosts

WinRM is the right transport when you need the full Windows-native module surface, including:

- `registry`
- `service`
- `user`
- `scheduled_task`
- `windows_feature`
- PowerShell-heavy configuration work

WinRM is a strong fit when:

- the controller already sits inside the secure network
- a bastion host can run Preflight on your behalf
- an approved VPN or network overlay lets the controller reach the WinRM endpoint

The key tradeoff is that WinRM is not itself a reverse-connection strategy. If the target cannot accept controller-initiated traffic, WinRM alone does not solve the access problem. You need some other approved network path to the WinRM listener, or you need a different deployment pattern such as staged bundles.

## Use Reverse SSH Tunnels When Targets Can Dial Out

Reverse SSH tunneling can help when the target is allowed to make an outbound SSH connection to a bastion, but the controller cannot connect to the target directly.

Preflight does not create or maintain that tunnel. Another tool or operating-system service has to keep the reverse tunnel alive. From Preflight's perspective, it is simply connecting to a reachable SSH host and port.

Typical shape:

```text
target --outbound ssh -R--> bastion
operator running preflight ----ssh----> bastion:forwarded-port -> target:22
```

That means your inventory entry points at the forwarded endpoint rather than the target's private address:

```yaml
- name: signage-host-01
  address: bastion.example.org
  transport: ssh
  port: 2201
  username: exhibit
  private_key: secret:signage-key
```

This pattern is useful when outbound SSH is approved and the target fits one of the supported SSH runtimes:

- Windows hosts with a usable PowerShell runtime over SSH can use the built-in Windows module set.
- POSIX hosts can use the narrower POSIX-over-SSH surface: `directory`, `file`, `shell`, `wait` (`file_exists`, `port_open`), and `powershell` when installed.

That is still the important limit. Reverse SSH tunneling can make SSH reachable, but it does not add plugin-module execution and it does not expand POSIX SSH into the full Windows management surface. If the environment cannot provide the right SSH runtime, use WinRM from a reachable controller or switch to bundle-based local execution.

For the operational detail this section only sketches — hardening a shared bastion, scoping one key per target so machines can't reach each other's tunnels, and the manual-trigger model that keeps a tunnel from outliving the person who opened it — see [Set up a reverse-tunnel bastion](../how-to/set-up-a-tunnel-bastion.md) and [Onboard a target through a reverse-tunnel bastion](../how-to/onboard-a-target-through-a-bastion.md).

## Stage Bundles When No Inbound Management Path Is Available

Staged bundles are usually the most robust answer when the target network cannot allow inbound administration at all. `preflight stage` renders a target-specific plan into a zip archive. `preflight apply --bundle` then runs locally on the target, so the apply step no longer depends on WinRM or SSH.

Inventory can declare the destination platform, allowing a controller on a
different OS to stage the bundle without contacting the host. The
[inventory reference](../reference/inventory.md#platform-fields) owns that
configuration contract.

This is a good fit when:

- you can transfer files into the environment through an approved channel
- a person, MDM/RMM tool, scheduled task, or remote-support session can launch the bundle locally
- you still need the full Windows module set

This model is often easier to approve in secure environments because the execution artifact is explicit and the target does not need a permanently reachable management listener.

The main tradeoffs are operational:

- each bundle is target-specific
- you need a path to transfer and launch it
- staging fails if the run would require embedding decrypted secret values

## Choose The Pattern That Matches The Constraint

| Pattern | Best when | Strengths | Limits |
| --- | --- | --- | --- |
| Run Preflight inside the secure network with WinRM | A controller or bastion can reach Windows hosts | Full Windows module support with the normal remote model | Requires a trusted machine inside the environment or another approved path to WinRM |
| Direct SSH or reverse-tunneled SSH | Outbound SSH is allowed and the target matches a supported SSH runtime | Useful for Windows built-ins over PowerShell or portable tasks over POSIX without direct inbound access | Plugin modules are not yet supported over SSH, and POSIX-over-SSH remains a narrower runtime |
| Stage bundles and apply locally | No live inbound management path can be approved | No remote transport during apply and full local module behavior | Requires bundle transfer plus local execution on each target |

## Practical Recommendation

For managed Windows hosts, use WinRM when you can place the controller on the right side of the network boundary. Use reverse SSH only as a narrow operator-managed escape hatch for SSH-friendly tasks. When the environment cannot support controller-initiated administration, staged bundles are usually the clearest and most supportable option.

## Related Docs

- [Run a playbook against remote hosts](../how-to/remote-execution.md)
- [Stage bundles for air-gapped deployment](../how-to/air-gapped-deployment.md)
- [Set up a reverse-tunnel bastion](../how-to/set-up-a-tunnel-bastion.md)
- [Onboard a target through a reverse-tunnel bastion](../how-to/onboard-a-target-through-a-bastion.md)
- [Inventory reference](../reference/inventory.md)
- [Targets, transports, and plugins](./targets-and-transports.md)
