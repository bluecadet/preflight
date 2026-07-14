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

func TestParse_HostPlatform(t *testing.T) {
	inv, err := inventory.Parse([]byte(`
hosts:
  - name: ts1
    transport: local
    platform:
      os: windows
      arch: amd64
`))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	host := inv.Hosts[0]
	if host.Platform == nil {
		t.Fatal("expected declared platform")
	}
	if got := host.Platform.OS; got != "windows" {
		t.Fatalf("platform OS = %q, want windows", got)
	}
	if got := host.Platform.Arch; got != "amd64" {
		t.Fatalf("platform arch = %q, want amd64", got)
	}
}

func TestParse_HostPlatformRequiresOSAndArch(t *testing.T) {
	tests := map[string]string{
		"missing os": `
hosts:
  - name: ts1
    platform:
      arch: amd64
`,
		"missing arch": `
hosts:
  - name: ts1
    platform:
      os: windows
`,
	}

	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := inventory.Parse([]byte(data))
			if err == nil {
				t.Fatal("expected schema validation error")
			}
			if !strings.Contains(err.Error(), "inventory: schema validation failed") {
				t.Fatalf("error = %q, want schema validation failure", err)
			}
		})
	}
}

func TestParse_HostPlatformRejectsInvalidValues(t *testing.T) {
	tests := map[string]string{
		"unsupported os": `
hosts:
  - name: ts1
    platform:
      os: freebsd
      arch: amd64
`,
		"unsupported arch": `
hosts:
  - name: ts1
    platform:
      os: windows
      arch: "386"
`,
		"unknown field": `
hosts:
  - name: ts1
    platform:
      os: windows
      arch: amd64
      runtime: windows-powershell
`,
	}

	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := inventory.Parse([]byte(data))
			if err == nil {
				t.Fatal("expected schema validation error")
			}
			if !strings.Contains(err.Error(), "inventory: schema validation failed") {
				t.Fatalf("error = %q, want schema validation failure", err)
			}
		})
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

