# Onboard a target through a reverse-tunnel bastion

Add a Windows target on a private network to a reverse-tunnel bastion, then manage it with Preflight through that bastion. Each target gets its own key, which the bastion limits to one loopback port.

## Prerequisites

- A bastion with `admin` and `tunnel` users ([Set up a reverse-tunnel bastion](./set-up-a-tunnel-bastion.md))
- OpenSSH Server enabled on the target ([Enable remote access on a Windows target](./enable-remote-access.md))
- Console access to the target, such as VNC. Someone on site starts the tunnel by hand each time.
- A list of targets and their assigned bastion ports. Every target needs a unique port.

## 1. Generate a tunnel key on the target

On the target, in an elevated PowerShell session:

```powershell
New-Item -ItemType Directory -Path C:\ProgramData\preflight-tunnel -Force | Out-Null
ssh-keygen -t ed25519 -f C:\ProgramData\preflight-tunnel\id_ed25519 -N '' -C "museum-pc-01"
Get-Content C:\ProgramData\preflight-tunnel\id_ed25519.pub
```

Copy the public key. The private key stays on the target.

## 2. Register the key on the bastion

Assign the target the next unused port, for example `2201`, and record it in your list. Two targets on one port break each other.

On the bastion, add the key to the `tunnel` user's `authorized_keys` with restrictions:

```bash
echo 'restrict,port-forwarding,permitlisten="localhost:2201",command="/usr/sbin/nologin" ssh-ed25519 AAAA...museum-pc-01-key... museum-pc-01' \
  | sudo tee -a /home/tunnel/.ssh/authorized_keys
sudo chown tunnel:tunnel /home/tunnel/.ssh/authorized_keys
sudo chmod 600 /home/tunnel/.ssh/authorized_keys
```

| Option | Effect |
|--------|--------|
| `restrict` | Disables PTY allocation, agent and X11 forwarding, and port forwarding |
| `port-forwarding` | Re-enables port forwarding |
| `permitlisten="localhost:2201"` | Allows a reverse listener on `127.0.0.1:2201` and no other port |
| `command="/usr/sbin/nologin"` | Refuses any shell or command |

Because each key can listen only on its own port, one target's key cannot open or intercept another target's tunnel.

## 3. Add a tunnel script to the target

Save this as `open-tunnel.ps1` where the on-site operator can find it:

```powershell
ssh -N -R 2201:localhost:22 tunnel@bastion.example.org `
    -i C:\ProgramData\preflight-tunnel\id_ed25519 `
    -o ServerAliveInterval=30 `
    -o ExitOnForwardFailure=yes
```

`-N` forwards without running a command and holds the window open. Closing the window closes the tunnel. `ExitOnForwardFailure=yes` makes a port conflict or rejected forward exit with an error.

Double-clicking a `.ps1` file may open it in an editor or flash a window and close, depending on file associations and execution policy. Have the operator right-click and choose **Run with PowerShell**, or give them a `.cmd` file that runs `powershell.exe -ExecutionPolicy Bypass -File open-tunnel.ps1`.

## 4. Install the management key on the target

Preflight logs in to the target's own sshd through the tunnel, with a separate key from the tunnel key. Add the management public key for the account Preflight uses.

For an account in the local Administrators group, Windows OpenSSH ignores the per-user `authorized_keys` file. Put the key in `C:\ProgramData\ssh\administrators_authorized_keys` and restrict its ACL:

```powershell
icacls C:\ProgramData\ssh\administrators_authorized_keys /inheritance:r
icacls C:\ProgramData\ssh\administrators_authorized_keys /grant "Administrators:F" "SYSTEM:F"
```

Other accounts use `%USERPROFILE%\.ssh\authorized_keys`.

## 5. Add the target to the inventory

Preflight does not read `~/.ssh/config`. Describe both hops in `preflight.yml` with a `jump` block. The bastion resolves `address`, so it is `127.0.0.1` plus the assigned port:

```yaml
inventory:
  hosts:
    - name: museum-pc-01
      address: 127.0.0.1
      port: 2201
      transport: ssh
      username: exhibit
      private_key: secret:museum-pc-01-os-key
      jump:
        address: bastion.example.org
        username: admin
        private_key: secret:bastion-admin-key
```

`private_key` on the host is the management key from step 4, not the tunnel key. See [Jump host fields](../reference/inventory.md#jump-host-fields).

## 6. Configure interactive SSH (optional)

For manual sessions with plain `ssh`, add the same path to an include file:

```text
# ~/.ssh/config
Include ~/.ssh/museum-tunnels.conf
```

```text
# ~/.ssh/museum-tunnels.conf
Host bastion
    HostName bastion.example.org
    User admin
    IdentityFile ~/.ssh/bastion_admin_key
    IdentitiesOnly yes

Host museum-pc-01
    HostName localhost
    Port 2201
    User exhibit
    ProxyJump bastion
    IdentityFile ~/.ssh/museum-pc-01_key
    IdentitiesOnly yes
```

Set `IdentitiesOnly yes` on both hosts. Without it, `ssh` offers every key it has at each hop and can trip `Too many authentication failures`.

## 7. Verify

Have the on-site operator run `open-tunnel.ps1`. Then, from the controller:

```bash
preflight facts museum-pc-01
```

Gathered facts confirm the jump host, the forwarded port, and the target credentials.

## Troubleshooting

| Symptom | Cause |
|---------|-------|
| `Too many authentication failures` from `ssh` | A `Host` block lacks `IdentitiesOnly yes` |
| The tunnel script exits with a forwarding error | The `-R` port does not match `permitlisten`, or another target holds the port |
| The tunnel key connects but gives no shell | Expected: the tunnel key allows forwarding only |
| The target's sshd rejects a correct management key | The account is a local administrator; use `administrators_authorized_keys` (step 4) |
| The connection drops when the operator closes the window | Expected: the tunnel lives only as long as the script runs |

## Related docs

- [Set up a reverse-tunnel bastion](./set-up-a-tunnel-bastion.md)
- [Deploy across restricted networks](../explanation/restricted-network-deployment.md)
- [Bastion and jump hosts](../explanation/targets-and-transports.md#bastion--jump-hosts)
- [Manage secrets](./manage-secrets.md)
