package targeting_test

import (
	"context"
	"testing"

	"github.com/bluecadet/preflight/internal/inventory"
	"github.com/bluecadet/preflight/internal/targeting"
)

func TestBuildTargetSSH_PassesHostKeyFields(t *testing.T) {
	data := `
groups:
  staging:
    hosts:
      - name: staging-pc-01
        address: 10.1.0.5
        transport: ssh
        username: exhibit
        known_hosts_file: /home/user/.ssh/known_hosts
        host_key_algorithms:
          - ssh-ed25519
          - ssh-rsa
`
	inv, err := inventory.Parse([]byte(data))
	if err != nil {
		t.Fatalf("parse inventory: %v", err)
	}

	resolved, err := targeting.ResolveHosts(context.Background(), inv, []string{"staging-pc-01"}, nil, nil, "")
	if err != nil {
		t.Fatalf("ResolveHosts: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved host, got %d", len(resolved))
	}

	ref := resolved[0].InventoryRef
	if ref.KnownHostsFile != "/home/user/.ssh/known_hosts" {
		t.Errorf("expected KnownHostsFile to be propagated, got %q", ref.KnownHostsFile)
	}
	if len(ref.HostKeyAlgorithms) != 2 {
		t.Fatalf("expected 2 HostKeyAlgorithms, got %d", len(ref.HostKeyAlgorithms))
	}
	if ref.HostKeyAlgorithms[0] != "ssh-ed25519" || ref.HostKeyAlgorithms[1] != "ssh-rsa" {
		t.Errorf("unexpected HostKeyAlgorithms: %v", ref.HostKeyAlgorithms)
	}
}
