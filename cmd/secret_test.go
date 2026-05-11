package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/config"
	"github.com/bluecadet/preflight/internal/secrets"
)

func TestRunSecretIdentityGenerateWritesPrivateIdentity(t *testing.T) {
	dir := t.TempDir()
	identityPath := filepath.Join(dir, ".age", "keys.txt")
	cmd := newSecretIdentityGenerateTestCommand()
	if err := cmd.Flags().Set("out", identityPath); err != nil {
		t.Fatalf("Set out: %v", err)
	}

	out, err := captureStdout(t, func() error {
		return runSecretIdentityGenerate(cmd, nil)
	})
	if err != nil {
		t.Fatalf("runSecretIdentityGenerate: %v", err)
	}
	if !strings.Contains(out, "Wrote identity to "+identityPath) || !strings.Contains(out, "Public recipient: age1") {
		t.Fatalf("unexpected output: %q", out)
	}
	info, err := os.Stat(identityPath)
	if err != nil {
		t.Fatalf("Stat(identity): %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("identity mode = %o, want 600", got)
	}
	data, err := os.ReadFile(identityPath)
	if err != nil {
		t.Fatalf("ReadFile(identity): %v", err)
	}
	if _, err := age.ParseIdentities(bytes.NewReader(data)); err != nil {
		t.Fatalf("generated identity did not parse: %v", err)
	}

	if _, err := captureStdout(t, func() error {
		return runSecretIdentityGenerate(cmd, nil)
	}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected overwrite refusal, got %v", err)
	}
}

func TestRunSecretIdentityRecipientPrintsRecipientsInFileOrder(t *testing.T) {
	dir := t.TempDir()
	first, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("Generate first identity: %v", err)
	}
	second, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("Generate second identity: %v", err)
	}
	identityPath := filepath.Join(dir, "keys.txt")
	contents := strings.Join([]string{
		"# identities",
		first.String(),
		"",
		second.String(),
	}, "\n")
	if err := os.WriteFile(identityPath, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(identity): %v", err)
	}

	out, err := captureStdout(t, func() error {
		return runSecretIdentityRecipient(nil, []string{identityPath})
	})
	if err != nil {
		t.Fatalf("runSecretIdentityRecipient: %v", err)
	}
	want := first.Recipient().String() + "\n" + second.Recipient().String() + "\n"
	if out != want {
		t.Fatalf("recipients output = %q, want %q", out, want)
	}
}

func TestRunSecretRekeyReencryptsAllSecretsAndSavesOverrides(t *testing.T) {
	dir := t.TempDir()
	restore := chdirForTest(t, dir)
	defer restore()

	_, _, newIdentity := writeRekeyProject(t, dir, []string{"api-token", "db-password"})

	cmd := newSecretRekeyTestCommand()
	if err := cmd.Flags().Set("recipient", newIdentity.Recipient().String()); err != nil {
		t.Fatalf("Set recipient: %v", err)
	}

	out, err := captureStdout(t, func() error {
		return runSecretRekey(cmd, nil)
	})
	if err != nil {
		t.Fatalf("runSecretRekey: %v", err)
	}
	if !strings.Contains(out, "Rekeyed secret \"api-token\"") || !strings.Contains(out, "Rekeyed secret \"db-password\"") {
		t.Fatalf("unexpected output: %q", out)
	}

	apiPlaintext := decryptAgeFile(t, filepath.Join(dir, "secrets", "api-token.age"), newIdentity)
	if string(apiPlaintext) != "api-secret" {
		t.Fatalf("api-token plaintext = %q, want api-secret", apiPlaintext)
	}
	dbPlaintext := decryptAgeFile(t, filepath.Join(dir, "secrets", "db-password.age"), newIdentity)
	if string(dbPlaintext) != "db-secret" {
		t.Fatalf("db-password plaintext = %q, want db-secret", dbPlaintext)
	}

	updated, err := config.ParseFile(filepath.Join(dir, config.FileName))
	if err != nil {
		t.Fatalf("ParseFile(config): %v", err)
	}
	if len(updated.Secrets.Recipients) != 1 || updated.Secrets.Recipients[0] != newIdentity.Recipient().String() {
		t.Fatalf("config recipients = %#v, want new recipient", updated.Secrets.Recipients)
	}
}

