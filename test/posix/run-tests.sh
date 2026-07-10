#!/bin/sh
# Brings up the Ubuntu and Rocky POSIX test containers, waits for sshd,
# runs the POSIX integration suite against each, and tears down.
#
# Mirrors what the GitHub Actions workflow does, so devs and CI exercise
# the exact same path.
set -e

cd "$(dirname "$0")/../.."
compose_file="test/posix/docker-compose.yml"

cleanup() {
    docker compose -f "$compose_file" down --volumes --remove-orphans 2>/dev/null || true
}
trap cleanup EXIT INT TERM

echo "==> Building and starting POSIX test containers"
docker compose -f "$compose_file" up -d --build

echo "==> Waiting for sshd to accept connections"
sh test/posix/wait-for-ssh.sh localhost 2222 90
sh test/posix/wait-for-ssh.sh localhost 2223 90

echo "==> Running POSIX integration tests against Ubuntu 24.04 (port 2222)"
PREFLIGHT_TEST_SSH_POSIX_HOST=localhost \
PREFLIGHT_TEST_SSH_POSIX_PORT=2222 \
PREFLIGHT_TEST_SSH_POSIX_USER=pf-admin \
PREFLIGHT_TEST_SSH_POSIX_PASS=preflight \
    go test -tags integration -count=1 -run TestIntegration_POSIX ./internal/target/

echo "==> Running POSIX integration tests against Rocky Linux 9 (port 2223)"
PREFLIGHT_TEST_SSH_POSIX_HOST=localhost \
PREFLIGHT_TEST_SSH_POSIX_PORT=2223 \
PREFLIGHT_TEST_SSH_POSIX_USER=pf-admin \
PREFLIGHT_TEST_SSH_POSIX_PASS=preflight \
    go test -tags integration -count=1 -run TestIntegration_POSIX ./internal/target/

echo "==> All POSIX integration tests passed"
