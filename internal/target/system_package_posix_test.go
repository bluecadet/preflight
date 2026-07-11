package target

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeSystemPackageBackend is a minimal posixShellBackend for system_package
// unit tests. It returns a configured package manager, captures the command
// and stdin passed to RunPOSIXCommand, and returns a canned stdout/stderr/exit.
type fakeSystemPackageBackend struct {
	pm         string
	pmErr      error
	lastCmd    string
	lastStdin  []byte
	stdout     string
	stderr     string
	exitCode   int
	runErr     error
	copyCalled bool
}

func (f *fakeSystemPackageBackend) RunPowerShellScript(_ context.Context, _ string, _ OutputFunc) (string, error) {
	return "", nil
}

func (f *fakeSystemPackageBackend) RunPOSIXCommand(_ context.Context, command string, stdin []byte) (string, string, int, error) {
	f.lastCmd = command
	f.lastStdin = stdin
	return f.stdout, f.stderr, f.exitCode, f.runErr
}

func (f *fakeSystemPackageBackend) PackageManager(_ context.Context) (string, error) {
	return f.pm, f.pmErr
}

func (f *fakeSystemPackageBackend) CopyFile(context.Context, string, string) error {
	f.copyCalled = true
	return nil
}

func (f *fakeSystemPackageBackend) ReadFile(context.Context, string) ([]byte, error) {
	return nil, nil
}

func (f *fakeSystemPackageBackend) PowerShellBinary() string             { return "" }
func (f *fakeSystemPackageBackend) Probe(context.Context) (Probe, error) { return Probe{}, nil }

