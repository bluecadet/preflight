package targeting_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bluecadet/preflight/internal/inventory"
	"github.com/bluecadet/preflight/internal/secrets"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/targeting"
)

// fakeSecretProvider is a minimal secrets.Provider backed by an in-memory
// map, used to exercise secret resolution without touching age/repo-backed
// providers.
type fakeSecretProvider struct {
	values map[string]string
}

func (p *fakeSecretProvider) Resolve(_ context.Context, name string) ([]byte, error) {
	v, ok := p.values[name]
	if !ok {
		return nil, fmt.Errorf("fakeSecretProvider: no such secret %q", name)
	}
	return []byte(v), nil
}

func (p *fakeSecretProvider) List() []string {
	names := make([]string, 0, len(p.values))
	for name := range p.values {
		names = append(names, name)
	}
	return names
}

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

func TestBuildTargetSSH_PassesHostKeyPolicy(t *testing.T) {
	data := `
hosts:
  - name: staging-pc-01
    address: 10.1.0.5
    transport: ssh
    username: exhibit
    host_key_policy: strict
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
	cfg := sshTgt.Config()
	if cfg.HostKeyPolicy != "strict" {
		t.Errorf("SSHConfig.HostKeyPolicy: got %q, want %q", cfg.HostKeyPolicy, "strict")
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

func TestBuildTargetSSH_PassesPrivateKeyPassphrase(t *testing.T) {
	data := `
hosts:
  - name: staging-pc-01
    address: 10.1.0.5
    transport: ssh
    username: exhibit
    private_key: |
      -----BEGIN PLACEHOLDER-----
      not-a-real-key
      -----END PLACEHOLDER-----
    private_key_passphrase: s3cret-passphrase
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
	if got, want := sshTgt.Config().PrivateKeyPassphrase, "s3cret-passphrase"; got != want {
		t.Errorf("SSHConfig.PrivateKeyPassphrase: got %q, want %q", got, want)
	}
}

func TestBuildTargetSSH_PassesJumpFields(t *testing.T) {
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
      password: bastion-pass
      private_key: bastion-key
      private_key_passphrase: bastion-key-pass
      known_hosts_file: /home/user/.ssh/jump_known_hosts
      host_key_policy: strict
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
	jump := sshTgt.Config().Jump
	if jump == nil {
		t.Fatal("expected SSHConfig.Jump to be populated")
	}
	if jump.Host != "bastion.example.com" {
		t.Errorf("Jump.Host: got %q, want %q", jump.Host, "bastion.example.com")
	}
	if jump.Port != 2222 {
		t.Errorf("Jump.Port: got %d, want 2222", jump.Port)
	}
	if jump.Username != "jumpuser" {
		t.Errorf("Jump.Username: got %q, want %q", jump.Username, "jumpuser")
	}
	if jump.Password != "bastion-pass" {
		t.Errorf("Jump.Password: got %q, want %q", jump.Password, "bastion-pass")
	}
	if jump.PrivateKey != "bastion-key" {
		t.Errorf("Jump.PrivateKey: got %q, want %q", jump.PrivateKey, "bastion-key")
	}
	if jump.PrivateKeyPassphrase != "bastion-key-pass" {
		t.Errorf("Jump.PrivateKeyPassphrase: got %q, want %q", jump.PrivateKeyPassphrase, "bastion-key-pass")
	}
	if jump.KnownHostsFile != "/home/user/.ssh/jump_known_hosts" {
		t.Errorf("Jump.KnownHostsFile: got %q, want %q", jump.KnownHostsFile, "/home/user/.ssh/jump_known_hosts")
	}
	if jump.HostKeyPolicy != "strict" {
		t.Errorf("Jump.HostKeyPolicy: got %q, want %q", jump.HostKeyPolicy, "strict")
	}
	if jump.Timeout != 0 {
		t.Errorf("Jump.Timeout: got %s, want 0 (so the 30s default applies)", jump.Timeout)
	}
}

