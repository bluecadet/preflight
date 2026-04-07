package targeting_test

import (
	"context"
	"testing"

	"github.com/bluecadet/preflight/internal/inventory"
	"github.com/bluecadet/preflight/internal/target"
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

	// Verify InventoryRef fields are populated.
	ref := resolved[0].InventoryRef
	if ref.KnownHostsFile != "/home/user/.ssh/known_hosts" {
		t.Errorf("InventoryRef.KnownHostsFile: got %q, want %q", ref.KnownHostsFile, "/home/user/.ssh/known_hosts")
	}
	if len(ref.HostKeyAlgorithms) != 2 {
		t.Fatalf("InventoryRef.HostKeyAlgorithms: got %d entries, want 2", len(ref.HostKeyAlgorithms))
	}
	if ref.HostKeyAlgorithms[0] != "ssh-ed25519" || ref.HostKeyAlgorithms[1] != "ssh-rsa" {
		t.Errorf("InventoryRef.HostKeyAlgorithms: got %v", ref.HostKeyAlgorithms)
	}

	// Verify the fields are wired through to the constructed SSHTarget.Config
	// so that a regression in buildTarget is caught here, not silently at
	// connection time.
	sshTgt, ok := resolved[0].Target.(*target.SSHTarget)
	if !ok {
		t.Fatalf("expected target to be *target.SSHTarget, got %T", resolved[0].Target)
	}
	cfg := sshTgt.Config()
	if cfg.KnownHostsFile != "/home/user/.ssh/known_hosts" {
		t.Errorf("SSHConfig.KnownHostsFile: got %q, want %q", cfg.KnownHostsFile, "/home/user/.ssh/known_hosts")
	}
	if len(cfg.HostKeyAlgorithms) != 2 {
		t.Fatalf("SSHConfig.HostKeyAlgorithms: got %d entries, want 2", len(cfg.HostKeyAlgorithms))
	}
	if cfg.HostKeyAlgorithms[0] != "ssh-ed25519" || cfg.HostKeyAlgorithms[1] != "ssh-rsa" {
		t.Errorf("SSHConfig.HostKeyAlgorithms: got %v", cfg.HostKeyAlgorithms)
	}
}