func TestRunSecretRekeyRejectsOverridesWithNamedSecrets(t *testing.T) {
	dir := t.TempDir()
	restore := chdirForTest(t, dir)
	defer restore()

	_, _, newIdentity := writeRekeyProject(t, dir, []string{"api-token"})

	cmd := newSecretRekeyTestCommand()
	if err := cmd.Flags().Set("recipient", newIdentity.Recipient().String()); err != nil {
		t.Fatalf("Set recipient: %v", err)
	}
	if _, err := captureStdout(t, func() error {
		return runSecretRekey(cmd, []string{"api-token"})
	}); err == nil || !strings.Contains(err.Error(), "overrides require rekeying all configured secrets") {
		t.Fatalf("expected named override rejection, got %v", err)
	}
}

func TestRunSecretRekeyResolvesAllSecretsBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	restore := chdirForTest(t, dir)
	defer restore()

	_, oldIdentity, newIdentity := writeRekeyProject(t, dir, []string{"api-token", "db-password"})
	if err := os.Remove(filepath.Join(dir, "secrets", "db-password.age")); err != nil {
		t.Fatalf("Remove(db-password): %v", err)
	}

	cmd := newSecretRekeyTestCommand()
	if err := cmd.Flags().Set("recipient", newIdentity.Recipient().String()); err != nil {
		t.Fatalf("Set recipient: %v", err)
	}
	if _, err := captureStdout(t, func() error {
		return runSecretRekey(cmd, nil)
	}); err == nil {
		t.Fatal("expected missing second secret to fail")
	}

	apiPlaintext := decryptAgeFile(t, filepath.Join(dir, "secrets", "api-token.age"), oldIdentity)
	if string(apiPlaintext) != "api-secret" {
		t.Fatalf("api-token plaintext = %q, want api-secret", apiPlaintext)
	}
	if _, err := decryptAgeFileErr(filepath.Join(dir, "secrets", "api-token.age"), newIdentity); err == nil {
		t.Fatal("expected api-token to remain encrypted only for old identity")
	}
}

func newSecretIdentityGenerateTestCommand() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("out", "", "")
	return cmd
}

func newSecretRekeyTestCommand() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().StringSlice("recipient", nil, "")
	cmd.Flags().String("identity", "", "")
	return cmd
}

func writeRekeyProject(t *testing.T, dir string, names []string) (*config.Config, *age.X25519Identity, *age.X25519Identity) {
	t.Helper()

	oldIdentity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("Generate old identity: %v", err)
	}
	newIdentity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("Generate new identity: %v", err)
	}
	identityPath := filepath.Join(dir, ".age", "keys.txt")
	if err := os.MkdirAll(filepath.Dir(identityPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(identity dir): %v", err)
	}
	if err := os.WriteFile(identityPath, []byte(oldIdentity.String()+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(identity): %v", err)
	}

	entries := make(map[string]config.SecretEntry, len(names))
	for _, name := range names {
		entries[name] = config.SecretEntry{File: filepath.ToSlash(filepath.Join("secrets", name+".age"))}
	}
	cfg := &config.Config{
		Vars: map[string]any{},
		Secrets: config.SecretsConfig{
			Identity:   identityPath,
			Recipients: []string{oldIdentity.Recipient().String()},
			Entries:    entries,
		},
	}
	if err := config.SaveFile(filepath.Join(dir, config.FileName), cfg); err != nil {
		t.Fatalf("SaveFile(config): %v", err)
	}
	provider := secrets.NewRepoProvider(dir, cfg.Secrets)
	values := map[string]string{
		"api-token":   "api-secret",
		"db-password": "db-secret",
	}
	for _, name := range names {
		value := values[name]
		if value == "" {
			value = name + "-secret"
		}
		if err := provider.Encrypt(name, []byte(value)); err != nil {
			t.Fatalf("Encrypt(%s): %v", name, err)
		}
	}
	return cfg, oldIdentity, newIdentity
}

func decryptAgeFile(t *testing.T, path string, identity age.Identity) []byte {
	t.Helper()
	plaintext, err := decryptAgeFileErr(path, identity)
	if err != nil {
		t.Fatalf("decryptAgeFile(%q): %v", path, err)
	}
	return plaintext
}

func decryptAgeFileErr(path string, identity age.Identity) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	reader, err := age.Decrypt(bytes.NewReader(data), identity)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(reader)
}
