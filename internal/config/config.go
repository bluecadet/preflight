package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// FileName is the default project configuration filename.
const FileName = "preflight.yml"

// Config is the parsed representation of a project-level preflight.yml file.
type Config struct {
	Project     string         `yaml:"project"`
	Environment string         `yaml:"environment"`
	Vars        map[string]any `yaml:"vars"`
	Secrets     SecretsConfig  `yaml:"secrets"`
}

// SecretsConfig configures the repo-backed age secrets provider.
type SecretsConfig struct {
	Identity   string                 `yaml:"identity,omitempty"`
	Recipients []string               `yaml:"recipients,omitempty"`
	Entries    map[string]SecretEntry `yaml:"entries,omitempty"`
}

// SecretEntry describes one encrypted secret in the repo.
type SecretEntry struct {
	File string `yaml:"file"`
	Type string `yaml:"type,omitempty"`
}

// Parse parses project config YAML bytes.
func Parse(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse error: %w", err)
	}
	if cfg.Vars == nil {
		cfg.Vars = make(map[string]any)
	}
	if cfg.Secrets.Entries == nil {
		cfg.Secrets.Entries = make(map[string]SecretEntry)
	}
	return &cfg, nil
}

// ParseFile reads and parses a project config file.
func ParseFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %q: %w", path, err)
	}
	return Parse(data)
}

// LoadOptional parses path if it exists, otherwise returns an empty config.
func LoadOptional(path string) (*Config, error) {
	cfg, err := ParseFile(path)
	if err == nil {
		return cfg, nil
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) && errors.Is(pathErr.Err, os.ErrNotExist) {
		return &Config{
			Vars: make(map[string]any),
			Secrets: SecretsConfig{
				Entries: make(map[string]SecretEntry),
			},
		}, nil
	}
	return nil, err
}

// SaveFile writes cfg to path, creating parent directories as needed.
func SaveFile(path string, cfg *Config) error {
	if cfg == nil {
		cfg = &Config{}
	}
	if cfg.Vars == nil {
		cfg.Vars = make(map[string]any)
	}
	if cfg.Secrets.Entries == nil {
		cfg.Secrets.Entries = make(map[string]SecretEntry)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("config: mkdir %q: %w", dir, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("config: write %q: %w", path, err)
	}
	return nil
}
