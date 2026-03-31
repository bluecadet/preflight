package facts_test

import (
	"testing"

	"github.com/claytercek/preflight/internal/facts"
)

func TestAsMap_OSKeys(t *testing.T) {
	f := &facts.Facts{
		OS: facts.OSFacts{
			Name:     "Windows 10",
			Version:  "10.0.19041",
			Build:    19041,
			Arch:     "amd64",
			Hostname: "exhibit-pc-01",
		},
		Hostname: "exhibit-pc-01",
	}

	m := f.AsMap()

	osMap, ok := m["os"].(map[string]interface{})
	if !ok {
		t.Fatal("expected m[\"os\"] to be map")
	}
	for _, key := range []string{"name", "version", "build", "arch"} {
		if _, ok := osMap[key]; !ok {
			t.Errorf("expected os key %q", key)
		}
	}
	if osMap["build"] != 19041 {
		t.Errorf("expected build=19041, got %v", osMap["build"])
	}
}

func TestAsMap_Hostname(t *testing.T) {
	f := &facts.Facts{Hostname: "test-pc"}
	m := f.AsMap()
	if m["hostname"] != "test-pc" {
		t.Errorf("expected hostname=test-pc, got %v", m["hostname"])
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
	disks, ok := m["disks"].([]map[string]interface{})
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
