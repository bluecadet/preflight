package inventory_test

import (
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/bluecadet/preflight/internal/inventory"
)

const sampleInventory = `
vars:
  timezone: "America/New_York"
groups:
  lobby:
    vars:
      resolution: "3840x2160"
  windows:
    vars:
      shell: powershell
  gallery:
    vars:
      resolution: "1920x1080"
hosts:
  - name: lobby-pc-01
    address: 192.168.1.10
    transport: winrm
    groups: [lobby, windows]
  - name: lobby-pc-02
    address: 192.168.1.11
    transport: winrm
    groups: [lobby, windows]
  - name: gallery-pc-01
    address: 192.168.1.20
    transport: winrm
    groups: [gallery, windows]
`

func TestParse_HostFirstInventory(t *testing.T) {
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
	if len(inv.Hosts) != 3 {
		t.Fatalf("expected 3 hosts, got %d", len(inv.Hosts))
	}
	if got := inv.Hosts[0].Groups; len(got) != 2 || got[0] != "lobby" || got[1] != "windows" {
		t.Fatalf("unexpected host groups: %#v", got)
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

func TestInventoryVarsMergedIntoHosts(t *testing.T) {
	inv, _ := inventory.Parse([]byte(sampleInventory))
	hosts, _ := inv.HostsForTarget("lobby-pc-01")
	h := hosts[0]
	tz, ok := h.Vars["timezone"]
	if !ok {
		t.Fatal("expected timezone var from inventory vars")
	}
	if tz != "America/New_York" {
		t.Errorf("expected America/New_York, got %v", tz)
	}
}

func TestGroupVarsMergedInHostGroupOrder(t *testing.T) {
	data := `
vars:
  nested:
    base: true
groups:
  first:
    vars:
      role: first
      nested:
        first: true
        shared: first
  second:
    vars:
      role: second
      nested:
        second: true
        shared: second
hosts:
  - name: ordered
    groups: [first, second]
    vars:
      host_var: yes
`
	inv, err := inventory.Parse([]byte(data))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	hosts, err := inv.HostsForTarget("ordered")
	if err != nil {
		t.Fatalf("unexpected target error: %v", err)
	}
	h := hosts[0]
	if got := h.Vars["role"]; got != "second" {
		t.Fatalf("role = %v, want second", got)
	}
	nested, ok := h.Vars["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested is %T, want map[string]any", h.Vars["nested"])
	}
	for _, key := range []string{"base", "first", "second"} {
		if nested[key] != true {
			t.Fatalf("nested[%s] = %v, want true", key, nested[key])
		}
	}
	if nested["shared"] != "second" {
		t.Fatalf("nested.shared = %v, want second", nested["shared"])
	}
}

func TestParse_SchemaValidationFailure(t *testing.T) {
	_, err := inventory.Parse([]byte(`
hosts:
  - name: bad-host
    transport: telnet
`))
	if err == nil {
		t.Fatal("expected schema validation error")
	}
	if !strings.Contains(err.Error(), "inventory: schema validation failed") {
		t.Fatalf("error = %q, want substring %q", err.Error(), "inventory: schema validation failed")
	}
}

func TestParse_UndefinedGroupReference(t *testing.T) {
	_, err := inventory.Parse([]byte(`
hosts:
  - name: kiosk-a
    groups: [missing]
`))
	if err == nil {
		t.Fatal("expected undefined group error")
	}
	if !strings.Contains(err.Error(), `host "kiosk-a" references undefined group "missing"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParse_DuplicateHostName(t *testing.T) {
	_, err := inventory.Parse([]byte(`
hosts:
  - name: kiosk-a
  - name: kiosk-a
`))
	if err == nil {
		t.Fatal("expected duplicate host error")
	}
	if !strings.Contains(err.Error(), `duplicate host name "kiosk-a"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDefaultPort_WinRM(t *testing.T) {
	inv, _ := inventory.Parse([]byte(sampleInventory))
	hosts, _ := inv.HostsForTarget("lobby-pc-01")
	if hosts[0].Port != 5985 {
		t.Errorf("expected default WinRM port 5985, got %d", hosts[0].Port)
	}
}

func TestDefaultPort_WinRMHTTPS(t *testing.T) {
	data := `
hosts:
  - name: https-host
    address: 10.0.0.1
    transport: winrm
    https: true
`
	inv, err := inventory.Parse([]byte(data))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	hosts, err := inv.HostsForTarget("https-host")
	if err != nil {
		t.Fatalf("unexpected target error: %v", err)
	}
	if hosts[0].Port != 5986 {
		t.Errorf("expected default WinRM HTTPS port 5986, got %d", hosts[0].Port)
	}
}

func TestDefaultPort_WinRMHTTPSExplicitPort(t *testing.T) {
	data := `
hosts:
  - name: https-host-explicit
    address: 10.0.0.2
    transport: winrm
    https: true
    port: 9999
`
	inv, err := inventory.Parse([]byte(data))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	hosts, err := inv.HostsForTarget("https-host-explicit")
	if err != nil {
		t.Fatalf("unexpected target error: %v", err)
	}
	if hosts[0].Port != 9999 {
		t.Errorf("expected explicit port 9999, got %d", hosts[0].Port)
	}
}

func TestDefaultPort_Local(t *testing.T) {
	data := `
hosts:
  - name: local-host
    address: localhost
    transport: local
`
	inv, err := inventory.Parse([]byte(data))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	hosts, err := inv.HostsForTarget("local-host")
	if err != nil {
		t.Fatalf("unexpected target error: %v", err)
	}
	if hosts[0].Port != 0 {
		t.Errorf("expected port 0 for local transport, got %d", hosts[0].Port)
	}
}

func TestDefaultTransport(t *testing.T) {
	data := `
hosts:
  - name: default-transport
    address: 10.0.0.1
`
	inv, err := inventory.Parse([]byte(data))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	hosts, err := inv.HostsForTarget("default-transport")
	if err != nil {
		t.Fatalf("unexpected target error: %v", err)
	}
	if hosts[0].Transport != inventory.TransportSSH {
		t.Errorf("expected default transport SSH, got %s", hosts[0].Transport)
	}
}

func TestDefaultPort_DefaultTransport(t *testing.T) {
	data := `
hosts:
  - name: default-transport
    address: 10.0.0.1
`
	inv, err := inventory.Parse([]byte(data))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	hosts, err := inv.HostsForTarget("default-transport")
	if err != nil {
		t.Fatalf("unexpected target error: %v", err)
	}
	if hosts[0].Port != 22 {
		t.Errorf("expected default SSH port 22, got %d", hosts[0].Port)
	}
}

func TestParseSecretReferenceFields(t *testing.T) {
	data := `
groups:
  secure: {}
hosts:
  - name: secure-host
    address: 10.0.0.10
    transport: ssh
    password: secret:winrm-password
    private_key: secret:signage-key
    groups: [secure]
`
	inv, err := inventory.Parse([]byte(data))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	hosts, err := inv.HostsForTarget("secure")
	if err != nil {
		t.Fatalf("unexpected target error: %v", err)
	}
	if got := hosts[0].Password; got != "secret:winrm-password" {
		t.Fatalf("expected password to be preserved, got %q", got)
	}
	if got := hosts[0].PrivateKey; got != "secret:signage-key" {
		t.Fatalf("expected private_key to be preserved, got %q", got)
	}
}

func TestParsePrivateKeyPassphraseField(t *testing.T) {
	data := `
hosts:
  - name: encrypted-key-host
    address: 10.0.0.11
    transport: ssh
    private_key: secret:signage-key
    private_key_passphrase: secret:signage-key-passphrase
`
	inv, err := inventory.Parse([]byte(data))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	hosts, err := inv.HostsForTarget("encrypted-key-host")
	if err != nil {
		t.Fatalf("unexpected target error: %v", err)
	}
	if got := hosts[0].PrivateKeyPassphrase; got != "secret:signage-key-passphrase" {
		t.Fatalf("expected private_key_passphrase to be preserved, got %q", got)
	}
}

func TestParseSSHHostKeyVerificationFields(t *testing.T) {
	data := `
hosts:
  - name: staging-pc-01
    address: 10.1.0.5
    transport: ssh
    known_hosts_file: /home/user/.ssh/known_hosts
    host_key_algorithms:
      - ssh-ed25519
      - ssh-rsa
`
	inv, err := inventory.Parse([]byte(data))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	hosts, err := inv.HostsForTarget("staging-pc-01")
	if err != nil {
		t.Fatalf("unexpected target error: %v", err)
	}
	h := hosts[0]
	if h.KnownHostsFile != "/home/user/.ssh/known_hosts" {
		t.Errorf("expected known_hosts_file to be populated, got %q", h.KnownHostsFile)
	}
	if len(h.HostKeyAlgorithms) != 2 {
		t.Fatalf("expected 2 host_key_algorithms, got %d", len(h.HostKeyAlgorithms))
	}
	if h.HostKeyAlgorithms[0] != "ssh-ed25519" || h.HostKeyAlgorithms[1] != "ssh-rsa" {
		t.Errorf("unexpected host_key_algorithms: %v", h.HostKeyAlgorithms)
	}
}

func TestParseSSHHostKeyPolicyField(t *testing.T) {
	data := `
hosts:
  - name: staging-pc-01
    address: 10.1.0.5
    transport: ssh
    host_key_policy: strict
`
	inv, err := inventory.Parse([]byte(data))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	hosts, err := inv.HostsForTarget("staging-pc-01")
	if err != nil {
		t.Fatalf("unexpected target error: %v", err)
	}
	if got := hosts[0].HostKeyPolicy; got != "strict" {
		t.Errorf("expected host_key_policy to be populated, got %q", got)
	}
}

func TestParseSSHHostKeyPolicyField_Absent(t *testing.T) {
	data := `
hosts:
  - name: staging-pc-01
    address: 10.1.0.5
    transport: ssh
`
	inv, err := inventory.Parse([]byte(data))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	hosts, err := inv.HostsForTarget("staging-pc-01")
	if err != nil {
		t.Fatalf("unexpected target error: %v", err)
	}
	if got := hosts[0].HostKeyPolicy; got != "" {
		t.Errorf("expected empty host_key_policy when absent, got %q", got)
	}
}

// TestParseSSHHostKeyPolicyField_Invalid verifies that an invalid
// host_key_policy value is rejected. Via the public Parse entry point this is
// caught by schema validation (which lists the allowed enum values); the
// parser's own switch-based check in ParseNode is a defense-in-depth guard
// for callers that decode a node without running schema validation first.
func TestParseSSHHostKeyPolicyField_Invalid(t *testing.T) {
	data := `
hosts:
  - name: staging-pc-01
    address: 10.1.0.5
    transport: ssh
    host_key_policy: bogus
`
	_, err := inventory.Parse([]byte(data))
	if err == nil {
		t.Fatal("expected error for invalid host_key_policy, got nil")
	}
	if !strings.Contains(err.Error(), "host_key_policy") || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestParseSSHHostKeyPolicyField_InvalidViaParseNode exercises the parser's
// own host_key_policy validation directly (bypassing schema validation) to
// confirm the "inventory: host %q: invalid host_key_policy %q" error shape.
func TestParseSSHHostKeyPolicyField_InvalidViaParseNode(t *testing.T) {
	data := `
hosts:
  - name: staging-pc-01
    address: 10.1.0.5
    transport: ssh
    host_key_policy: bogus
`
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(data), &root); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	_, err := inventory.ParseNode(root.Content[0])
	if err == nil {
		t.Fatal("expected error for invalid host_key_policy, got nil")
	}
	if !strings.Contains(err.Error(), `host "staging-pc-01": invalid host_key_policy "bogus"`) {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestParseHostTimeout(t *testing.T) {
	data := `
hosts:
  - name: staging-pc-01
    address: 10.1.0.5
    transport: ssh
    timeout: 45s
`
	inv, err := inventory.Parse([]byte(data))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	hosts, err := inv.HostsForTarget("staging-pc-01")
	if err != nil {
		t.Fatalf("unexpected target error: %v", err)
	}
	if got, want := hosts[0].Timeout, 45*time.Second; got != want {
		t.Errorf("expected timeout %s, got %s", want, got)
	}
}

func TestParseHostTimeout_Absent(t *testing.T) {
	data := `
hosts:
  - name: staging-pc-01
    address: 10.1.0.5
    transport: ssh
`
	inv, err := inventory.Parse([]byte(data))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	hosts, err := inv.HostsForTarget("staging-pc-01")
	if err != nil {
		t.Fatalf("unexpected target error: %v", err)
	}
	if hosts[0].Timeout != 0 {
		t.Errorf("expected zero timeout when absent, got %s", hosts[0].Timeout)
	}
}

func TestParseHostTimeout_Invalid(t *testing.T) {
	data := `
hosts:
  - name: staging-pc-01
    address: 10.1.0.5
    transport: ssh
    timeout: not-a-duration
`
	_, err := inventory.Parse([]byte(data))
	if err == nil {
		t.Fatal("expected error for invalid timeout, got nil")
	}
	if !strings.Contains(err.Error(), `host "staging-pc-01": invalid timeout "not-a-duration"`) {
		t.Errorf("unexpected error message: %v", err)
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

func TestHostGroupsPreserved(t *testing.T) {
	inv, err := inventory.Parse([]byte(sampleInventory))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	hosts, err := inv.HostsForTarget("lobby-pc-01")
	if err != nil {
		t.Fatalf("unexpected target error: %v", err)
	}
	if got := hosts[0].Groups; len(got) != 2 || got[0] != "lobby" || got[1] != "windows" {
		t.Fatalf("unexpected groups: %#v", got)
	}
}
