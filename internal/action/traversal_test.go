package action

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestActionDirForRef_Traversal verifies that refs containing path traversal
// segments are rejected.
func TestActionDirForRef_Traversal(t *testing.T) {
	base := t.TempDir()

	cases := []struct {
		ref     string
		wantErr bool
	}{
		{"valid/action", false},
		{"github.com/org/repo@abc123", false},
		{"../../etc/passwd", true},
		{"../outside", true},
		{"valid/../../etc", true},
		{"a/b/../../../escape", true},
	}

	for _, tc := range cases {
		_, err := actionDirForRef(base, tc.ref)
		if tc.wantErr && err == nil {
			t.Errorf("actionDirForRef(%q): expected error, got nil", tc.ref)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("actionDirForRef(%q): unexpected error: %v", tc.ref, err)
		}
	}
}

// TestLocalResolver_Traversal verifies that LocalResolver rejects refs that
// would escape the base directory.
func TestLocalResolver_Traversal(t *testing.T) {
	base := t.TempDir()

	// Write a legitimate action.
	legitimate := filepath.Join(base, "myorg", "myaction")
	if err := os.MkdirAll(legitimate, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legitimate, "action.yml"), []byte(`
name: test
tasks: []
`), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewLocalResolver(base)

	// A valid ref should resolve.
	action, err := r.Resolve(context.Background(), "myorg/myaction")
	if err != nil {
		t.Errorf("valid ref: unexpected error: %v", err)
	}
	if action == nil {
		t.Error("valid ref: expected action, got nil")
	}

	// Traversal refs should return an error.
	traversalCases := []string{
		"../../etc/passwd",
		"../outside",
		"myorg/../../outside",
	}
	for _, ref := range traversalCases {
		_, err := r.Resolve(context.Background(), ref)
		if err == nil {
			t.Errorf("LocalResolver.Resolve(%q): expected error, got nil", ref)
		}
	}
}

// TestParseRemoteRef_ActionPathTraversal verifies that .. segments in the
// action sub-path are rejected during parsing.
func TestParseRemoteRef_ActionPathTraversal(t *testing.T) {
	cases := []struct {
		ref     string
		wantErr bool
	}{
		{"github.com/org/repo@main", false},
		{"github.com/org/repo/valid/path@main", false},
		{"github.com/org/repo/../escape@main", true},
		{"github.com/org/repo/valid/../../escape@main", true},
		{"github.com/org/repo/path/with\\backslash@main", true},
	}

	for _, tc := range cases {
		_, err := ParseRemoteRef(tc.ref)
		if tc.wantErr && err == nil {
			t.Errorf("ParseRemoteRef(%q): expected error, got nil", tc.ref)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("ParseRemoteRef(%q): unexpected error: %v", tc.ref, err)
		}
	}
}
