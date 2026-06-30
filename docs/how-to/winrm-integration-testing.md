# Run The Integration Test Suite

Use this guide when you want to run Preflight's live integration tests
against a real Windows VM. The suite exercises the full end-to-end execution
path — bootstrap, auth, transport, guard, oracle, cleanup, and idempotency —
through a single module (registry) as the proving slice, repeated over every
configured transport (WinRM and/or SSH-to-Windows).

## Prerequisites

- A Windows VM on the same network as your dev machine (or otherwise reachable)
- The `preflight` repository checked out on your dev machine
- Go 1.21+ on your dev machine
- Network connectivity from your dev machine to the VM on port 5985 (WinRM)
  and/or port 22 (SSH)

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

At the end, the script prints instructions for setting the connection vars
on your dev machine.

## 3. Set The Environment Variables

The test harness reads individual `KEY=VALUE` pairs from the environment or
from a `.env.test` file. Create `.env.test` at the **repo root** (gitignored;
never commit it):

```
PREFLIGHT_TEST_WINRM_HOST=192.168.x.x
PREFLIGHT_TEST_WINRM_PORT=5985
PREFLIGHT_TEST_WINRM_USER=pf-test
PREFLIGHT_TEST_WINRM_PASS=YourStrongPassword123!

# Optional: SSH-to-Windows for the same VM (requires OpenSSH Server)
PREFLIGHT_TEST_SSH_HOST=192.168.x.x
PREFLIGHT_TEST_SSH_PORT=22
PREFLIGHT_TEST_SSH_USER=pf-test
PREFLIGHT_TEST_SSH_PASS=YourStrongPassword123!
# PREFLIGHT_TEST_SSH_KEY=/path/to/id_rsa  # optional, password auth is default
```

`PREFLIGHT_TEST_WINRM_PORT` and `PREFLIGHT_TEST_SSH_PORT` default to 5985
and 22 respectively when omitted. Each transport is independently optional —
set only the vars for the transports you want to test.

> **Security note**: The password appears in the file in plain text because
> the WinRM transport sends it as Basic auth. Only use this against disposable
> VMs. Never point these vars at a production machine.

## 4. Run The Test

The test runner loads `.env.test` automatically — no `source` or `direnv` needed.
Variables already exported in your shell take precedence over the file.

```bash
cd preflight
go test -v -run TestIntegration_Registry ./internal/target/
```

Expected output when both WinRM and SSH are configured:

```
=== RUN   TestIntegration_Registry
=== RUN   TestIntegration_Registry/winrm
--- PASS: TestIntegration_Registry/winrm (XX.XXs)
=== RUN   TestIntegration_Registry/ssh
--- PASS: TestIntegration_Registry/ssh (XX.XXs)
--- PASS: TestIntegration_Registry (XX.XXs)
```

To run all integration tests (both the multi-transport registry test and the
WinRM-only tests for other modules):

```bash
go test -v -run 'TestIntegration|TestWinRMIntegration' ./internal/target/
```

### Skipping behaviour

Each transport is independently opt-in. When the env vars for a transport are
unset, its subtest skips cleanly:

```
=== RUN   TestIntegration_Registry
=== RUN   TestIntegration_Registry/winrm
    integration_registry_test.go:XX: PREFLIGHT_TEST_WINRM_HOST / _USER / _PASS not set
--- SKIP: TestIntegration_Registry/winrm (0.00s)
=== RUN   TestIntegration_Registry/ssh
    integration_registry_test.go:XX: PREFLIGHT_TEST_SSH_HOST / _USER / _PASS not set
--- SKIP: TestIntegration_Registry/ssh (0.00s)
--- SKIP: TestIntegration_Registry (0.00s)
```

When no transports are configured, the parent test also skips. CI jobs stay
green without any configuration changes.

### Sentinel guard

If a transport points at a machine that is missing the sacrificial sentinel,
the test hard-skips with a loud message instead of mutating the target:

```
=== RUN   TestIntegration_Registry
=== RUN   TestIntegration_Registry/winrm
    winrm_integration_harness_test.go:XX: sacrificial sentinel not found on target ...
--- SKIP: TestIntegration_Registry/winrm (0.00s)
```

## Test Anatomy

`TestIntegration_Registry` is wrapped in `forEachTransport`, which runs the
same body function once per configured transport:

1. **Gate per transport**: Skips the transport subtest when its env vars are
   unset (HOST/USER/PASS independently per transport)
2. **Sentinel guard**: Asserts `HKLM\SOFTWARE\PreflightTest\IsSacrificial=1`
   via the `PowerShellRunner` interface
3. **Cleanup**: `t.Cleanup` removes the per-run registry key regardless of
   how far the test gets
4. **Present**: Creates a DWORD value, verifies via independent oracle
5. **Idempotent**: Re-check and re-apply both return `StatusOK`
6. **Dry-run**: Check-only with a different value predicts `StatusChanged`;
   oracle confirms the actual value was not mutated
7. **Drift**: Mutates the value behind the module's back via PowerShell,
   asserts Check detects it and Apply converges back
8. **Absent**: Removes the value, then removes the entire key; oracle
   confirms both, then asserts idempotent re-check returns `StatusOK`

The independent oracle is load-bearing: asserting only through the module's
own `Check()` would pass a module whose `Check` and `Apply` share a bug.

## Adding A New Integration Test

To add a new module to the integration suite:

1. Add a `TestIntegration_<Module>` function that calls `forEachTransport`
2. Register cleanup via `t.Cleanup` (use the `PowerShellRunner` interface)
3. Write an independent oracle that reads state without using the module's
   Check method
4. Use `mustExecute` for every Execute step (collapses err+status assertion)
5. Assert both correctness (oracle matches expectation) and idempotency
   (rerun Check/Apply produce `StatusOK`)
6. For coverage completeness, include dry-run and drift branches (see the
   registry test for the pattern)

When a module cannot operate over a given transport (e.g. WinRM symlink
limitation for `windows_feature`), gate the operation with a capability check
and `t.Skip` with a clear reason rather than `t.Fatal`.

## Troubleshooting

| Symptom | Likely cause |
|---------|--------------|
| `connection refused` | WinRM/SSH not enabled on the VM, or wrong IP/port |
| `401 Unauthorized` | WinRM Basic auth not enabled, or wrong username/password |
| `sentinel not found` | Bootstrap not run on this VM, or sentinel was removed |
| `timeout` | Firewall blocking the port, or VM unreachable |
| Test skips on CI | Expected — env vars are not set in CI |

Re-run the bootstrap script on the VM if you suspect the WinRM configuration
has drifted. For a completely fresh start, revert the VM to a snapshot or
redeploy the evaluation image.

## Related Docs

- [Validate a WinRM connection from macOS](./validate-winrm-from-macos.md)
- [Run a playbook against remote hosts](./remote-execution.md)
- [Targets, transports, and plugins](../explanation/targets-and-transports.md)