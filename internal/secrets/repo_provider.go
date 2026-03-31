package secrets

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"filippo.io/age"

	"github.com/bluecadet/preflight/internal/config"
)

// RepoProvider stores and resolves age-encrypted secrets from the repo.
type RepoProvider struct {
	rootDir string
	cfg     config.SecretsConfig
}

// NewRepoProvider creates a repo-backed secrets provider rooted at rootDir.
func NewRepoProvider(rootDir string, cfg config.SecretsConfig) *RepoProvider {
	return &RepoProvider{
		rootDir: rootDir,
		cfg:     cfg,
	}
}

// Resolve decrypts the named secret using the configured age identity file.
func (p *RepoProvider) Resolve(_ context.Context, name string) ([]byte, error) {
	entry, ok := p.cfg.Entries[name]
	if !ok {
		return nil, fmt.Errorf("secret %q is not defined in preflight.yml", name)
	}
	identities, err := p.loadIdentities()
	if err != nil {
		return nil, err
	}
	path := p.absPath(entry.File)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read encrypted secret %q: %w", path, err)
	}
	reader, err := age.Decrypt(bytes.NewReader(data), identities...)
	if err != nil {
		return nil, fmt.Errorf("decrypt %q: %w", path, err)
	}
	plaintext, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read decrypted secret %q: %w", path, err)
	}
	return plaintext, nil
}

// Encrypt encrypts plaintext into the configured file for name.
func (p *RepoProvider) Encrypt(name string, plaintext []byte) error {
	entry, ok := p.cfg.Entries[name]
	if !ok {
		return fmt.Errorf("secret %q is not defined in preflight.yml", name)
	}
	recipients, err := p.loadRecipients()
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipients...)
	if err != nil {
		return fmt.Errorf("encrypt %q: %w", name, err)
	}
	if _, err := w.Write(plaintext); err != nil {
		return fmt.Errorf("encrypt %q: %w", name, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("encrypt %q: %w", name, err)
	}
	path := p.absPath(entry.File)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %q: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write encrypted secret %q: %w", path, err)
	}
	return nil
}

// List returns all configured secret names.
func (p *RepoProvider) List() []string {
	names := make([]string, 0, len(p.cfg.Entries))
	for name := range p.cfg.Entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (p *RepoProvider) absPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(p.rootDir, path)
}

func (p *RepoProvider) loadIdentities() ([]age.Identity, error) {
	if p.cfg.Identity == "" {
		return nil, fmt.Errorf("no secrets.identity configured in preflight.yml")
	}
	f, err := os.Open(p.absPath(p.cfg.Identity))
	if err != nil {
		return nil, fmt.Errorf("open identity file: %w", err)
	}
	defer f.Close()
	identities, err := age.ParseIdentities(f)
	if err != nil {
		return nil, fmt.Errorf("parse identity file: %w", err)
	}
	if len(identities) == 0 {
		return nil, fmt.Errorf("identity file did not contain any age identities")
	}
	return identities, nil
}

func (p *RepoProvider) loadRecipients() ([]age.Recipient, error) {
	if len(p.cfg.Recipients) == 0 {
		return nil, fmt.Errorf("no secrets.recipients configured in preflight.yml")
	}
	recipients := make([]age.Recipient, 0, len(p.cfg.Recipients))
	for _, encoded := range p.cfg.Recipients {
		recipient, err := age.ParseX25519Recipient(encoded)
		if err != nil {
			return nil, fmt.Errorf("parse recipient %q: %w", encoded, err)
		}
		recipients = append(recipients, recipient)
	}
	return recipients, nil
}
