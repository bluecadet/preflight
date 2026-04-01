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
)

// BundleProvider resolves bundle-local secret payload files.
type BundleProvider struct {
	rootDir      string
	identityPath string
	encrypted    bool
	entries      map[string]string
}

// NewBundleProvider builds a bundle-local secret provider rooted at rootDir.
func NewBundleProvider(rootDir string, encrypted bool, identityPath string, entries map[string]string) *BundleProvider {
	if entries == nil {
		entries = make(map[string]string)
	}
	return &BundleProvider{
		rootDir:      rootDir,
		identityPath: identityPath,
		encrypted:    encrypted,
		entries:      entries,
	}
}

// Resolve loads one secret from the bundle payload, decrypting it when needed.
func (p *BundleProvider) Resolve(_ context.Context, name string) ([]byte, error) {
	relPath, ok := p.entries[name]
	if !ok {
		return nil, fmt.Errorf("secret %q is not available in the bundle", name)
	}
	path := relPath
	if !filepath.IsAbs(path) {
		path = filepath.Join(p.rootDir, relPath)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read bundled secret %q: %w", path, err)
	}
	if !p.encrypted {
		return data, nil
	}
	if p.identityPath == "" {
		return nil, fmt.Errorf("bundle secret identity is required")
	}
	identities, err := loadAgeIdentities(p.identityPath)
	if err != nil {
		return nil, err
	}
	reader, err := age.Decrypt(bytes.NewReader(data), identities...)
	if err != nil {
		return nil, fmt.Errorf("decrypt bundled secret %q: %w", path, err)
	}
	plaintext, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read decrypted bundled secret %q: %w", path, err)
	}
	return plaintext, nil
}

// List returns bundle secret names in deterministic order.
func (p *BundleProvider) List() []string {
	names := make([]string, 0, len(p.entries))
	for name := range p.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
