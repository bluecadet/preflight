package facts_test

import (
	"context"
	"testing"

	"github.com/bluecadet/preflight/internal/facts"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/target/targettest"
)

func TestAsMap_OSKeys(t *testing.T) {
	f := &facts.Facts{
		OS: facts.OSFacts{
			Name:           "Windows 10",
			Version:        "10.0.19041",
			Build:          19041,
			Arch:           "amd64",
			Hostname:       "exhibit-pc-01",
			Family:         "windows",
			PackageManager: "",
			Init:           "",
		},
		Hostname: "exhibit-pc-01",
	}

	m := f.AsMap()

	osMap, ok := m["os"].(map[string]any)
	if !ok {
		t.Fatal("expected m[\"os\"] to be map")
	}
	for _, key := range []string{"name", "version", "build", "arch", "hostname", "family", "package_manager", "init"} {
		if _, ok := osMap[key]; !ok {
			t.Errorf("expected os key %q", key)
		}
	}
	if osMap["build"] != 19041 {
		t.Errorf("expected build=19041, got %v", osMap["build"])
	}
	if osMap["family"] != "windows" {
		t.Errorf("expected family=windows, got %v", osMap["family"])
	}
}

func TestAsMap_Hostname(t *testing.T) {
	f := &facts.Facts{Hostname: "test-pc"}
	m := f.AsMap()
	if m["hostname"] != "test-pc" {
		t.Errorf("expected hostname=test-pc, got %v", m["hostname"])
	}
}

// TestAsMap_AbsentSignalsAreEmpty asserts the empty-string absence contract:
// the POSIX-only facts.os keys are present even when their signal is absent,
// rendering as an empty string (build as 0). This lets playbooks branch on
// {{ facts.os.package_manager }} without distinguishing missing from empty.
func TestAsMap_AbsentSignalsAreEmpty(t *testing.T) {
	f := &facts.Facts{
		OS: facts.OSFacts{
			Hostname: "mbp",
			Family:   "darwin",
			// macOS: no os-release, no apt/dnf, no systemd
		},
	}
	osMap := f.AsMap()["os"].(map[string]any)
	for _, key := range []string{"name", "version", "package_manager", "init", "arch"} {
		v, ok := osMap[key]
		if !ok {
			t.Errorf("expected os key %q to always be present", key)
			continue
		}
		if v != "" {
			t.Errorf("expected absent signal %q to be empty string, got %v", key, v)
		}
	}
	if osMap["build"] != 0 {
		t.Errorf("expected absent build to be 0, got %v", osMap["build"])
	}
	// Present signals are still rendered.
	if osMap["family"] != "darwin" || osMap["hostname"] != "mbp" {
		t.Errorf("expected present signals rendered, got family=%v hostname=%v", osMap["family"], osMap["hostname"])
	}
}

// TestGatherOS_POSIXEnriched verifies the POSIX fact view: name/version come
// from os-release (via the probe-backed TargetInfo), package_manager and init
// are surfaced, and build stays absent (Windows-only).
func TestGatherOS_POSIXEnriched(t *testing.T) {
	g := facts.New(&targettest.Fake{InfoValue: enrichedPOSIXInfo()})
	osFacts, err := g.GatherOS(context.Background())
	if err != nil {
		t.Fatalf("GatherOS: %v", err)
	}
	if osFacts.Name != "ubuntu" {
		t.Errorf("name: got %q, want ubuntu", osFacts.Name)
	}
	if osFacts.Version != "22.04" {
		t.Errorf("version: got %q, want 22.04", osFacts.Version)
	}
	if osFacts.Family != "linux" {
		t.Errorf("family: got %q, want linux", osFacts.Family)
	}
	if osFacts.PackageManager != "apt" {
		t.Errorf("package_manager: got %q, want apt", osFacts.PackageManager)
	}
	if osFacts.Init != "systemd" {
		t.Errorf("init: got %q, want systemd", osFacts.Init)
	}
	if osFacts.Build != 0 {
		t.Errorf("build: got %d, want 0 (POSIX has no build)", osFacts.Build)
	}
}

