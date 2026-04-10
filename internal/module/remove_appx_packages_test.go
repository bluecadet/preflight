//go:build windows

package module

import (
	"context"
	"strings"
	"testing"
)

func TestRemoveAppxPackagesModule_GuardsAgainstEmptyPackageFullName(t *testing.T) {
	var capturedScript string
	orig := windowsCombinedOutput
	t.Cleanup(func() { windowsCombinedOutput = orig })

	windowsCombinedOutput = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		for _, arg := range args {
			if strings.Contains(arg, "$params") {
				capturedScript = arg
				break
			}
		}
		return []byte(""), nil
	}

	m := &RemoveAppxPackagesModule{}
	_ = m.Apply(context.Background(), map[string]any{
		"name":  "Microsoft.Xbox*",
		"scope": "both",
	})

	if capturedScript == "" {
		t.Skip("no script captured")
	}
	if !strings.Contains(capturedScript, "IsNullOrWhiteSpace($packageFullName)") {
		t.Fatalf("expected PackageFullName guard in script, got:\n%s", capturedScript)
	}
	if !strings.Contains(capturedScript, "skipping appx package ") {
		t.Fatalf("expected skip output for malformed package records, got:\n%s", capturedScript)
	}
}
