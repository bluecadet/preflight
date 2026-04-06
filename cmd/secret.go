package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/config"
	"github.com/bluecadet/preflight/internal/secrets"
)

var secretCmd = &cobra.Command{
	Use:   "secret",
	Short: "Manage project secrets",
}

var secretListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured project secrets",
	RunE:  runSecretList,
}

var secretEncryptCmd = &cobra.Command{
	Use:   "encrypt <name>",
	Short: "Encrypt a secret from a file into the repo-backed age store",
	Args:  cobra.ExactArgs(1),
	RunE:  runSecretEncrypt,
}

var secretEditCmd = &cobra.Command{
	Use:   "edit <name>",
	Short: "Decrypt a secret into your editor, then re-encrypt it",
	Args:  cobra.ExactArgs(1),
	RunE:  runSecretEdit,
}

func init() {
	secretEncryptCmd.Flags().String("from-file", "", "path to a plaintext file to encrypt")
	_ = secretEncryptCmd.MarkFlagRequired("from-file")
	secretEncryptCmd.Flags().StringSlice("recipient", nil, "age recipient(s) to encrypt to")
	secretEncryptCmd.Flags().String("identity", "", "path to an age identity file used for future decrypt/edit operations")
	secretEditCmd.Flags().StringSlice("recipient", nil, "override age recipient(s) for re-encryption")
	secretEditCmd.Flags().String("identity", "", "override path to the age identity file used to decrypt")

	secretCmd.AddCommand(secretListCmd)
	secretCmd.AddCommand(secretEncryptCmd)
	secretCmd.AddCommand(secretEditCmd)
	rootCmd.AddCommand(secretCmd)
}

func runSecretList(_ *cobra.Command, _ []string) error {
	cwd, _ := os.Getwd()
	cfg, err := loadProjectConfig(cwd)
	if err != nil {
		return err
	}
	if len(cfg.Secrets.Entries) == 0 {
		fmt.Println("No secrets configured.")
		return nil
	}
	provider := secrets.NewRepoProvider(cwd, cfg.Secrets)
	for _, name := range provider.List() {
		entry := cfg.Secrets.Entries[name]
		fmt.Printf("%-24s %s\n", name, entry.File)
	}
	return nil
}

func runSecretEncrypt(cmd *cobra.Command, args []string) error {
	name := args[0]
	fromFile, _ := cmd.Flags().GetString("from-file")

	cwd, _ := os.Getwd()
	cfgPath := projectConfigPath(cwd)
	cfg, err := config.LoadOptional(cfgPath)
	if err != nil {
		return err
	}

	recipients, _ := cmd.Flags().GetStringSlice("recipient")
	if len(recipients) == 0 {
		recipients = cfg.Secrets.Recipients
	}
	if len(recipients) == 0 {
		return fmt.Errorf("secret encrypt: no recipients configured (set secrets.recipients or pass --recipient)")
	}

	identity, _ := cmd.Flags().GetString("identity")
	if identity == "" {
		identity = cfg.Secrets.Identity
	}

	if cfg.Secrets.Entries == nil {
		cfg.Secrets.Entries = make(map[string]config.SecretEntry)
	}
	entry, ok := cfg.Secrets.Entries[name]
	if !ok {
		entry = config.SecretEntry{File: filepath.ToSlash(filepath.Join("secrets", sanitizeSecretName(name)+".age"))}
	}
	cfg.Secrets.Entries[name] = entry
	cfg.Secrets.Recipients = recipients
	if identity != "" {
		cfg.Secrets.Identity = identity
	}

	provider := secrets.NewRepoProvider(cwd, cfg.Secrets)
	data, err := os.ReadFile(fromFile)
	if err != nil {
		return fmt.Errorf("secret encrypt: read %q: %w", fromFile, err)
	}
	if err := provider.Encrypt(name, data); err != nil {
		return fmt.Errorf("secret encrypt: %w", err)
	}
	if err := config.SaveFile(cfgPath, cfg); err != nil {
		return err
	}

	fmt.Printf("Encrypted secret %q to %s\n", name, entry.File)
	return nil
}

func runSecretEdit(cmd *cobra.Command, args []string) error {
	name := args[0]
	cwd, _ := os.Getwd()
	cfgPath := projectConfigPath(cwd)
	cfg, err := config.LoadOptional(cfgPath)
	if err != nil {
		return err
	}
	if _, ok := cfg.Secrets.Entries[name]; !ok {
		return fmt.Errorf("secret edit: secret %q is not defined", name)
	}

	recipients, _ := cmd.Flags().GetStringSlice("recipient")
	if len(recipients) > 0 {
		cfg.Secrets.Recipients = recipients
	}
	identity, _ := cmd.Flags().GetString("identity")
	if identity != "" {
		cfg.Secrets.Identity = identity
	}

	provider := secrets.NewRepoProvider(cwd, cfg.Secrets)
	plaintext, err := provider.Resolve(context.Background(), name)
	if err != nil {
		return fmt.Errorf("secret edit: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "preflight-secret-*")
	if err != nil {
		return fmt.Errorf("secret edit: tempdir: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	tmpPath := filepath.Join(tmpDir, sanitizeSecretName(name)+".txt")
	if err := os.WriteFile(tmpPath, plaintext, 0o600); err != nil {
		return fmt.Errorf("secret edit: write temp file: %w", err)
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	editCmd := exec.Command(editor, tmpPath)
	editCmd.Stdin = os.Stdin
	editCmd.Stdout = os.Stdout
	editCmd.Stderr = os.Stderr
	if err := editCmd.Run(); err != nil {
		return fmt.Errorf("secret edit: run editor: %w", err)
	}

	updated, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("secret edit: read edited file: %w", err)
	}
	if err := provider.Encrypt(name, updated); err != nil {
		return fmt.Errorf("secret edit: %w", err)
	}
	if err := config.SaveFile(cfgPath, cfg); err != nil {
		return err
	}

	fmt.Printf("Updated secret %q\n", name)
	return nil
}

func sanitizeSecretName(name string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
	clean := re.ReplaceAllString(name, "-")
	if clean == "" {
		return "secret"
	}
	return clean
}
