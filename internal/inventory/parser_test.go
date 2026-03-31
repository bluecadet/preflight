package inventory_test

import (
	"os"
	"testing"

	"github.com/bluecadet/preflight/internal/inventory"
)

const sampleInventory = `
groups:
  all:
    vars:
      timezone: "America/New_York"
  lobby:
    vars:
      resolution: "3840x2160"
    hosts:
      - name: lobby-pc-01
        address: 192.168.1.10
        transport: winrm
      - name: lobby-pc-02
        address: 192.168.1.11
        transport: winrm
  gallery:
    vars:
      resolution: "1920x1080"
    hosts:
      - name: gallery-pc-01
        address: 192.168.1.20
        transport: winrm
`

func TestParse_Groups(t *testing.T) {
	inv, err := inventory.Parse([]byte(sampleInventory))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if _, ok := inv.Groups["lobby"]; !ok {
		t.Error("expected lobby group")
	}
	if _, ok := inv.Groups["gallery"]; !ok {
		t.Error("expected gallery group")
	}
	if len(inv.GroupOrder) != 3 {
		t.Fatalf("expected 3 ordered groups, got %d", len(inv.GroupOrder))
	}
	if inv.GroupOrder[0] != "all" || inv.GroupOrder[1] != "lobby" || inv.GroupOrder[2] != "gallery" {
		t.Fatalf("unexpected group order: %#v", inv.GroupOrder)
	}
}

func TestHostsForTarget_Group(t *testing.T) {
	inv, _ := inventory.Parse([]byte(sampleInventory))
	hosts, err := inv.HostsForTarget("lobby")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hosts) != 2 {
		t.Errorf("expected 2 lobby hosts, got %d", len(hosts))
	}
	for _, h := range hosts {
		if h.Name != "lobby-pc-01" && h.Name != "lobby-pc-02" {
			t.Errorf("unexpected host: %s", h.Name)
		}
	}
}

func TestHostsForTarget_HostName(t *testing.T) {
	inv, _ := inventory.Parse([]byte(sampleInventory))
	hosts, err := inv.HostsForTarget("lobby-pc-01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	if hosts[0].Name != "lobby-pc-01" {
		t.Errorf("unexpected host: %s", hosts[0].Name)
	}
}

func TestHostsForTarget_All(t *testing.T) {
	inv, _ := inventory.Parse([]byte(sampleInventory))
	hosts, err := inv.HostsForTarget("all")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hosts) != 3 {
		t.Errorf("expected 3 hosts, got %d", len(hosts))
	}
}

func TestHostsForTarget_Unknown(t *testing.T) {
	inv, _ := inventory.Parse([]byte(sampleInventory))
	_, err := inv.HostsForTarget("nonexistent")
	if err == nil {
		t.Error("expected error for unknown target")
	}
}

func TestAllGroupVarsMergedIntoHosts(t *testing.T) {
	inv, _ := inventory.Parse([]byte(sampleInventory))
	hosts, _ := inv.HostsForTarget("lobby-pc-01")
	h := hosts[0]
	tz, ok := h.Vars["timezone"]
	if !ok {
		t.Fatal("expected timezone var from 'all' group")
	}
	if tz != "America/New_York" {
		t.Errorf("expected America/New_York, got %v", tz)
	}
}

func TestGroupVarsMergedIntoHosts(t *testing.T) {
	inv, _ := inventory.Parse([]byte(sampleInventory))
	hosts, _ := inv.HostsForTarget("lobby-pc-01")
	h := hosts[0]
	res, ok := h.Vars["resolution"]
	if !ok {
		t.Fatal("expected resolution var from lobby group")
	}
	if res != "3840x2160" {
		t.Errorf("expected 3840x2160, got %v", res)
	}
}

func TestParseFile(t *testing.T) {
	f, err := os.CreateTemp("", "inventory-*.yml")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(f.Name()) })
	if _, err := f.WriteString(sampleInventory); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	inv, err := inventory.ParseFile(f.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inv.Groups) == 0 {
		t.Error("expected groups")
	}
}

func TestDefaultPort_WinRM(t *testing.T) {
	inv, _ := inventory.Parse([]byte(sampleInventory))
	hosts, _ := inv.HostsForTarget("lobby-pc-01")
	if hosts[0].Port != 5985 {
		t.Errorf("expected default WinRM port 5985, got %d", hosts[0].Port)
	}
}

func TestParseSecretReferenceFields(t *testing.T) {
	data := `
groups:
  secure:
    hosts:
      - name: secure-host
        address: 10.0.0.10
        transport: ssh
        password_from: secret:winrm-password
        private_key_from: secret:signage-key
`
	inv, err := inventory.Parse([]byte(data))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	hosts, err := inv.HostsForTarget("secure")
	if err != nil {
		t.Fatalf("unexpected target error: %v", err)
	}
	if got := hosts[0].PasswordFrom; got != "secret:winrm-password" {
		t.Fatalf("expected password_from to be preserved, got %q", got)
	}
	if got := hosts[0].PrivateKeyFrom; got != "secret:signage-key" {
		t.Fatalf("expected private_key_from to be preserved, got %q", got)
	}
}

func TestSelectTargets_DedupesInSelectorOrder(t *testing.T) {
	inv, err := inventory.Parse([]byte(sampleInventory))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	hosts, err := inv.SelectTargets([]string{"lobby-pc-02", "lobby", "gallery"})
	if err != nil {
		t.Fatalf("unexpected target error: %v", err)
	}

	got := make([]string, 0, len(hosts))
	for _, host := range hosts {
		got = append(got, host.Name)
	}
	want := []string{"lobby-pc-02", "lobby-pc-01", "gallery-pc-01"}
	if len(got) != len(want) {
		t.Fatalf("got %d hosts, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("host[%d] = %q, want %q (all=%v)", i, got[i], want[i], got)
		}
	}
}
