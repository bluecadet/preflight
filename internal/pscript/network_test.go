package pscript

import (
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runFirewallCheck runs FirewallRuleCheckScript under pwsh against a stubbed
// existing rule and returns the script's verdict.
func runFirewallCheck(t *testing.T, params map[string]any, existingProtocol string, existingPorts []string) string {
	t.Helper()
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("pwsh not installed")
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	portsJSON, err := json.Marshal(existingPorts)
	if err != nil {
		t.Fatal(err)
	}
	script := `
$params = '` + string(paramsJSON) + `' | ConvertFrom-Json
function Get-NetFirewallRule { [pscustomobject]@{ Direction = 'Inbound'; Action = 'Allow' } }
function Get-NetFirewallPortFilter { [pscustomobject]@{ Protocol = '` + existingProtocol + `'; LocalPort = @('` + string(portsJSON) + `' | ConvertFrom-Json) } }
` + FirewallRuleCheckScript
	path := filepath.Join(t.TempDir(), "check.ps1")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(pwsh, "-NoProfile", "-NonInteractive", "-File", path).CombinedOutput()
	if err != nil {
		t.Fatalf("pwsh failed: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestFirewallRuleCheckScriptComparesPortsAsSets(t *testing.T) {
	base := map[string]any{"name": "web", "direction": "inbound", "action": "allow", "protocol": "tcp"}
	for _, tc := range []struct {
		name     string
		ports    []string
		protocol string
		existing []string
		want     string
	}{
		{"same ports reordered", []string{"80", "443"}, "TCP", []string{"443", "80"}, "False"},
		{"single port", []string{"8080"}, "TCP", []string{"8080"}, "False"},
		{"extra port", []string{"80", "443"}, "TCP", []string{"80"}, "True"},
		{"protocol drift", []string{"80"}, "UDP", []string{"80"}, "True"},
	} {
		params := map[string]any{"ports": tc.ports}
		maps.Copy(params, base)
		if got := runFirewallCheck(t, params, tc.protocol, tc.existing); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
