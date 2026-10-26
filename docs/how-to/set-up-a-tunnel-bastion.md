# Set up a reverse-tunnel bastion

Provision and harden a Linux VM that Windows targets on a private network can dial out to with a reverse SSH tunnel. You set up the bastion once for the whole fleet, then add each target with [Onboard a target through a reverse-tunnel bastion](./onboard-a-target-through-a-bastion.md).

Preflight does not create or manage the tunnel. It only connects through the bastion as an SSH jump host. To decide whether this pattern fits your network, see [Deploy across restricted networks](../explanation/restricted-network-deployment.md#use-reverse-ssh-tunnels-when-targets-can-dial-out).

## Prerequisites

- An always-on Debian or Ubuntu VM with a public IP. The commands below use `apt` and `ufw`; adjust them for other distributions.
- `sudo` access on the VM, through an account that logs in with an SSH key

Each target needs the OpenSSH client to open the tunnel and OpenSSH Server so Preflight can log in through it. Both ship with Windows.

## 1. Create the admin and tunnel users

Create one account for operators and one for tunnels, so a stolen target key cannot reach the operator account:

```bash
sudo adduser admin
sudo usermod -aG sudo admin
sudo adduser --disabled-password --shell /usr/sbin/nologin tunnel
sudo install -d -m 700 -o tunnel -g tunnel /home/tunnel/.ssh
```

Copy your current key to `admin` before you disable password logins, or you will lock yourself out:

```bash
sudo install -d -m 700 -o admin -g admin /home/admin/.ssh
sudo install -m 600 -o admin -g admin ~/.ssh/authorized_keys /home/admin/.ssh/authorized_keys
```

The `tunnel` user gets keys later, one per target, during onboarding.

## 2. Lock down sshd

Add these settings to `/etc/ssh/sshd_config`, or to a file in `/etc/ssh/sshd_config.d/`:

```text
PermitRootLogin no
PasswordAuthentication no
GatewayPorts no
```

`GatewayPorts no` binds every reverse-forwarded port to `127.0.0.1` on the bastion, so no forwarded port is reachable from the internet. Only a session already on the bastion can connect to it.

Check the config and restart sshd:

```bash
sudo sshd -t
sudo systemctl restart ssh
```

Keep your current session open. In a second terminal, confirm `ssh admin@<bastion>` still works.

## 3. Restrict the firewall

Allow inbound SSH and nothing else:

```bash
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow 22/tcp
sudo ufw enable
```

## 4. Install baseline hardening

```bash
sudo apt update
sudo apt install -y fail2ban unattended-upgrades
sudo dpkg-reconfigure -plow unattended-upgrades
```

`fail2ban` bans IPs that repeatedly fail SSH authentication. `unattended-upgrades` installs security patches automatically.

## 5. Verify

- [ ] `admin` logs in with a key
- [ ] `sudo sshd -T | grep -E 'permitrootlogin|passwordauthentication|gatewayports'` prints `no` for all three
- [ ] `tunnel` has no password and `/usr/sbin/nologin` as its shell
- [ ] `sudo ufw status` allows only `22/tcp` inbound
- [ ] `systemctl is-active fail2ban unattended-upgrades` prints `active` twice

## Troubleshooting

| Symptom | Cause |
|---------|-------|
| Locked out after restarting sshd | `admin` had no `authorized_keys` when password logins were disabled. Fix `sshd_config` through your provider's recovery console. |
| `Connection refused` on port 22 | `ufw` is enabled without the `22/tcp` rule. Check `sudo ufw status` from the provider console. |
| A forwarded port is reachable from outside the bastion | `GatewayPorts` is not `no`, or sshd was not restarted. Check `sudo sshd -T \| grep gatewayports`. |

## Related docs

- [Onboard a target through a reverse-tunnel bastion](./onboard-a-target-through-a-bastion.md)
- [Bastion and jump hosts](../explanation/targets-and-transports.md#bastion--jump-hosts)
- [Inventory reference](../reference/inventory.md#jump-host-fields)
