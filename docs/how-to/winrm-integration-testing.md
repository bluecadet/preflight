# Run The WinRM Integration Test Suite

Use this guide when you want to run Preflight's live WinRM integration tests
against a real Windows VM. The suite exercises the full end-to-end execution
path — bootstrap, auth, guard, oracle, cleanup, and idempotency — through a
single module (registry) as the proving slice.

## Prerequisites

- A Windows VM on the same network as your dev machine (or otherwise reachable)
- The `preflight` repository checked out on your dev machine
- Go 1.21+ on your dev machine
- Network connectivity from your dev machine to the VM on port 5985

## 1. Get A Windows VM

### VMware Fusion (macOS)

The fastest path is a free Windows evaluation VM from Microsoft:

```bash
# Download a Windows 11 Dev Environment VM
brew install wget
wget -O ~/Downloads/win11-dev.vmwarevm.zip \
  'https://aka.ms/windev_VM_vmware'

# Extract and open in VMware Fusion
cd ~/Downloads
unzip win11-dev.vmwarevm.zip
open win11-dev.vmwarevm
```

The Windows 11 Dev Environment VMs come with Visual Studio and developer tools
pre-installed. They expire after 90 days, which makes them ideal disposable
test targets.

After the VM boots:

1. Complete the OOBE (accept defaults, set a local user password)
2. Note the IP address shown on the login screen, or find it later with
   `ipconfig` inside the VM
3. Ensure both machines are on the same network (NAT or bridged)

### VirtualBox (cross-platform)

Microsoft also publishes Hyper-V and VirtualBox images from the same download
page. The bootstrap script works identically regardless of hypervisor.

## 2. Run The Bootstrap Script

Inside the Windows VM, open **PowerShell as Administrator** and run:

```powershell
# Set the password for the pf-test user (use a strong, unique password)
$env:PREFLIGHT_TEST_WINRM_PASS = 'YourStrongPassword123!'

# Run the bootstrap script directly from the preflight repo
# (or copy scripts/bootstrap-winrm-vm.ps1 to the VM first)
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass -Force
iex ((New-Object System.Net.WebClient).DownloadString(
  'https://raw.githubusercontent.com/bluecadet/preflight/main/scripts/bootstrap-winrm-vm.ps1'
))
```

If the VM does not have internet access, copy the script to the VM first:

```powershell
# On the VM, paste the script contents or run from a local file
.\bootstrap-winrm-vm.ps1
```

When prompted, enter the same password you set above.

The script will:

1. Enable WinRM over HTTP with Basic authentication on port 5985
2. Create the `pf-test` local user and add it to Administrators
3. Open the Windows Firewall for inbound WinRM traffic
4. Write the sacrificial sentinel to the registry

At the end, the script prints the exact `PREFLIGHT_TEST_WINRM` environment
variable value to set on your dev machine.

## 3. Set The Environment Variable

On your **dev machine** (macOS or Linux), set the env var using the JSON blob
printed by the bootstrap script:

```bash
export PREFLIGHT_TEST_WINRM='{"host":"192.168.x.x","port":5985,"user":"pf-test","pass":"YourStrongPassword123!"}'
```

Or add it to `.env.test` in the repo root (gitignored) for persistence:

```bash
echo 'PREFLIGHT_TEST_WINRM={"host":"192.168.x.x","port":5985,"user":"pf-test","pass":"YourStrongPassword123!"}' >> .env.test
```

> **Security note**: The password appears in the env var in plain text because
> the WinRM transport sends it as Basic auth. Only use this against disposable
> VMs. Never point `PREFLIGHT_TEST_WINRM` at a production machine.

## 4. Run The Test

```bash
cd preflight
go test -v -run TestWinRMIntegration ./internal/target/
```

Expected output (abbreviated):

```
=== RUN   TestWinRMIntegration_Registry
--- PASS: TestWinRMIntegration_Registry (XX.XXs)
```

### Skipping behaviour

When `PREFLIGHT_TEST_WINRM` is unset, the test skips with a clear message:

```
=== RUN   TestWinRMIntegration_Registry
    winrm_integration_test.go:XX: PREFLIGHT_TEST_WINRM is not set; skipping ...
--- SKIP: TestWinRMIntegration_Registry (0.00s)
```

This means the Ubuntu CI job stays green and all existing workflows are
unaffected.

### Sentinel guard

If `PREFLIGHT_TEST_WINRM` points at a machine that is missing the sacrificial
sentinel, the test hard-skips with a loud message instead of mutating the
target:

```
=== RUN   TestWinRMIntegration_Registry
    winrm_integration_test.go:XX: sacrificial sentinel not found on target ...
--- SKIP: TestWinRMIntegration_Registry (0.00s)
```

## Test Anatomy

The `TestWinRMIntegration_Registry` test:

1. **Gate**: Skips if `PREFLIGHT_TEST_WINRM` is unset
2. **Guard**: Asserts the sacrificial sentinel exists on the target
3. **Apply**: Creates a DWORD value under `HKLM\SOFTWARE\PreflightTest\IntegrationTest`
4. **Oracle**: Verifies the value via a standalone PowerShell `Get-ItemProperty` call
5. **Idempotency**: Re-runs Check (expects no change) and Apply (expects no-op)
6. **Multi-value**: Applies a string alongside the DWORD and verifies both
7. **Removal**: Removes a value and asserts absence via the oracle
8. **Ensure absent**: Removes the entire key and confirms via the oracle
9. **Cleanup**: Removes the entire test namespace via `t.Cleanup`, robust to
   mid-test failure

The independent oracle is load-bearing: asserting only through the module's
own `Check()` would pass a module whose `Check` and `Apply` share a bug.

## Adding A New Integration Test

To add a new module to the integration suite:

1. Add a `TestWinRMIntegration_<Module>` function in the same file
2. Call `getWinRMConfigFromEnv()` and `assertSacrificialSentinel()` as guards
3. Register cleanup via `t.Cleanup`
4. Write an independent oracle that reads state without using the module's
   Check method
5. Assert both correctness (oracle matches expectation) and idempotency
   (rerun Check/Apply produce no change)

## Troubleshooting

| Symptom | Likely cause |
|---------|--------------|
| `connection refused` | WinRM not enabled on the VM, or wrong IP/port |
| `401 Unauthorized` | WinRM Basic auth not enabled, or wrong username/password |
| `sentinel not found` | Bootstrap not run on this VM, or sentinel was removed |
| `timeout` | Firewall blocking port 5985, or VM unreachable |
| Test skips on CI | Expected — `PREFLIGHT_TEST_WINRM` is not set in CI |

Re-run the bootstrap script on the VM if you suspect the WinRM configuration
has drifted. For a completely fresh start, revert the VM to a snapshot or
redeploy the evaluation image.

## Related Docs

- [Validate a WinRM connection from macOS](./validate-winrm-from-macos.md)
- [Run a playbook against remote hosts](./remote-execution.md)
- [Targets, transports, and plugins](../explanation/targets-and-transports.md)