func TestParseSystemPackageParams_DefaultsAndValidation(t *testing.T) {
	t.Run("missing packages", func(t *testing.T) {
		_, err := parseSystemPackageParams(map[string]any{})
		if err == nil || !strings.Contains(err.Error(), "packages") {
			t.Fatalf("expected missing packages error, got %v", err)
		}
	})
	t.Run("empty list", func(t *testing.T) {
		_, err := parseSystemPackageParams(map[string]any{"packages": []any{}})
		if err == nil || !strings.Contains(err.Error(), "empty") {
			t.Fatalf("expected empty error, got %v", err)
		}
	})
	t.Run("missing name", func(t *testing.T) {
		_, err := parseSystemPackageParams(map[string]any{"packages": []any{
			map[string]any{"version": "1.0"},
		}})
		if err == nil || !strings.Contains(err.Error(), "name") {
			t.Fatalf("expected name error, got %v", err)
		}
	})
	t.Run("bad ensure", func(t *testing.T) {
		_, err := parseSystemPackageParams(map[string]any{"packages": []any{
			map[string]any{"name": "tree", "ensure": "installed"},
		}})
		if err == nil || !strings.Contains(err.Error(), "present|absent") {
			t.Fatalf("expected ensure error, got %v", err)
		}
	})
	t.Run("defaults ensure to present", func(t *testing.T) {
		specs, err := parseSystemPackageParams(map[string]any{"packages": []any{
			map[string]any{"name": "tree"},
		}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if specs[0].ensure != "present" {
			t.Errorf("ensure = %q, want present", specs[0].ensure)
		}
		if specs[0].version != "" {
			t.Errorf("version = %q, want empty", specs[0].version)
		}
	})
}

func TestSystemPackagePayload(t *testing.T) {
	specs := []systemPackageSpec{
		{name: "tree", version: "1.0", ensure: "present"},
		{name: "cowsay", version: "", ensure: "absent"},
	}
	got := string(systemPackagePayload(specs))
	want := "tree|1.0|present\ncowsay||absent\n"
	if got != want {
		t.Fatalf("payload: got %q, want %q", got, want)
	}
}

func TestCheckPOSIXSystemPackage_MissingPrerequisite(t *testing.T) {
	backend := &fakeSystemPackageBackend{pm: ""}
	_, err := checkPOSIXSystemPackage(context.Background(), backend, map[string]any{
		"packages": []any{map[string]any{"name": "tree"}},
	})
	var mse *ModuleSupportError
	if !errors.As(err, &mse) {
		t.Fatalf("expected *ModuleSupportError, got %T: %v", err, err)
	}
	if mse.Class != ClassMissingPrerequisite {
		t.Errorf("class = %q, want %q", mse.Class, ClassMissingPrerequisite)
	}
	if mse.Module != "system_package" {
		t.Errorf("module = %q, want system_package", mse.Module)
	}
}

func TestCheckPOSIXSystemPackage_UnsupportedManager(t *testing.T) {
	backend := &fakeSystemPackageBackend{pm: "pacman"}
	_, err := checkPOSIXSystemPackage(context.Background(), backend, map[string]any{
		"packages": []any{map[string]any{"name": "tree"}},
	})
	var mse *ModuleSupportError
	if !errors.As(err, &mse) || mse.Class != ClassMissingPrerequisite {
		t.Fatalf("expected missing_prerequisite for unsupported manager, got %v", err)
	}
}

func TestCheckPOSIXSystemPackage_AptChangeVsOK(t *testing.T) {
	cases := []struct {
		name   string
		stdout string
		want   bool
	}{
		{"change", "change\n", true},
		{"ok", "ok\n", false},
		{"ok-trimmed", "  ok  \n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend := &fakeSystemPackageBackend{pm: "apt", stdout: tc.stdout}
			res, err := checkPOSIXSystemPackage(context.Background(), backend, map[string]any{
				"packages": []any{map[string]any{"name": "tree"}},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.NeedsChange != tc.want {
				t.Errorf("NeedsChange = %v, want %v", res.NeedsChange, tc.want)
			}
			if backend.lastCmd != posixSystemPackageAptCheckScript {
				t.Errorf("expected apt check script, got %q", backend.lastCmd)
			}
		})
	}
}

func TestCheckPOSIXSystemPackage_DnfSelectsDnfScript(t *testing.T) {
	backend := &fakeSystemPackageBackend{pm: "dnf", stdout: "ok\n"}
	_, err := checkPOSIXSystemPackage(context.Background(), backend, map[string]any{
		"packages": []any{map[string]any{"name": "tree"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backend.lastCmd != posixSystemPackageDnfCheckScript {
		t.Errorf("expected dnf check script, got %q", backend.lastCmd)
	}
}

func TestCheckPOSIXSystemPackage_PassesPayloadAsStdin(t *testing.T) {
	backend := &fakeSystemPackageBackend{pm: "apt", stdout: "ok\n"}
	_, err := checkPOSIXSystemPackage(context.Background(), backend, map[string]any{
		"packages": []any{map[string]any{"name": "tree", "version": "1.0"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "tree|1.0|present\n"
	if string(backend.lastStdin) != want {
		t.Errorf("stdin = %q, want %q", backend.lastStdin, want)
	}
}

func TestCheckPOSIXSystemPackage_NonZeroExitIsError(t *testing.T) {
	backend := &fakeSystemPackageBackend{pm: "apt", exitCode: 2, stderr: "boom"}
	_, err := checkPOSIXSystemPackage(context.Background(), backend, map[string]any{
		"packages": []any{map[string]any{"name": "tree"}},
	})
	if err == nil || !strings.Contains(err.Error(), "code 2") {
		t.Fatalf("expected exit-code error, got %v", err)
	}
}

func TestApplyPOSIXSystemPackage_MissingPrerequisite(t *testing.T) {
	backend := &fakeSystemPackageBackend{pm: ""}
	_, err := applyPOSIXSystemPackage(context.Background(), backend, map[string]any{
		"packages": []any{map[string]any{"name": "tree"}},
	}, nil)
	var mse *ModuleSupportError
	if !errors.As(err, &mse) || mse.Class != ClassMissingPrerequisite {
		t.Fatalf("expected missing_prerequisite, got %v", err)
	}
	if backend.lastCmd != "" {
		t.Errorf("apply script should not run when prerequisite missing, ran %q", backend.lastCmd)
	}
}

func TestApplyPOSIXSystemPackage_AptRunsApplyScript(t *testing.T) {
	backend := &fakeSystemPackageBackend{pm: "apt", stdout: ""}
	_, err := applyPOSIXSystemPackage(context.Background(), backend, map[string]any{
		"packages": []any{map[string]any{"name": "tree", "ensure": "present"}},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backend.lastCmd != posixSystemPackageAptApplyScript {
		t.Errorf("expected apt apply script, got %q", backend.lastCmd)
	}
}

func TestApplyPOSIXSystemPackage_NonZeroExitIsError(t *testing.T) {
	backend := &fakeSystemPackageBackend{pm: "dnf", exitCode: 1, stderr: "nope"}
	_, err := applyPOSIXSystemPackage(context.Background(), backend, map[string]any{
		"packages": []any{map[string]any{"name": "tree"}},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "code 1") {
		t.Fatalf("expected exit-code error, got %v", err)
	}
}

func TestApplyPOSIXSystemPackage_ProbeErrorPropagates(t *testing.T) {
	backend := &fakeSystemPackageBackend{pmErr: errors.New("transport down")}
	_, err := applyPOSIXSystemPackage(context.Background(), backend, map[string]any{
		"packages": []any{map[string]any{"name": "tree"}},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "transport down") {
		t.Fatalf("expected probe error to propagate, got %v", err)
	}
}