func TestBuildTargetSSH_NoJumpBlockLeavesConfigJumpNil(t *testing.T) {
	data := `
hosts:
  - name: staging-pc-01
    address: 10.1.0.5
    transport: ssh
    username: exhibit
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
	if jump := sshTgt.Config().Jump; jump != nil {
		t.Errorf("expected SSHConfig.Jump to be nil, got %#v", jump)
	}
}

func TestBuildTargetSSH_ResolvesJumpPasswordSecret(t *testing.T) {
	data := `
hosts:
  - name: staging-pc-01
    address: 10.1.0.5
    transport: ssh
    username: exhibit
    jump:
      address: bastion.example.com
      username: jumpuser
      password: secret:jump-password
`
	inv, err := inventory.Parse([]byte(data))
	if err != nil {
		t.Fatalf("parse inventory: %v", err)
	}

	provider := &fakeSecretProvider{values: map[string]string{"jump-password": "hunter2"}}
	resolver := secrets.NewResolver(map[string]secrets.Provider{
		secrets.DefaultProviderName: provider,
	})

	resolved, err := targeting.ResolveHosts(context.Background(), inv, []string{"staging-pc-01"}, nil, resolver, "")
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
	jump := sshTgt.Config().Jump
	if jump == nil {
		t.Fatal("expected SSHConfig.Jump to be populated")
	}
	if jump.Password != "hunter2" {
		t.Errorf("Jump.Password: got %q, want resolved secret %q", jump.Password, "hunter2")
	}
}

func TestBuildTargetSSH_ResolvesJumpPrivateKeyAndPassphraseSecrets(t *testing.T) {
	data := `
hosts:
  - name: staging-pc-01
    address: 10.1.0.5
    transport: ssh
    username: exhibit
    jump:
      address: bastion.example.com
      username: jumpuser
      private_key: secret:jump-private-key
      private_key_passphrase: secret:jump-passphrase
`
	inv, err := inventory.Parse([]byte(data))
	if err != nil {
		t.Fatalf("parse inventory: %v", err)
	}

	provider := &fakeSecretProvider{values: map[string]string{
		"jump-private-key": "-----BEGIN JUMP KEY-----",
		"jump-passphrase":  "jump-hunter2",
	}}
	resolver := secrets.NewResolver(map[string]secrets.Provider{
		secrets.DefaultProviderName: provider,
	})

	resolved, err := targeting.ResolveHosts(context.Background(), inv, []string{"staging-pc-01"}, nil, resolver, "")
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
	jump := sshTgt.Config().Jump
	if jump == nil {
		t.Fatal("expected SSHConfig.Jump to be populated")
	}
	if jump.PrivateKey != "-----BEGIN JUMP KEY-----" {
		t.Errorf("Jump.PrivateKey: got %q, want resolved secret", jump.PrivateKey)
	}
	if jump.PrivateKeyPassphrase != "jump-hunter2" {
		t.Errorf("Jump.PrivateKeyPassphrase: got %q, want resolved secret", jump.PrivateKeyPassphrase)
	}
}

func TestBuildTargetSSH_JumpPlainCredentialsPassThroughResolver(t *testing.T) {
	data := `
hosts:
  - name: staging-pc-01
    address: 10.1.0.5
    transport: ssh
    username: exhibit
    jump:
      address: bastion.example.com
      username: jumpuser
      password: plain-pass
      private_key: plain-key
      private_key_passphrase: plain-passphrase
`
	inv, err := inventory.Parse([]byte(data))
	if err != nil {
		t.Fatalf("parse inventory: %v", err)
	}

	provider := &fakeSecretProvider{values: map[string]string{}}
	resolver := secrets.NewResolver(map[string]secrets.Provider{
		secrets.DefaultProviderName: provider,
	})

	resolved, err := targeting.ResolveHosts(context.Background(), inv, []string{"staging-pc-01"}, nil, resolver, "")
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
	jump := sshTgt.Config().Jump
	if jump == nil {
		t.Fatal("expected SSHConfig.Jump to be populated")
	}
	if jump.Password != "plain-pass" {
		t.Errorf("Jump.Password: got %q, want %q", jump.Password, "plain-pass")
	}
	if jump.PrivateKey != "plain-key" {
		t.Errorf("Jump.PrivateKey: got %q, want %q", jump.PrivateKey, "plain-key")
	}
	if jump.PrivateKeyPassphrase != "plain-passphrase" {
		t.Errorf("Jump.PrivateKeyPassphrase: got %q, want %q", jump.PrivateKeyPassphrase, "plain-passphrase")
	}
}

func TestBuildTargetSSH_JumpSecretResolveErrorIncludesHostName(t *testing.T) {
	data := `
hosts:
  - name: staging-pc-01
    address: 10.1.0.5
    transport: ssh
    username: exhibit
    jump:
      address: bastion.example.com
      username: jumpuser
      password: secret:missing-jump-password
`
	inv, err := inventory.Parse([]byte(data))
	if err != nil {
		t.Fatalf("parse inventory: %v", err)
	}

	provider := &fakeSecretProvider{values: map[string]string{}}
	resolver := secrets.NewResolver(map[string]secrets.Provider{
		secrets.DefaultProviderName: provider,
	})

	_, err = targeting.ResolveHosts(context.Background(), inv, []string{"staging-pc-01"}, nil, resolver, "")
	if err == nil {
		t.Fatal("expected error from unresolved jump secret, got nil")
	}
	if got, want := err.Error(), `resolve host "staging-pc-01" jump auth`; !strings.Contains(got, want) {
		t.Errorf("error %q does not contain %q", got, want)
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
