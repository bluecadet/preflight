package secrets_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"

	"github.com/bluecadet/preflight/internal/config"
	"github.com/bluecadet/preflight/internal/secrets"
)

func TestRepoProviderResolve(t *testing.T) {
	dir := t.TempDir()
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("GenerateX25519Identity: %v", err)
	}
	recipient := identity.Recipient()

	identityPath := filepath.Join(dir, "keys.txt")
	if err := os.WriteFile(identityPath, []byte(identity.String()+"\n"), 0o600); err != nil {
		t.Fatalf("write identity: %v", err)
	}

	secretPath := filepath.Join(dir, "secrets", "app-password.age")
	var encrypted bytes.Buffer
	w, err := age.Encrypt(&encrypted, recipient)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := w.Write([]byte("super-secret")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(secretPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(secretPath, encrypted.Bytes(), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	provider := secrets.NewRepoProvider(dir, config.SecretsConfig{
		Identity: identityPath,
		Entries: map[string]config.SecretEntry{
			"app-password": {File: secretPath},
		},
	})

	got, err := provider.Resolve(context.Background(), "app-password")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(got) != "super-secret" {
		t.Fatalf("expected decrypted secret, got %q", string(got))
	}

}

func TestResolverResolveMap(t *testing.T) {
	dir := t.TempDir()
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("GenerateX25519Identity: %v", err)
	}
	recipient := identity.Recipient()

	identityPath := filepath.Join(dir, "keys.txt")
	if err := os.WriteFile(identityPath, []byte(identity.String()+"\n"), 0o600); err != nil {
		t.Fatalf("write identity: %v", err)
	}

	cfg := config.SecretsConfig{
		Identity:   identityPath,
		Recipients: []string{recipient.String()},
		Entries: map[string]config.SecretEntry{
			"db-password": {File: "secrets/db-password.age"},
		},
	}
	provider := secrets.NewRepoProvider(dir, cfg)
	if err := provider.Encrypt("db-password", []byte("hunter2")); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	resolver := secrets.NewResolver(map[string]secrets.Provider{
		secrets.DefaultProviderName: provider,
	})
	params := map[string]any{
		"password_from": "secret:db-password",
		"nested": map[string]any{
			"token": "secret:db-password",
		},
	}

	got, err := resolver.ResolveMap(context.Background(), params)
	if err != nil {
		t.Fatalf("ResolveMap: %v", err)
	}
	if got["password"] != "hunter2" {
		t.Fatalf("expected resolved password, got %#v", got["password"])
	}
	nested, ok := got["nested"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested map, got %T", got["nested"])
	}
	if nested["token"] != "hunter2" {
		t.Fatalf("expected nested token to resolve, got %#v", nested["token"])
	}
}

func TestBundleProviderResolveEncrypted(t *testing.T) {
	dir := t.TempDir()
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("GenerateX25519Identity: %v", err)
	}
	identityPath := filepath.Join(dir, "keys.txt")
	if err := os.WriteFile(identityPath, []byte(identity.String()+"\n"), 0o600); err != nil {
		t.Fatalf("write identity: %v", err)
	}

	secretPath := filepath.Join(dir, "secrets", "db-password.age")
	if err := os.MkdirAll(filepath.Dir(secretPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	var encrypted bytes.Buffer
	w, err := age.Encrypt(&encrypted, identity.Recipient())
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := w.Write([]byte("hunter2")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := os.WriteFile(secretPath, encrypted.Bytes(), 0o600); err != nil {
		t.Fatalf("write encrypted secret: %v", err)
	}

	provider := secrets.NewBundleProvider(dir, true, identityPath, map[string]string{
		"db-password": filepath.ToSlash(filepath.Join("secrets", "db-password.age")),
	})
	got, err := provider.Resolve(context.Background(), "db-password")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(got) != "hunter2" {
		t.Fatalf("expected decrypted bundled secret, got %q", string(got))
	}
}

func TestBundleProviderResolvePlaintext(t *testing.T) {
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "secrets", "db-password")
	if err := os.MkdirAll(filepath.Dir(secretPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(secretPath, []byte("hunter2"), 0o600); err != nil {
		t.Fatalf("write plaintext secret: %v", err)
	}

	provider := secrets.NewBundleProvider(dir, false, "", map[string]string{
		"db-password": filepath.ToSlash(filepath.Join("secrets", "db-password")),
	})
	got, err := provider.Resolve(context.Background(), "db-password")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(got) != "hunter2" {
		t.Fatalf("expected plaintext bundled secret, got %q", string(got))
	}
}

func TestIsRefDoesNotTreatWindowsPathsOrURLsAsSecrets(t *testing.T) {
	cases := []string{
		`C:\Exhibits\Lobby`,
		`https://example.com/app`,
		`mailto:signage@example.com`,
		`user:pass`,
	}

	for _, tc := range cases {
		if secrets.IsRef(tc) {
			t.Fatalf("expected %q to not be treated as a secret ref", tc)
		}
	}

	if !secrets.IsRef("secret:db-password") {
		t.Fatal(`expected "secret:db-password" to be treated as a secret ref`)
	}
}
