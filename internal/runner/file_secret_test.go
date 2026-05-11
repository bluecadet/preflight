package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bluecadet/preflight/internal/config"
	"github.com/bluecadet/preflight/internal/module"
	"github.com/bluecadet/preflight/internal/secrets"
	"github.com/bluecadet/preflight/internal/target"
)

func TestApplyWritesFileContentFromSecret(t *testing.T) {
	dir := t.TempDir()
	identity, err := ageGenerateIdentity(dir)
	if err != nil {
		t.Fatalf("ageGenerateIdentity: %v", err)
	}

	provider := secrets.NewRepoProvider(dir, config.SecretsConfig{
		Identity:   filepath.Join(dir, "keys.txt"),
		Recipients: []string{identity.Recipient().String()},
		Entries: map[string]config.SecretEntry{
			"license": {File: "secrets/license.age"},
		},
	})
	if err := provider.Encrypt("license", []byte("secret\nlicense\n")); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	dest := filepath.Join(dir, "out", "license.txt")
	r := New(target.NewLocalTarget(module.Registry()), emptyResolver(), Config{
		Secrets: secrets.NewResolver(map[string]secrets.Provider{
			secrets.DefaultProviderName: provider,
		}),
	})
	plan := &ExecutionPlan{
		PlaybookName: "file-secret-test",
		Vars:         map[string]any{},
		Tasks: []*PlanTask{{
			ID:     "task-0",
			Name:   "write license",
			Module: "file",
			Params: map[string]any{
				"dest":    dest,
				"content": "secret:license",
			},
		}},
	}

	if err := r.Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", dest, err)
	}
	if string(data) != "secret\nlicense\n" {
		t.Fatalf("expected secret-backed file content, got %q", string(data))
	}
}
