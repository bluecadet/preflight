//go:build integration

package target

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMain loads .env.test from the module root before any test runs, then
// delegates to the standard test runner. Variables already set in the
// environment (e.g. from CI or a manual export) are never overwritten.
func TestMain(m *testing.M) {
	loadDotEnvTest()
	os.Exit(m.Run())
}

// loadDotEnvTest walks up from the test working directory to find .env.test
// (stopping at the directory containing go.mod or .git) and loads KEY=VALUE
// pairs into the process environment. It is a no-op when the file is absent.
// Variables already present in the environment are never overwritten so CI
// exports and manual exports take precedence.
func loadDotEnvTest() {
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	envFile := findEnvTestFile(dir)
	if envFile == "" {
		return
	}
	f, err := os.Open(envFile)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		// Strip optional surrounding single or double quotes.
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') ||
				(val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		// Do not override variables already present in the environment.
		if os.Getenv(key) == "" {
			os.Setenv(key, val) //nolint:errcheck
		}
	}
}

// findEnvTestFile walks up from dir looking for .env.test, stopping when it
// finds a directory that contains go.mod or .git. Returns the path to
// .env.test if found, or an empty string if absent.
func findEnvTestFile(dir string) string {
	for {
		candidate := filepath.Join(dir, ".env.test")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		// Stop at the module/repo root.
		for _, marker := range []string{"go.mod", ".git"} {
			if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
				return ""
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// getWinRMConfigFromEnv reads four separate env vars to build the WinRM
// connection config. Returns nil + false when any required var is missing so
// callers can t.Skip cleanly.
//
// Required vars:
//   - PREFLIGHT_TEST_WINRM_HOST
//   - PREFLIGHT_TEST_WINRM_USER
//   - PREFLIGHT_TEST_WINRM_PASS
//
// Optional vars:
//   - PREFLIGHT_TEST_WINRM_PORT (default 5985)
func getWinRMConfigFromEnv() (*WinRMConfig, bool) {
	host := os.Getenv("PREFLIGHT_TEST_WINRM_HOST")
	user := os.Getenv("PREFLIGHT_TEST_WINRM_USER")
	pass := os.Getenv("PREFLIGHT_TEST_WINRM_PASS")
	if host == "" || user == "" || pass == "" {
		return nil, false
	}
	// Verify the host resolves before attempting a WinRM connection. This
	// prevents tests from hanging when .env.test contains placeholder values
	// (e.g. [IP_ADDRESS]) that are not valid, resolvable hostnames.
	resolverCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if addrs, err := net.DefaultResolver.LookupHost(resolverCtx, host); err != nil || len(addrs) == 0 {
		return nil, false
	}
	port := 5985
	if raw := os.Getenv("PREFLIGHT_TEST_WINRM_PORT"); raw != "" {
		if p, err := strconv.Atoi(raw); err == nil && p > 0 {
			port = p
		}
	}
	return &WinRMConfig{
		Host:     host,
		Port:     port,
		Username: user,
		Password: pass,
		Timeout:  60 * time.Second,
	}, true
}

var (
	runIDOnce sync.Once
	runID     string
)

// testRunID returns a stable unique token for this test process. It is derived
// from the current Unix nanosecond timestamp plus 4 random bytes, giving
// enough entropy to avoid collisions across concurrent `go test` runs against
// the same VM. The value is computed once and reused for all tests in the run.
func testRunID() string {
	runIDOnce.Do(func() {
		b := make([]byte, 4)
		if _, err := rand.Read(b); err != nil {
			// Fall back to time-only if crypto/rand is unavailable.
			runID = fmt.Sprintf("%x", time.Now().UnixNano())
			return
		}
		runID = fmt.Sprintf("%x%s", time.Now().UnixNano(), hex.EncodeToString(b))
	})
	return runID
}

// assertSacrificialSentinel checks that the target has the sacrificial sentinel
// at HKLM\SOFTWARE\PreflightTest\IsSacrificial (DWORD=1). Without this marker
// the test refuses to mutate the target, preventing accidental changes to a
// non-sacrificial machine.
func assertSacrificialSentinel(t *testing.T, tgt PowerShellRunner) {
	t.Helper()
	ctx := context.Background()

	out, err := tgt.RunPowerShell(ctx, `
$path = 'Registry::HKEY_LOCAL_MACHINE\SOFTWARE\PreflightTest'
$props = Get-ItemProperty -LiteralPath $path -Name IsSacrificial -ErrorAction SilentlyContinue
if ($null -eq $props -or $null -eq $props.IsSacrificial) {
  Write-Output 'absent'
  exit 0
}
if ($props.IsSacrificial -eq 1) { Write-Output 'present'; exit 0 }
Write-Output ('unexpected:' + $props.IsSacrificial)
`)
	if err != nil {
		t.Fatalf("sentinel check failed: %v — cannot proceed", err)
	}
	out = strings.TrimSpace(out)
	if out != "present" {
		t.Skipf("sacrificial sentinel not found on target (response: %q). "+
			"Ensure HKLM\\SOFTWARE\\PreflightTest\\IsSacrificial=1 is set on the VM "+
			"(see the 'Windows Integration Tests' section of CONTRIBUTING.md).", out)
	}
}

// getSSHConfigFromEnv reads four env vars to build the SSH connection config.
// Returns nil + false when any required var is missing so callers can t.Skip
// cleanly. Mirrors the WinRM four-var contract.
//
// Required vars:
//   - PREFLIGHT_TEST_SSH_HOST
//   - PREFLIGHT_TEST_SSH_USER
//   - PREFLIGHT_TEST_SSH_PASS
//
// Optional vars:
//   - PREFLIGHT_TEST_SSH_PORT (default 22)
//   - PREFLIGHT_TEST_SSH_KEY   (path to a private key file; password auth is
//     always attempted when PREFLIGHT_TEST_SSH_PASS is set)
func getSSHConfigFromEnv() (*SSHConfig, bool) {
	host := os.Getenv("PREFLIGHT_TEST_SSH_HOST")
	user := os.Getenv("PREFLIGHT_TEST_SSH_USER")
	pass := os.Getenv("PREFLIGHT_TEST_SSH_PASS")
	if host == "" || user == "" || pass == "" {
		return nil, false
	}
	// Verify the host resolves before attempting an SSH connection. This
	// prevents tests from hanging when .env.test contains placeholder values
	// (e.g. [IP_ADDRESS]) that are not valid, resolvable hostnames.
	resolverCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if addrs, err := net.DefaultResolver.LookupHost(resolverCtx, host); err != nil || len(addrs) == 0 {
		return nil, false
	}
	port := 22
	if raw := os.Getenv("PREFLIGHT_TEST_SSH_PORT"); raw != "" {
		if p, err := strconv.Atoi(raw); err == nil && p > 0 {
			port = p
		}
	}
	cfg := &SSHConfig{
		Host:     host,
		Port:     port,
		Username: user,
		Password: pass,
		// The lab VM's host key rotates whenever the VM is rebuilt, and the
		// default accept-new policy would pin it into the developer's real
		// ~/.ssh/known_hosts. Skip verification for the sacrificial test VM.
		HostKeyPolicy: HostKeyPolicyInsecure,
	}
	if key := os.Getenv("PREFLIGHT_TEST_SSH_KEY"); key != "" {
		cfg.PrivateKey = key
	}
	return cfg, true
}

// forEachTransport builds a target for each configured transport, asserts the
// sacrificial sentinel, and runs fn as a per-transport subtest. Transports
// whose env vars are unset are independently skipped — opt-in per transport.
//
// The fn callback receives a PowerShellRunner (for oracles and cleanup) and
// a Target (for Execute). Both may be the same concrete value.
func forEachTransport(t *testing.T, fn func(t *testing.T, runner PowerShellRunner, tgt Target)) {
	t.Helper()

	t.Run("winrm", func(t *testing.T) {
		cfg, ok := getWinRMConfigFromEnv()
		if !ok {
			t.Skip("PREFLIGHT_TEST_WINRM_HOST / _USER / _PASS not set")
		}
		tgt := NewWinRMTarget(*cfg)
		t.Cleanup(func() { _ = tgt.Close() })
		assertSacrificialSentinel(t, tgt)
		fn(t, tgt, tgt)
	})

	t.Run("ssh", func(t *testing.T) {
		cfg, ok := getSSHConfigFromEnv()
		if !ok {
			t.Skip("PREFLIGHT_TEST_SSH_HOST / _USER / _PASS not set")
		}
		tgt := NewSSHTarget(*cfg, nil)
		t.Cleanup(func() { _ = tgt.Close() })
		assertSacrificialSentinel(t, tgt)
		fn(t, tgt, tgt)
	})
}

// mustExecute collapses the Execute+err+status assertion ceremony into a
// single call. It fails the test if Execute returns an error or the result
// status does not match expectedStatus. Explicitly NOT a declarative
// lifecycle driver — it is a simple step helper for integration tests.
func mustExecute(t *testing.T, tgt Target, taskID, module string, params map[string]any, opts ExecutionOptions, dryRun bool, expectedStatus Status) Result {
	t.Helper()
	ctx := context.Background()
	result, err := tgt.Execute(ctx, taskID, module, params, opts, dryRun, nil)
	if err != nil {
		t.Fatalf("%s: Execute failed: %v", taskID, err)
	}
	if result.Status != expectedStatus {
		t.Fatalf("%s: expected status %q, got %q: %s", taskID, expectedStatus, result.Status, result.Message)
	}
	return result
}

// mustMatchOracle asserts that the independent registry oracle returns the
// expected value for the given provider-qualified registry path and value
// name. It fails the test if the oracle's output does not match.
func mustMatchOracle(t *testing.T, runner PowerShellRunner, providerPath, valueName, expected string) {
	t.Helper()
	got := readRegistryOracle(t, runner, providerPath, valueName)
	if got != expected {
		t.Fatalf("independent oracle: expected %q, got %q (path=%q, value=%q)", expected, got, providerPath, valueName)
	}
}
