package target

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// userStateResponder builds a responder for a user with the given existence
// state and supplementary group list. It interprets the id-based probes the
// user module issues:
//   - id "name" >/dev/null 2>&1  -> exit 0 if exists, 1 otherwise
//   - id -nG "name"             -> stdout space-separated groups
func userStateResponder(name string, exists bool, groups []string) func(string, []byte) (string, string, int) {
	return func(command string, _ []byte) (string, string, int) {
		if strings.Contains(command, "id -nG") {
			if !exists {
				return "", fmt.Sprintf("id: %q: no such user", name), 1
			}
			return strings.Join(groups, " "), "", 0
		}
		// Existence probe (or any other id invocation).
		if strings.HasPrefix(command, "id ") {
			if exists {
				return "", "", 0
			}
			return "", "", 1
		}
		// Mutating commands (useradd/userdel/usermod/chpasswd) always succeed
		// in the fake; tests assert they were issued via ranCommand.
		return "", "", 0
	}
}

// --- Check -------------------------------------------------------------------

func TestCheckPOSIXUser_PresentMissingNeedsChange(t *testing.T) {
	b := &fakePOSIXBackend{responder: userStateResponder("alice", false, nil)}
	got, err := checkPOSIXUser(context.Background(), b, map[string]any{"name": "alice"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.NeedsChange {
		t.Fatal("missing user: expected NeedsChange, got OK")
	}
}

func TestCheckPOSIXUser_PresentExistsNoGroupsOK(t *testing.T) {
	b := &fakePOSIXBackend{responder: userStateResponder("alice", true, []string{"alice"})}
	got, err := checkPOSIXUser(context.Background(), b, map[string]any{"name": "alice"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.NeedsChange {
		t.Fatal("existing user with no desired groups: expected OK, got NeedsChange")
	}
}

func TestCheckPOSIXUser_PresentExistsHasGroupOK(t *testing.T) {
	b := &fakePOSIXBackend{responder: userStateResponder("alice", true, []string{"alice", "users"})}
	got, err := checkPOSIXUser(context.Background(), b, map[string]any{
		"name":   "alice",
		"groups": []any{"users"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.NeedsChange {
		t.Fatal("existing user already in group: expected OK, got NeedsChange")
	}
}

func TestCheckPOSIXUser_PresentMissingGroupNeedsChange(t *testing.T) {
	b := &fakePOSIXBackend{responder: userStateResponder("alice", true, []string{"alice"})}
	got, err := checkPOSIXUser(context.Background(), b, map[string]any{
		"name":   "alice",
		"groups": []any{"users"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.NeedsChange {
		t.Fatal("user missing desired group: expected NeedsChange, got OK")
	}
}

func TestCheckPOSIXUser_AbsentExistsNeedsChange(t *testing.T) {
	b := &fakePOSIXBackend{responder: userStateResponder("alice", true, []string{"alice"})}
	got, err := checkPOSIXUser(context.Background(), b, map[string]any{
		"name":   "alice",
		"ensure": "absent",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.NeedsChange {
		t.Fatal("existing user with ensure absent: expected NeedsChange, got OK")
	}
}

func TestCheckPOSIXUser_AbsentMissingOK(t *testing.T) {
	b := &fakePOSIXBackend{responder: userStateResponder("alice", false, nil)}
	got, err := checkPOSIXUser(context.Background(), b, map[string]any{
		"name":   "alice",
		"ensure": "absent",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.NeedsChange {
		t.Fatal("absent user with ensure absent: expected OK, got NeedsChange")
	}
}

func TestCheckPOSIXUser_MissingNameErrors(t *testing.T) {
	b := &fakePOSIXBackend{responder: userStateResponder("alice", true, nil)}
	if _, err := checkPOSIXUser(context.Background(), b, map[string]any{}); err == nil {
		t.Fatal("expected error for missing name, got nil")
	}
}

func TestCheckPOSIXUser_UnknownEnsureErrors(t *testing.T) {
	b := &fakePOSIXBackend{responder: userStateResponder("alice", true, nil)}
	if _, err := checkPOSIXUser(context.Background(), b, map[string]any{
		"name":   "alice",
		"ensure": "maybe",
	}); err == nil {
		t.Fatal("expected error for unknown ensure, got nil")
	}
}

// --- Apply -------------------------------------------------------------------

func TestApplyPOSIXUser_CreateNoPassword(t *testing.T) {
	b := &fakePOSIXBackend{responder: userStateResponder("alice", false, nil)}
	if err := applyPOSIXUser(context.Background(), b, map[string]any{"name": "alice"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !b.ranCommand("useradd") {
		t.Error("expected useradd to run")
	}
	if b.ranCommand("chpasswd") {
		t.Error("chpasswd must not run when no password is set")
	}
}

func TestApplyPOSIXUser_CreateWithPasswordSetsViaChpasswd(t *testing.T) {
	b := &fakePOSIXBackend{responder: userStateResponder("alice", false, nil)}
	if err := applyPOSIXUser(context.Background(), b, map[string]any{
		"name":     "alice",
		"password": "s3cret",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !b.ranCommand("useradd") {
		t.Error("expected useradd to run")
	}
	// chpasswd reads "name:password\n" from stdin — never on the command line.
	if !b.ranCommand("chpasswd") {
		t.Fatal("expected chpasswd to run on creation with a password")
	}
	var chpasswdStdin []byte
	for i, c := range b.commands {
		if strings.Contains(c, "chpasswd") {
			chpasswdStdin = b.stdins[i]
			break
		}
	}
	want := "alice:s3cret\n"
	if string(chpasswdStdin) != want {
		t.Fatalf("chpasswd stdin: got %q, want %q", string(chpasswdStdin), want)
	}
}

func TestApplyPOSIXUser_CreateWithGroupsRunsUsermodAdditive(t *testing.T) {
	b := &fakePOSIXBackend{responder: userStateResponder("alice", false, nil)}
	if err := applyPOSIXUser(context.Background(), b, map[string]any{
		"name":   "alice",
		"groups": []any{"users", "adm"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !b.ranCommand("usermod -aG") {
		t.Error("expected usermod -aG to run")
	}
	// Groups are passed as a single comma-joined argument, not multiple flags.
	if !b.ranCommand("users,adm") {
		t.Error("expected comma-joined group list in usermod command")
	}
}

func TestApplyPOSIXUser_ExistingUserDoesNotResetPassword(t *testing.T) {
	// The password-drift limitation: an existing user is never re-passwded,
	// even when a password is supplied and Apply runs (for group changes).
	b := &fakePOSIXBackend{responder: userStateResponder("alice", true, []string{"alice"})}
	if err := applyPOSIXUser(context.Background(), b, map[string]any{
		"name":     "alice",
		"password": "s3cret",
		"groups":   []any{"users"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.ranCommand("useradd") {
		t.Error("useradd must not run for an existing user")
	}
	if b.ranCommand("chpasswd") {
		t.Error("chpasswd must not run for an existing user (password-drift limitation)")
	}
	if !b.ranCommand("usermod -aG") {
		t.Error("expected usermod -aG to add the missing group")
	}
}

func TestApplyPOSIXUser_ExistingUserNoGroupsIsNoOp(t *testing.T) {
	b := &fakePOSIXBackend{responder: userStateResponder("alice", true, []string{"alice"})}
	if err := applyPOSIXUser(context.Background(), b, map[string]any{"name": "alice"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, c := range b.commands {
		if strings.Contains(c, "useradd") || strings.Contains(c, "usermod") || strings.Contains(c, "userdel") || strings.Contains(c, "chpasswd") {
			t.Fatalf("no-op apply issued a mutating command: %s", c)
		}
	}
}

func TestApplyPOSIXUser_AbsentRunsUserdel(t *testing.T) {
	b := &fakePOSIXBackend{responder: userStateResponder("alice", true, []string{"alice"})}
	if err := applyPOSIXUser(context.Background(), b, map[string]any{
		"name":   "alice",
		"ensure": "absent",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !b.ranCommand("userdel") {
		t.Error("expected userdel to run")
	}
}

func TestApplyPOSIXUser_MissingNameErrors(t *testing.T) {
	b := &fakePOSIXBackend{responder: userStateResponder("alice", true, nil)}
	if err := applyPOSIXUser(context.Background(), b, map[string]any{}); err == nil {
		t.Fatal("expected error for missing name, got nil")
	}
}

// TestApplyPOSIXUser_CommandFailurePropagates ensures a non-zero exit from a
// mutating command is surfaced as an error, not silently swallowed.
func TestApplyPOSIXUser_CommandFailurePropagates(t *testing.T) {
	b := &fakePOSIXBackend{responder: func(command string, _ []byte) (string, string, int) {
		if strings.Contains(command, "useradd") {
			return "", "useradd: user already exists", 9
		}
		if strings.HasPrefix(command, "id ") {
			return "", "", 1 // user missing -> triggers useradd
		}
		return "", "", 0
	}}
	err := applyPOSIXUser(context.Background(), b, map[string]any{"name": "alice"})
	if err == nil {
		t.Fatal("expected error from failed useradd, got nil")
	}
	if !strings.Contains(err.Error(), "9") {
		t.Fatalf("expected error to mention exit code 9, got: %v", err)
	}
}
