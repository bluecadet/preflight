# POSIX/SSH Integration Test Containers

Two privileged systemd-enabled Docker containers running sshd, used by the
POSIX integration test suite (`TestIntegration_POSIX*` in
`internal/target/`).

| Container  | Base image      | Package manager | Host SSH port |
|------------|-----------------|-----------------|---------------|
| `ubuntu`   | `ubuntu:24.04`  | apt             | 2222          |
| `rocky`    | `rockylinux:9`  | dnf             | 2223          |

Each container provisions three users (password `preflight` for all):

| User          | Sudo            | Purpose                                         |
|---------------|-----------------|-------------------------------------------------|
| `pf-admin`    | NOPASSWD        | Main test account (used by the first coverage)  |
| `pf-sudopass` | password required | Exercises `sudo -S` (later become tickets)   |
| `pf-nosudo`   | none            | Exercises `requires_root` refusal (later)       |

Root SSH login is disabled (`PermitRootLogin no` in
`/etc/ssh/sshd_config.d/00-preflight.conf`). A sacrificial sentinel marker
at `/etc/preflight-test-sacrificial` prevents the test harness from running
against a non-sacrificial machine.

## Running locally

```bash
make test-integration-posix
```

This brings up both containers, waits for sshd, runs the integration suite
against each, and tears down. Requires Docker.

To run against a single container manually:

```bash
# Start one container
docker compose -f test/posix/docker-compose.yml up -d --build ubuntu

# Wait for sshd
sh test/posix/wait-for-ssh.sh localhost 2222 90

# Run the tests
PREFLIGHT_TEST_SSH_POSIX_HOST=localhost \
PREFLIGHT_TEST_SSH_POSIX_PORT=2222 \
PREFLIGHT_TEST_SSH_POSIX_USER=pf-admin \
PREFLIGHT_TEST_SSH_POSIX_PASS=preflight \
    go test -tags integration -count=1 -run TestIntegration_POSIX -v ./internal/target/

# Tear down
docker compose -f test/posix/docker-compose.yml down
```

## Environment variables

| Variable                          | Required | Default | Description                     |
|-----------------------------------|----------|---------|---------------------------------|
| `PREFLIGHT_TEST_SSH_POSIX_HOST`   | yes      | —       | SSH host                        |
| `PREFLIGHT_TEST_SSH_POSIX_PORT`   | no       | 22      | SSH port                        |
| `PREFLIGHT_TEST_SSH_POSIX_USER`   | yes      | —       | SSH username                    |
| `PREFLIGHT_TEST_SSH_POSIX_PASS`   | yes      | —       | SSH password                    |
| `PREFLIGHT_TEST_SSH_POSIX_KEY`    | no       | —       | Path to a private key file     |

Tests skip cleanly when the required vars are unset.

## CI

GitHub Actions runs the suite on every PR touching Go code, using a matrix
of both containers. See the `test-posix-integration` job in
`.github/workflows/ci.yml`.
