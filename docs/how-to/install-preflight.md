# Install Preflight

Install the `preflight` CLI from a precompiled release, or build it from source.

| Method | Use when |
| --- | --- |
| Install script | Setting up a workstation |
| Direct download | You need to verify artifacts yourself |
| Build from source | You are developing Preflight or need an unreleased change |

## Install with the script

On macOS or Linux:

```bash
curl -fsSL https://preflight.bluecadet.com/install.sh | sh
```

The script installs to `/usr/local/bin/preflight`, using `sudo` if that directory is not writable. To install elsewhere, set `PREFLIGHT_INSTALL_DIR`:

```bash
curl -fsSL https://preflight.bluecadet.com/install.sh | PREFLIGHT_INSTALL_DIR="$HOME/.local/bin" sh
```

On Windows PowerShell:

```powershell
irm https://preflight.bluecadet.com/install.ps1 | iex
```

The script installs to `%LOCALAPPDATA%\preflight\preflight.exe` and adds that directory to your user `PATH`.

Both scripts install the latest release. Set `PREFLIGHT_VERSION=v1.2.3` (or `$env:PREFLIGHT_VERSION = "v1.2.3"`) to pin a release.

Both scripts check the archive against the published SHA-256 checksum. If `cosign` is on your `PATH`, they also verify the checksum file's signature.

## Download a release

1. Download the archive for your OS and architecture from [GitHub Releases](https://github.com/bluecadet/preflight/releases). Archives follow the pattern `preflight_<OS>_<ARCH>.tar.gz` (`.zip` on Windows), for example `preflight_Windows_x86_64.zip`, `preflight_Darwin_arm64.tar.gz`, or `preflight_Linux_x86_64.tar.gz`.
2. Extract the `preflight` binary to a directory on your `PATH`.
3. Confirm the install:

   ```bash
   preflight --version
   ```

## Verify a release manually

Each release includes `preflight_checksums.txt` and a Sigstore bundle, `preflight_checksums.txt.sigstore.json`. The release workflow signs the checksum file with keyless `cosign` through GitHub OIDC.

1. Download the archive, `preflight_checksums.txt`, and `preflight_checksums.txt.sigstore.json`.
2. Compute the archive digest and compare it with the matching line in `preflight_checksums.txt`:

   ```bash
   shasum -a 256 <archive>
   ```

   ```powershell
   Get-FileHash <archive> -Algorithm SHA256
   ```

3. Verify the checksum file's signature:

   ```bash
   cosign verify-blob \
     --bundle preflight_checksums.txt.sigstore.json \
     --certificate-identity-regexp '^https://github\.com/bluecadet/preflight/\.github/workflows/release\.yml@refs/tags/.+$' \
     --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
     preflight_checksums.txt
   ```

## Build from source

You need Go and a checkout of the repository.

```bash
make build-local          # dist/preflight for this machine
make build-windows-amd64  # dist/preflight-windows-amd64.exe
make build-windows-arm64  # dist/preflight-windows-arm64.exe
```

## Upgrade

Re-run the install script. Set `PREFLIGHT_VERSION` to move to a specific release. If you installed by direct download, replace the binary with the one from the new archive.

Confirm the new version with `preflight --version`.

## Uninstall

Delete the binary.

On macOS or Linux:

```bash
sudo rm /usr/local/bin/preflight
```

On Windows, delete the install directory, then remove it from your user `PATH`:

```powershell
Remove-Item -Recurse -Force "$env:LOCALAPPDATA\preflight"
```

Uninstalling leaves your projects alone. To reclaim space, delete the remote action cache at `~/.preflight/actions`.

## Troubleshooting

### The script cannot write to the install directory

Set `PREFLIGHT_INSTALL_DIR` to a directory you can write to, so the script does not need `sudo`.

### The shell cannot find `preflight`

Add the install directory to your `PATH`, open a new shell, and run `preflight --version`.

## Related docs

- [Quickstart](../tutorials/quickstart.md)
- [Manage your first Windows target](../tutorials/first-windows-target.md)
- [Enable remote access on a Windows target](./enable-remote-access.md)
