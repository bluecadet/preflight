package target

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This drift test guards the contract from the SSH-first-class-transport spec
// §7/§8: the support matrix shown in docs/reference/modules.md must match the
// authoritative module×runtime matrix in catalog.go. When a module's runtime
// support or requires_root flag changes in the catalog, the docs must change
// in the same commit, and vice versa. Without this test the docs can silently
// drift from the code — a user reads one modules reference page and trusts it
// to know exactly what runs where, so the drift is load-bearing.
//
// Format contract (docs/reference/modules.md):
//
//	### `module_name`
//
//	**Supported runtimes:** `windows-powershell`, `posix-shell`
//
// or, for a module that requires root on POSIX:
//
//	### `module_name`
//
//	**Supported runtimes:** `posix-shell` · **requires root**
//
// Each catalog built-in module MUST carry exactly one "Supported runtimes:"
// line. Runtime kinds are comma-separated backticked tokens. requires_root is
// marked by a trailing `· **requires root**`. Plugins are not catalog built-ins
// and have no matrix row.

// docsModulesPath resolves the modules reference relative to the test file.
// Tests run with the package dir as CWD (internal/target), so the repo root is
// two levels up.
func docsModulesPath(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Join(wd, "..", "..", "docs", "reference", "modules.md")
}

// parseDocsMatrix reads modules.md and returns, per module heading, the
// supported runtimes and requires_root flag declared in its "Supported
// runtimes" line. It t.Fatals on a catalog built-in missing the line or on a
// malformed line so the drift is loud.
func parseDocsMatrix(t *testing.T) map[string]struct {
	runtimes     []string
	requiresRoot bool
} {
	t.Helper()
	data, err := os.ReadFile(docsModulesPath(t))
	if err != nil {
		t.Fatalf("read modules.md: %v", err)
	}
	out := make(map[string]struct {
		runtimes     []string
		requiresRoot bool
	})
	lines := strings.Split(string(data), "\n")
	var current string
	for _, line := range lines {
		heading := parseModuleHeading(line)
		if heading != "" {
			current = heading
			continue
		}
		if current == "" {
			continue
		}
		mods := parseSupportedRuntimesLine(line)
		if mods == nil {
			continue
		}
		if _, dup := out[current]; dup {
			t.Fatalf("module %q: duplicate Supported runtimes line in modules.md", current)
		}
		out[current] = struct {
			runtimes     []string
			requiresRoot bool
		}{runtimes: mods.runtimes, requiresRoot: mods.requiresRoot}
		// A module's matrix line belongs to that module; reset current so a
		// stray second line under the same heading is caught as a duplicate.
		current = ""
	}
	return out
}

// moduleHeading is the parsed result of a "### `name`" heading.
func parseModuleHeading(line string) string {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "### `") {
		return ""
	}
	s = strings.TrimPrefix(s, "### `")
	name, found := strings.CutSuffix(s, "`")
	if !found || strings.Contains(name, "`") {
		return ""
	}
	return name
}

// supportedRuntimesRow is the parsed "Supported runtimes" line.
type supportedRuntimesRow struct {
	runtimes     []string
	requiresRoot bool
}

// parseSupportedRuntimesLine parses a line of the form
// "**Supported runtimes:** `a`, `b` · **requires root**" and returns nil if the
// line is not that shape. The trailing "· **requires root**" marker is
// optional.
func parseSupportedRuntimesLine(line string) *supportedRuntimesRow {
	s := strings.TrimSpace(line)
	const prefix = "**Supported runtimes:**"
	idx := strings.Index(s, prefix)
	if idx != 0 {
		return nil
	}
	rest := strings.TrimSpace(s[len(prefix):])
	requiresRoot := false
	if marker := " · **requires root**"; strings.HasSuffix(rest, marker) {
		requiresRoot = true
		rest = strings.TrimSpace(rest[:len(rest)-len(marker)])
	}
	var runtimes []string
	for field := range strings.SplitSeq(rest, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if len(field) < 2 || field[0] != '`' || field[len(field)-1] != '`' {
			return nil
		}
		runtimes = append(runtimes, field[1:len(field)-1])
	}
	if len(runtimes) == 0 {
		return nil
	}
	return &supportedRuntimesRow{runtimes: runtimes, requiresRoot: requiresRoot}
}

// TestDocsMatrixMatchesCatalog is the drift guard. For every catalog built-in
// module the docs must declare a Supported runtimes line whose set of runtime
// kinds matches CatalogSupportedRuntimes, and whose requires_root marker
// matches CatalogRequiresRoot. Docs-only modules (no catalog entry) are
// ignored so the reference page may carry narrative for non-catalog names.
func TestDocsMatrixMatchesCatalog(t *testing.T) {
	docs := parseDocsMatrix(t)

	// Every catalog built-in must appear in the docs matrix.
	catalogNames := CatalogNames(CapabilityInline | CapabilityRemote)
	for _, name := range catalogNames {
		row, ok := docs[name]
		if !ok {
			t.Errorf("module %q: catalog built-in has no Supported runtimes line in modules.md", name)
			continue
		}
		wantRuntimes := runtimeKindStrings(CatalogSupportedRuntimes(name))
		if !sameSet(row.runtimes, wantRuntimes) {
			t.Errorf("module %q: docs runtimes %v != catalog runtimes %v", name, row.runtimes, wantRuntimes)
		}
		if got, want := row.requiresRoot, CatalogRequiresRoot(name); got != want {
			t.Errorf("module %q: docs requires_root=%v != catalog requires_root=%v", name, got, want)
		}
	}

	// A docs matrix row for a name the catalog does not know is drift too:
	// it implies a documented module that no longer exists.
	for name := range docs {
		if !CatalogKnownModule(name) {
			t.Errorf("module %q: docs declare Supported runtimes but it is not a catalog built-in", name)
		}
	}
}

// TestDocsMatrixRequiresRootPlacedOnPOSIXOnly guards a structural invariant: a
// requires_root marker only makes sense on a module that runs on posix-shell
// (root is a POSIX concept). A marker on a Windows-only module is a docs bug.
func TestDocsMatrixRequiresRootPlacedOnPOSIXOnly(t *testing.T) {
	docs := parseDocsMatrix(t)
	for name, row := range docs {
		if !row.requiresRoot {
			continue
		}
		hasPOSIX := false
		for _, r := range row.runtimes {
			if r == string(RuntimeKindPOSIXShell) {
				hasPOSIX = true
			}
		}
		if !hasPOSIX {
			t.Errorf("module %q: docs mark requires_root but do not list %s as a supported runtime", name, RuntimeKindPOSIXShell)
		}
	}
}

// runtimeKindStrings converts a slice of RuntimeKind to plain strings.
func runtimeKindStrings(kinds []RuntimeKind) []string {
	out := make([]string, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, string(k))
	}
	return out
}

// sameSet reports whether two string slices hold the same elements regardless
// of order.
func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]struct{}, len(a))
	for _, s := range a {
		set[s] = struct{}{}
	}
	for _, s := range b {
		if _, ok := set[s]; !ok {
			return false
		}
	}
	return true
}
