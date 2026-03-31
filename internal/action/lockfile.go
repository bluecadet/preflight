package action

import (
	"encoding/json"
	"fmt"
	"os"
)

// LockEntry records a pinned action reference.
type LockEntry struct {
	Ref    string `json:"ref"`
	SHA    string `json:"sha"`
	Pinned string `json:"pinned"` // original ref before SHA pinning
}

// Lockfile manages the preflight.lock file which pins remote action refs to
// exact Git SHAs for reproducible builds.
type Lockfile struct {
	Actions map[string]LockEntry `json:"actions"`
}

// LoadLockfile reads and parses a lockfile from path. If the file does not
// exist, an empty Lockfile is returned without error.
func LoadLockfile(path string) (*Lockfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Lockfile{Actions: make(map[string]LockEntry)}, nil
		}
		return nil, fmt.Errorf("lockfile: read %q: %w", path, err)
	}
	var lf Lockfile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("lockfile: parse %q: %w", path, err)
	}
	if lf.Actions == nil {
		lf.Actions = make(map[string]LockEntry)
	}
	return &lf, nil
}

// Save writes the lockfile to path as indented JSON.
func (l *Lockfile) Save(path string) error {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return fmt.Errorf("lockfile: marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("lockfile: write %q: %w", path, err)
	}
	return nil
}

// Pin records a pinned SHA for the given ref.
func (l *Lockfile) Pin(ref, sha string) {
	l.Actions[ref] = LockEntry{Ref: ref, SHA: sha, Pinned: ref}
}

// Lookup returns the LockEntry for ref, or false if not pinned.
func (l *Lockfile) Lookup(ref string) (LockEntry, bool) {
	e, ok := l.Actions[ref]
	return e, ok
}
