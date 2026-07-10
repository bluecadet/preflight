.PHONY: all build-windows-amd64 build-windows-arm64 build-local test test-integration test-integration-posix vet install verify

windows: build-windows-amd64 build-windows-arm64

build-windows-amd64:
	GOOS=windows GOARCH=amd64 go build -o dist/preflight-windows-amd64.exe .

build-windows-arm64:
	GOOS=windows GOARCH=arm64 go build -o dist/preflight-windows-arm64.exe .

build-local:
	go build -o dist/preflight .

install: build-local
	go install

test:
	go test ./...

# Live tests against a real Windows endpoint over WinRM/SSH. Gated behind the
# `integration` build tag so they stay out of the default `test` target, and
# additionally skip at runtime when PREFLIGHT_TEST_WINRM_* is unset. Requires a
# sacrificial VM (see CONTRIBUTING.md) and a .env.test with connection details.
test-integration:
	go test -tags integration -count=1 ./internal/target/

# POSIX/SSH integration suite against disposable Docker containers (Ubuntu
# 24.04 + Rocky Linux 9). Brings up both containers, waits for sshd, runs the
# file/directory/shell/wait tests over SSH against each, and tears down.
# Requires Docker. Uses the same PREFLIGHT_TEST_SSH_POSIX_* env vars as CI.
test-integration-posix:
	test/posix/run-tests.sh

vet:
	go vet ./...

lint:
	golangci-lint run

verify: test lint vet