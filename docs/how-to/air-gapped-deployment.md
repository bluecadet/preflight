# Stage bundles for air-gapped deployment

Stage a playbook into a per-target bundle on a connected controller, carry it to an isolated Windows machine, and apply it there. The offline machine needs neither the playbook nor network access to fetch actions.

## Prerequisites

- `preflight` installed on the staging controller and on each offline target
- A playbook that passes `preflight validate`
- For plugin modules: a Windows controller with the same architecture as the targets, and the plugins discoverable (`preflight plugin list`)

## 1. Prepare encrypted secrets for target-side apply

Skip this step if the playbook does not use `secret:<name>` references.

A bundle carries the encrypted secret files it needs. The target decrypts them at apply time with its own age identity.

1. On the target, generate an identity and print its public recipient:

   ```powershell
   preflight secret identity generate --out .age\target-keys.txt
   preflight secret identity recipient .age\target-keys.txt
   ```

2. On the controller, add the target's recipient to `preflight.yml`, beside your own:

   ```yaml
   secrets:
     identity: ".age/keys.txt"
     recipients:
       - "age1developerrecipient..."
       - "age1targetrecipient..."
     entries:
       autologin-password:
         file: "secrets/autologin-password.age"
   ```

3. Re-encrypt the secrets to the new recipient list:

   ```bash
   preflight secret rekey
   ```

   Existing secret files stay encrypted to the old recipients until you run `rekey`.

## 2. Declare the target platform

Give each offline host a `platform` in `preflight.yml`:

```yaml
inventory:
  hosts:
    - name: lobby-pc-01
      address: 192.168.1.10
      transport: winrm
      platform:
        os: windows
        arch: amd64
```

With `platform` set, `stage` builds the bundle without connecting to the host, so any controller, including macOS or Linux, can stage for it. Without `platform`, `stage` connects to the host to read its OS and architecture.

## 3. Stage the bundles

```bash
preflight stage playbooks/lobby.yml --target lobby-pc-01
```

`stage` writes one zip per selected host to `dist/bundles/`. Use `--bundle-output-dir` to write elsewhere. Each file is named `<playbook>-<target>-<os>-<arch>.zip`; the same values appear in the bundle's `manifest.json`.

A bundle holds the resolved plan, the plugins the plan uses, and any encrypted secrets it references. See the [Bundle reference](../reference/bundles.md) for the format.

## 4. Transfer each bundle

Copy each bundle to the machine it was staged for. A bundle's plan is specific to one host.

## 5. Apply the bundle

On the offline machine:

```powershell
preflight apply --bundle .\lobby-lobby-pc-01-windows-amd64.zip
```

If the bundle contains encrypted secrets, pass the target identity:

```powershell
preflight apply --bundle .\lobby-lobby-pc-01-windows-amd64.zip --secret-identity .age\target-keys.txt
```

`apply --bundle` runs the bundled plan on the local machine. It does not read the source playbook, fetch actions, or load plugins from outside the bundle. Templates that reference `facts`, `env`, or `target.*` render on the target at apply time.

## Troubleshooting

### Staging fails with "depends on secret values that cannot be embedded"

A secret-like parameter, such as a `password`, holds a literal value instead of a `secret:<name>` reference, and `stage` refuses to write it into the bundle. Replace the value with a `secret:<name>` reference so the bundle carries the encrypted file. `--allow-plaintext-secrets-in-bundle` overrides the check and writes plaintext secrets into the zip; use it only when you control the transfer path end to end.

### Staging fails for a plugin

`stage` copies only the plugins the plan uses, and runs each one to check that it reports the expected module name. Plugins stage only when the controller's OS and architecture match the target's. Confirm the plugin appears in `preflight plugin list` on a matching controller.

### Staging fails with "unknown module"

The plan uses a module that is neither built in nor a discovered plugin. Check the module name, or install the plugin on the controller.

## Related docs

- [Bundle reference](../reference/bundles.md)
- [Manage secrets](./manage-secrets.md)
- [Deploy across restricted networks](../explanation/restricted-network-deployment.md)
