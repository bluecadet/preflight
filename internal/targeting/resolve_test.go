package targeting_test

import (
	"context"
	"testing"
	"time"

	"github.com/bluecadet/preflight/internal/inventory"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/targeting"
)

func TestBuildTargetSSH_PassesHostKeyFields(t *testing.T) {
	data := `
groups:
  staging:
    vars:
      area: staging
hosts:
  - name: staging-pc-01
    address: 10.1.0.5
    transport: ssh
    username: exhibit
    known_hosts_file: /home/user/.ssh/known_hosts
    host_key_algorithms:
      - ssh-ed25519
      - ssh-rsa
    groups: [staging]
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

func TestBuildTargetSSH_PassesTimeout(t *testing.T) {
	data := `
hosts:
  - name: staging-pc-01
    address: 10.1.0.5
    transport: ssh
    username: exhibit
    timeout: 45s
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

	sshTgt, ok := resolved[0].Target.(*target.SSHTarget)
	if !ok {
		t.Fatalf("expected target to be *target.SSHTarget, got %T", resolved[0].Target)
	}
	if got, want := sshTgt.Config().Timeout, 45*time.Second; got != want {
		t.Errorf("SSHConfig.Timeout: got %s, want %s", got, want)
	}
}

func TestResolveHosts_EmptySelectorsDefaultToAllHosts(t *testing.T) {
	inv := &inventory.Inventory{
		Groups: map[string]inventory.Group{},
		Hosts: []inventory.Host{
			{Name: "kiosk-a", Transport: inventory.TransportLocal},
			{Name: "kiosk-b", Transport: inventory.TransportLocal},
		},
	}

	resolved, err := targeting.ResolveHosts(context.Background(), inv, nil, nil, nil, "")
	if err != nil {
		t.Fatalf("ResolveHosts: %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(resolved))
	}
	if resolved[0].Name != "kiosk-a" || resolved[1].Name != "kiosk-b" {
		t.Fatalf("unexpected host order: %#v", []string{resolved[0].Name, resolved[1].Name})
	}
}

func TestResolveHosts_GroupAndHostSelectorsDeduplicate(t *testing.T) {
	inv := &inventory.Inventory{
		Groups: map[string]inventory.Group{
			"lobby": {Name: "lobby"},
		},
		Hosts: []inventory.Host{
			{Name: "kiosk-a", Transport: inventory.TransportLocal, Groups: []string{"lobby"}},
			{Name: "kiosk-b", Transport: inventory.TransportLocal, Groups: []string{"lobby"}},
		},
	}

	resolved, err := targeting.ResolveHosts(context.Background(), inv, []string{"lobby", "kiosk-a"}, nil, nil, "")
	if err != nil {
		t.Fatalf("ResolveHosts: %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("expected 2 deduplicated hosts, got %d", len(resolved))
	}
	if resolved[0].Name != "kiosk-a" || resolved[1].Name != "kiosk-b" {
		t.Fatalf("unexpected host order: %#v", []string{resolved[0].Name, resolved[1].Name})
	}
}
