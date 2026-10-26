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
			Name:     "Windows 10",
			Version:  "10.0.19041",
			Build:    19041,
			Arch:     "amd64",
			Hostname: "exhibit-pc-01",
			Family:   "windows",
		},
		Hostname: "exhibit-pc-01",
	}

	m := f.AsMap()

	osMap, ok := m["os"].(map[string]any)
	if !ok {
		t.Fatal("expected m[\"os\"] to be map")
	}
	for _, key := range []string{"name", "version", "build", "arch", "hostname", "family"} {
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

// TestGatherOS_Windows verifies name/build derive from the version/build
// strings.
func TestGatherOS_Windows(t *testing.T) {
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
