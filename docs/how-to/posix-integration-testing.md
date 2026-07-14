# Run The POSIX/SSH Integration Test Suite

Use this guide when you want to run Preflight's live POSIX/SSH integration
tests against disposable Linux containers. The suite exercises the full
end-to-end POSIX-over-SSH execution path — `file`, `directory`, `shell`,
`wait` (including `service_running`), `service`, `user`, `system_package`,
`reboot`'s `if_needed` probe, and the plugin handle end-to-end — over SSH
against real systemd-enabled hosts.

For the Windows/WinRM integration suite against a live Windows VM, see
[Run the integration test suite](./winrm-integration-testing.md). The two
suites are independent.

## What The Suite Covers

The POSIX suite lives behind the `integration` build tag in
`internal/target/` (functions named `TestIntegration_POSIX*`) plus the
shared ops-interface conformance suite and the plugin handle e2e test. It
runs against two privileged systemd-enabled Docker containers:

| Container | Base image     | Package manager | Host SSH port |
|-----------|----------------|-----------------|--------------|
| `ubuntu`  | `ubuntu:24.04` | apt             | 2222         |
| `rocky`   | `rockylinux:9` | dnf             | 2223         |

These are CI images, not the support matrix. POSIX support is
capability-based (strict POSIX `sh`, core utilities plus `base64`, systemd,
`apt`/`dnf`); see
[the built-in module reference](../reference/modules.md#posix-capability-baseline-and-tiers).

## Prerequisites

- Docker (with `docker compose`)
- The `preflight` repository checked out
- Go 1.25+ (per `go.mod`)

The containers run systemd as PID 1, so they must be started with
`privileged: true` and `cgroup: host` (already set in
`test/posix/docker-compose.yml`).

## 1. Run The Whole Suite With One Make Target

```bash
make test-integration-posix
```

This brings up both containers, waits for sshd to accept connections, runs
the integration suite against each in turn, and tears the containers down.
It is the exact path the GitHub Actions CI job uses, so a local green run
means CI will be green too.

Under the hood `test/posix/run-tests.sh` does:

```bash
docker compose -f test/posix/docker-compose.yml up -d --build
sh test/posix/wait-for-ssh.sh localhost 2222 90
sh test/posix/wait-for-ssh.sh localhost 2223 90
PREFLIGHT_TEST_SSH_POSIX_HOST=localhost \
PREFLIGHT_TEST_SSH_POSIX_PORT=2222 \
PREFLIGHT_TEST_SSH_POSIX_USER=pf-admin \
PREFLIGHT_TEST_SSH_POSIX_PASS=preflight \
    go test -tags integration -count=1 -run TestIntegration_POSIX ./internal/target/
# ...then the same against port 2223 (Rocky)
docker compose -f test/posix/docker-compose.yml down --volumes --remove-orphans
```

## 2. Run Against One Container Manually

To iterate against a single container without the full make target:

```bash
# Start one container
docker compose -f test/posix/docker-compose.yml up -d --build ubuntu

# Wait for sshd
sh test/posix/wait-for-ssh.sh localhost 2222 90

# Run the suite against it
PREFLIGHT_TEST_SSH_POSIX_HOST=localhost \
PREFLIGHT_TEST_SSH_POSIX_PORT=2222 \
PREFLIGHT_TEST_SSH_POSIX_USER=pf-admin \
PREFLIGHT_TEST_SSH_POSIX_PASS=preflight \
    go test -tags integration -count=1 -run TestIntegration_POSIX -v ./internal/target/

# Tear down
docker compose -f test/posix/docker-compose.yml down
```

## 3. Environment Variables

The harness reads these env vars and **skips cleanly when the required ones
are unset**, so CI jobs and `go test ./...` stay green without any
configuration. Each transport is independently opt-in.

| Variable                        | Required | Default | Description                         |
|---------------------------------|----------|---------|-------------------------------------|
| `PREFLIGHT_TEST_SSH_POSIX_HOST` | yes      | —       | SSH host                            |
| `PREFLIGHT_TEST_SSH_POSIX_PORT` | no       | 22      | SSH port                            |
| `PREFLIGHT_TEST_SSH_POSIX_USER` | yes      | —       | SSH username                        |
| `PREFLIGHT_TEST_SSH_POSIX_PASS` | yes      | —       | SSH password                        |
| `PREFLIGHT_TEST_SSH_POSIX_KEY`  | no       | —       | Path to a private key file          |

You do not need a `.env.test` file for the POSIX suite — the make target and
the CI job set the vars inline. If you prefer one, set the same keys there
and export them before running `go test`.

## 4. The Three-User Become Matrix

Each container provisions three local users (password `preflight` for all)
so the suite can exercise every branch of the POSIX privilege model from
[How `become` works](../explanation/become.md):

| User          | sudo              | What the suite exercises with it                                |
|---------------|-------------------|-----------------------------------------------------------------|
| `pf-admin`    | `NOPASSWD:ALL`    | The main test account; `requires_root` modules succeed via bare `become` |
| `pf-sudopass` | password required | `sudo -S` password feeding via `become.password`, and `sudo -n` fail-fast with `sudo-password-required` |
| `pf-nosudo`   | none              | `requires_root` refusal with `requires-root-violation`, and `sudo-missing` when `become` is enabled |

Root SSH login is disabled (`PermitRootLogin no` in each container's
`/etc/ssh/sshd_config.d/00-preflight.conf`), so the "root login as a stated
alternative" path is unit-tested rather than exercised here. The
`sudo-missing` typed error is also unit-tested only (no container ships
without `sudo`).

A sacrificial sentinel at `/etc/preflight-test-sacrificial` prevents the
harness from running against a machine that was not provisioned by these
Dockerfiles — the suite hard-skips if the sentinel is missing.

## 5. CI

GitHub Actions runs the POSIX suite on every PR that touches Go code or the
`test/posix/` tree, using a matrix of both containers. See the
`test-posix-integration` job in `.github/workflows/ci.yml`. The Windows
integration suite stays developer-run-only.

## Adding A New POSIX Integration Test

1. Add a `TestIntegration_POSIX_<Module>` function in
   `internal/target/` behind the `integration` build tag.
2. Use the shared POSIX harness helpers (sentinel guard, cleanup) the
   existing tests use.
3. Write an independent oracle that reads state without going through the
   module's own `Check()` — a `RunCommand` that queries the package
   database, `systemctl`, `/etc/passwd`, etc.
4. Assert both correctness (oracle matches expectation) and idempotency
   (re-running `Check`/`Apply` reports no change).
5. Gate module-specific prerequisites (no systemd, no `apt`/`dnf`) with a
   typed `missing_prerequisite` check and `t.Skip` with a clear reason
   rather than `t.Fatal`.

## Troubleshooting

| Symptom                                   | Likely cause                                                       |
|-------------------------------------------|--------------------------------------------------------------------|
| `docker compose up` fails to start systemd | Docker not given `privileged`/`cgroup: host`; use the make target |
| `connection refused` on 2222/2223         | sshd not up yet; re-run `wait-for-ssh.sh`                           |
| Test skips on a container host             | sentinel missing — rebuild the image (`docker compose up --build`)  |
| `permission denied (publickey,password)`   | wrong `PREFLIGHT_TEST_SSH_POSIX_PASS`; it is `preflight` for all users |

## Related Docs

- [Run the integration test suite (Windows/WinRM)](./winrm-integration-testing.md)
- [Run a playbook against remote hosts](./remote-execution.md)
- [How `become` works](../explanation/become.md)
- [Targets, transports, and plugins](../explanation/targets-and-transports.md)
