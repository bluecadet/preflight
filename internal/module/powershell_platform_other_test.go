//go:build !windows

package module

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestMain fakes platformLookPath for the whole package's local
// (non-Windows) test run, so tests that exercise the powershell module
// (which resolve a real local binary via platformPowerShellBinary before
// running it) do not depend on pwsh or powershell actually being installed
// on the machine running go test. Real resolution behavior -- preference
// order and the neither-found error -- is covered directly by the
// TestPlatformPowerShellBinary_* tests below, which install their own
// fake and restore it when done.
func TestMain(m *testing.M) {
	orig := platformLookPath
	platformLookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	code := m.Run()
	platformLookPath = orig
	os.Exit(code)
}

func TestPlatformPowerShellBinary_PrefersPwshOverPowershell(t *testing.T) {
	orig := platformLookPath
	t.Cleanup(func() { platformLookPath = orig })

	var looked []string
	platformLookPath = func(name string) (string, error) {
		looked = append(looked, name)
		return "/usr/local/bin/" + name, nil
	}

	got, err := platformPowerShellBinary()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/usr/local/bin/pwsh" {
		t.Fatalf("platformPowerShellBinary() = %q, want the resolved pwsh path", got)
	}
	if len(looked) != 1 || looked[0] != "pwsh" {
		t.Fatalf("looked up %v, want only [\"pwsh\"] (powershell should not be probed once pwsh resolves)", looked)
	}
}

func TestPlatformPowerShellBinary_FallsBackToPowershellWhenNoPwsh(t *testing.T) {
	orig := platformLookPath
	t.Cleanup(func() { platformLookPath = orig })

	platformLookPath = func(name string) (string, error) {
		if name == "pwsh" {
			return "", exec.ErrNotFound
		}
		return "/usr/bin/" + name, nil
	}

	got, err := platformPowerShellBinary()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/usr/bin/powershell" {
		t.Fatalf("platformPowerShellBinary() = %q, want the resolved powershell fallback path", got)
	}
}

func TestPlatformPowerShellBinary_ErrorNamesBothBinariesWhenNeitherFound(t *testing.T) {
	orig := platformLookPath
	t.Cleanup(func() { platformLookPath = orig })

	platformLookPath = func(name string) (string, error) { return "", exec.ErrNotFound }

	_, err := platformPowerShellBinary()
	if err == nil {
		t.Fatal("platformPowerShellBinary() error = nil, want an actionable error naming both binaries")
	}
	for _, want := range []string{"pwsh", "powershell"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("platformPowerShellBinary() error = %q, want it to name %q", err.Error(), want)
		}
	}
}
