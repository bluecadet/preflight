package action

import "testing"

func TestParseRemoteRef(t *testing.T) {
	tests := []struct {
		name       string
		ref        string
		repository string
		actionPath string
		revision   string
		ok         bool
	}{
		{
			name:       "repo only",
			ref:        "github.com/acme/actions@v1.2.3",
			repository: "github.com/acme/actions",
			revision:   "v1.2.3",
			ok:         true,
		},
		{
			name:       "repo plus path",
			ref:        "github.com/acme/actions/signage/kiosk@main",
			repository: "github.com/acme/actions",
			actionPath: "signage/kiosk",
			revision:   "main",
			ok:         true,
		},
		{
			name:       "sha pinned",
			ref:        "github.com/acme/actions/signage@0123456789abcdef0123456789abcdef01234567",
			repository: "github.com/acme/actions",
			actionPath: "signage",
			revision:   "0123456789abcdef0123456789abcdef01234567",
			ok:         true,
		},
		{name: "missing revision", ref: "github.com/acme/actions/signage", ok: false},
		{name: "missing repo segments", ref: "github.com/acme@v1", ok: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := ParseRemoteRef(tc.ref)
			if !tc.ok {
				if err == nil {
					t.Fatalf("expected parse error for %q", tc.ref)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRemoteRef(%q): %v", tc.ref, err)
			}
			if parsed.Repository != tc.repository {
				t.Fatalf("repository: got %q want %q", parsed.Repository, tc.repository)
			}
			if parsed.ActionPath != tc.actionPath {
				t.Fatalf("action path: got %q want %q", parsed.ActionPath, tc.actionPath)
			}
			if parsed.Revision != tc.revision {
				t.Fatalf("revision: got %q want %q", parsed.Revision, tc.revision)
			}
		})
	}
}

func TestLockfilePinCanonicalizesPinnedRef(t *testing.T) {
	lockfile := &Lockfile{Actions: make(map[string]LockEntry)}
	ref := "github.com/acme/actions/signage@v2.1"
	sha := "0123456789abcdef0123456789abcdef01234567"

	if err := lockfile.Pin(ref, sha); err != nil {
		t.Fatalf("Pin: %v", err)
	}

	entry, ok := lockfile.Lookup(ref)
	if !ok {
		t.Fatalf("expected lock entry for %q", ref)
	}
	if entry.SHA != sha {
		t.Fatalf("SHA: got %q want %q", entry.SHA, sha)
	}
	wantPinned := "github.com/acme/actions/signage@" + sha
	if entry.Pinned != wantPinned {
		t.Fatalf("Pinned: got %q want %q", entry.Pinned, wantPinned)
	}
}