func TestParseJumpBlock_RoundTrips(t *testing.T) {
	data := `
hosts:
  - name: staging-pc-01
    address: 10.1.0.5
    transport: ssh
    username: exhibit
    jump:
      address: bastion.example.com
      port: 2222
      username: jumpuser
      password: secret:jump-password
      private_key: secret:jump-key
      private_key_passphrase: secret:jump-key-passphrase
      known_hosts_file: /home/user/.ssh/jump_known_hosts
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
	jump := hosts[0].Jump
	if jump == nil {
		t.Fatal("expected Jump to be populated")
	}
	if jump.Address != "bastion.example.com" {
		t.Errorf("Jump.Address: got %q, want %q", jump.Address, "bastion.example.com")
	}
	if jump.Port != 2222 {
		t.Errorf("Jump.Port: got %d, want 2222", jump.Port)
	}
	if jump.Username != "jumpuser" {
		t.Errorf("Jump.Username: got %q, want %q", jump.Username, "jumpuser")
	}
	if jump.Password != "secret:jump-password" {
		t.Errorf("Jump.Password: got %q, want %q", jump.Password, "secret:jump-password")
	}
	if jump.PrivateKey != "secret:jump-key" {
		t.Errorf("Jump.PrivateKey: got %q, want %q", jump.PrivateKey, "secret:jump-key")
	}
	if jump.PrivateKeyPassphrase != "secret:jump-key-passphrase" {
		t.Errorf("Jump.PrivateKeyPassphrase: got %q, want %q", jump.PrivateKeyPassphrase, "secret:jump-key-passphrase")
	}
	if jump.KnownHostsFile != "/home/user/.ssh/jump_known_hosts" {
		t.Errorf("Jump.KnownHostsFile: got %q, want %q", jump.KnownHostsFile, "/home/user/.ssh/jump_known_hosts")
	}
	if jump.HostKeyPolicy != "strict" {
		t.Errorf("Jump.HostKeyPolicy: got %q, want %q", jump.HostKeyPolicy, "strict")
	}
}

func TestParseJumpBlock_Absent(t *testing.T) {
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
	if hosts[0].Jump != nil {
		t.Errorf("expected Jump to be nil when absent, got %#v", hosts[0].Jump)
	}
}

func TestParseJumpBlock_MissingAddressErrors(t *testing.T) {
	data := `
hosts:
  - name: staging-pc-01
    address: 10.1.0.5
    transport: ssh
    jump:
      username: jumpuser
`
	_, err := inventory.Parse([]byte(data))
	if err == nil {
		t.Fatal("expected error for jump block missing address, got nil")
	}
	if !strings.Contains(err.Error(), "jump") || !strings.Contains(err.Error(), "address") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestParseJumpBlock_InvalidHostKeyPolicyErrors(t *testing.T) {
	data := `
hosts:
  - name: staging-pc-01
    address: 10.1.0.5
    transport: ssh
    jump:
      address: bastion.example.com
      host_key_policy: bogus
`
	_, err := inventory.Parse([]byte(data))
	if err == nil {
		t.Fatal("expected error for invalid jump host_key_policy, got nil")
	}
	if !strings.Contains(err.Error(), "host_key_policy") || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestParseJumpBlock_InvalidHostKeyPolicyViaParseNode exercises the parser's
// own jump host_key_policy validation directly (bypassing schema validation)
// to confirm the "jump host_key_policy" error shape, mirroring
// TestParseSSHHostKeyPolicyField_InvalidViaParseNode for the host-level
// check.
func TestParseJumpBlock_InvalidHostKeyPolicyViaParseNode(t *testing.T) {
	data := `
hosts:
  - name: staging-pc-01
    address: 10.1.0.5
    transport: ssh
    jump:
      address: bastion.example.com
      host_key_policy: bogus
`
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(data), &root); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	_, err := inventory.ParseNode(root.Content[0])
	if err == nil {
		t.Fatal("expected error for invalid jump host_key_policy, got nil")
	}
	if !strings.Contains(err.Error(), `host "staging-pc-01": invalid jump host_key_policy "bogus"`) {
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

func TestParseHostTimeout_NonPositive(t *testing.T) {
	tests := []struct {
		name    string
		timeout string
	}{
		{name: "negative", timeout: "-30s"},
		{name: "zero", timeout: "0s"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := `
hosts:
  - name: staging-pc-01
    address: 10.1.0.5
    transport: ssh
    timeout: ` + tc.timeout + `
`
			_, err := inventory.Parse([]byte(data))
			if err == nil {
				t.Fatal("expected error for non-positive timeout, got nil")
			}
			if !strings.Contains(err.Error(), `host "staging-pc-01": timeout must be positive, got "`+tc.timeout+`"`) {
				t.Errorf("unexpected error message: %v", err)
			}
		})
	}
}

func TestParseHost_AllFieldsEndToEnd(t *testing.T) {
	data := `
groups:
  secure: {}
hosts:
  - name: staging-pc-01
    address: 10.1.0.5
    transport: ssh
    port: 2200
    username: exhibit
    password: secret:host-password
    private_key: secret:host-key
    private_key_passphrase: secret:host-key-passphrase
    known_hosts_file: /home/user/.ssh/known_hosts
    host_key_policy: strict
    host_key_algorithms: [ssh-ed25519]
    groups: [secure]
    timeout: 45s
    jump:
      address: bastion.example.com
      port: 2222
      username: jumpuser
      host_key_policy: accept-new
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

	if h.Port != 2200 {
		t.Errorf("Port: got %d, want 2200", h.Port)
	}
	if h.Username != "exhibit" {
		t.Errorf("Username: got %q, want %q", h.Username, "exhibit")
	}
	if h.Password != "secret:host-password" {
		t.Errorf("Password: got %q, want %q", h.Password, "secret:host-password")
	}
	if h.PrivateKey != "secret:host-key" {
		t.Errorf("PrivateKey: got %q, want %q", h.PrivateKey, "secret:host-key")
	}
	if h.PrivateKeyPassphrase != "secret:host-key-passphrase" {
		t.Errorf("PrivateKeyPassphrase: got %q, want %q", h.PrivateKeyPassphrase, "secret:host-key-passphrase")
	}
	if h.KnownHostsFile != "/home/user/.ssh/known_hosts" {
		t.Errorf("KnownHostsFile: got %q, want %q", h.KnownHostsFile, "/home/user/.ssh/known_hosts")
	}
	if h.HostKeyPolicy != "strict" {
		t.Errorf("HostKeyPolicy: got %q, want %q", h.HostKeyPolicy, "strict")
	}
	if len(h.HostKeyAlgorithms) != 1 || h.HostKeyAlgorithms[0] != "ssh-ed25519" {
		t.Errorf("HostKeyAlgorithms: got %v, want [ssh-ed25519]", h.HostKeyAlgorithms)
	}
	if len(h.Groups) != 1 || h.Groups[0] != "secure" {
		t.Errorf("Groups: got %v, want [secure]", h.Groups)
	}
	if h.Timeout != 45*time.Second {
		t.Errorf("Timeout: got %s, want %s", h.Timeout, 45*time.Second)
	}
	if h.Jump == nil {
		t.Fatal("expected Jump to be populated")
	}
	if h.Jump.Address != "bastion.example.com" {
		t.Errorf("Jump.Address: got %q, want %q", h.Jump.Address, "bastion.example.com")
	}
	if h.Jump.Port != 2222 {
		t.Errorf("Jump.Port: got %d, want 2222", h.Jump.Port)
	}
	if h.Jump.Username != "jumpuser" {
		t.Errorf("Jump.Username: got %q, want %q", h.Jump.Username, "jumpuser")
	}
	if h.Jump.HostKeyPolicy != "accept-new" {
		t.Errorf("Jump.HostKeyPolicy: got %q, want %q", h.Jump.HostKeyPolicy, "accept-new")
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