// TestGatherOS_WindowsUnchanged verifies the Windows fact view is unchanged by
// the POSIX enrichment: name/build still derive from the version/build
// strings, and the POSIX-only fields are empty.
func TestGatherOS_WindowsUnchanged(t *testing.T) {
	g := facts.New(&targettest.Fake{InfoValue: target.TargetInfo{
		Hostname:  "kiosk-a",
		OSVersion: "10.0.19045",
		OSBuild:   "19045",
		Arch:      "amd64",
		OSFamily:  target.OSFamilyWindows,
		Transport: target.TransportWinRM,
	}})
	osFacts, err := g.GatherOS(context.Background())
	if err != nil {
		t.Fatalf("GatherOS: %v", err)
	}
	if osFacts.Name != "Windows 10" {
		t.Errorf("name: got %q, want Windows 10", osFacts.Name)
	}
	if osFacts.Version != "10.0.19045" {
		t.Errorf("version: got %q, want 10.0.19045", osFacts.Version)
	}
	if osFacts.Build != 19045 {
		t.Errorf("build: got %d, want 19045", osFacts.Build)
	}
	if osFacts.Family != "windows" {
		t.Errorf("family: got %q, want windows", osFacts.Family)
	}
	if osFacts.PackageManager != "" || osFacts.Init != "" {
		t.Errorf("expected empty POSIX-only fields on Windows, got pm=%q init=%q", osFacts.PackageManager, osFacts.Init)
	}
}

// TestGatherOS_POSIXAbsentSignals verifies that a POSIX host whose probe found
// nothing (e.g. macOS) yields empty — never missing — POSIX-only facts.
func TestGatherOS_POSIXAbsentSignals(t *testing.T) {
	g := facts.New(&targettest.Fake{InfoValue: target.TargetInfo{
		Hostname:  "mbp",
		OSFamily:  target.OSFamilyDarwin,
		Arch:      "arm64",
		Transport: target.TransportSSH,
	}})
	osFacts, err := g.GatherOS(context.Background())
	if err != nil {
		t.Fatalf("GatherOS: %v", err)
	}
	if osFacts.Name != "" || osFacts.Version != "" || osFacts.PackageManager != "" || osFacts.Init != "" {
		t.Fatalf("expected all absent POSIX signals empty, got %+v", osFacts)
	}
	if osFacts.Family != "darwin" {
		t.Errorf("family: got %q, want darwin", osFacts.Family)
	}
}

func TestParseWindowsDrives_Array(t *testing.T) {
	// This tests the exported path indirectly via AsMap disk entries.
	f := &facts.Facts{
		Disks: []facts.DiskFacts{
			{Path: "C:", TotalGB: 100, FreeGB: 40, UsedGB: 60},
		},
	}
	m := f.AsMap()
	disks, ok := m["disks"].([]map[string]any)
	if !ok {
		t.Fatal("expected disks to be []map")
	}
	if len(disks) != 1 {
		t.Fatalf("expected 1 disk, got %d", len(disks))
	}
	if disks[0]["path"] != "C:" {
		t.Errorf("expected path=C:, got %v", disks[0]["path"])
	}
}

// enrichedPOSIXInfo returns the TargetInfo a POSIX probe-backed target would
// produce for an Ubuntu 22.04 host with apt and systemd.
func enrichedPOSIXInfo() target.TargetInfo {
	return target.TargetInfo{
		Hostname:       "kiosk-a",
		OSVersion:      "22.04",
		OSName:         "ubuntu",
		Arch:           "x86_64",
		OSFamily:       target.OSFamilyLinux,
		PackageManager: "apt",
		Init:           "systemd",
		Transport:      target.TransportSSH,
	}
}
