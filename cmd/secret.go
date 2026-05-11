package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	"filippo.io/age"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/bluecadet/preflight/internal/config"
	"github.com/bluecadet/preflight/internal/output"
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
	Short: "Encrypt a secret into the repo-backed age store",
	Long: `Encrypt a secret into the repo-backed age store.

The plaintext source is selected in this order:
  --from-file <path>   read from a file
  --from-stdin         read from standard input (trailing newline trimmed)
  (no flag, TTY)       prompt interactively without echo, confirming twice

If no source flag is set and stdin is not a terminal, the command exits with an
error rather than silently consuming piped input.`,
	Args: cobra.ExactArgs(1),
	RunE: runSecretEncrypt,
}

var secretEditCmd = &cobra.Command{
	Use:   "edit <name>",
	Short: "Decrypt a secret into your editor, then re-encrypt it",
	Args:  cobra.ExactArgs(1),
	RunE:  runSecretEdit,
}

var secretIdentityCmd = &cobra.Command{
	Use:   "identity",
	Short: "Manage age identities for project secrets",
}

var secretIdentityGenerateCmd = &cobra.Command{
	Use:   "generate --out <path>",
	Short: "Generate an age X25519 identity file",
	Args:  cobra.NoArgs,
	RunE:  runSecretIdentityGenerate,
}

var secretIdentityRecipientCmd = &cobra.Command{
	Use:   "recipient <path>",
	Short: "Print public recipient(s) for an age identity file",
	Args:  cobra.ExactArgs(1),
	RunE:  runSecretIdentityRecipient,
}

var secretRekeyCmd = &cobra.Command{
	Use:   "rekey [names...]",
	Short: "Re-encrypt configured secrets to the current recipients",
	Args:  cobra.ArbitraryArgs,
	RunE:  runSecretRekey,
}

func init() {
	addOutputFlags(secretListCmd)
	secretEncryptCmd.Flags().String("from-file", "", "path to a plaintext file to encrypt")
	secretEncryptCmd.Flags().Bool("from-stdin", false, "read plaintext from standard input")
	secretEncryptCmd.MarkFlagsMutuallyExclusive("from-file", "from-stdin")
	secretEncryptCmd.Flags().StringSlice("recipient", nil, "age recipient(s) to encrypt to")
	secretEncryptCmd.Flags().String("identity", "", "path to an age identity file used for future decrypt/edit operations")
	secretEditCmd.Flags().StringSlice("recipient", nil, "override age recipient(s) for re-encryption")
	secretEditCmd.Flags().String("identity", "", "override path to the age identity file used to decrypt")
	secretIdentityGenerateCmd.Flags().String("out", "", "path to write the generated age identity file")
	_ = secretIdentityGenerateCmd.MarkFlagRequired("out")
	secretRekeyCmd.Flags().StringSlice("recipient", nil, "override age recipient(s) for re-encryption")
	secretRekeyCmd.Flags().String("identity", "", "override path to the age identity file used to decrypt")

	secretIdentityCmd.AddCommand(secretIdentityGenerateCmd)
	secretIdentityCmd.AddCommand(secretIdentityRecipientCmd)
	secretCmd.AddCommand(secretListCmd)
	secretCmd.AddCommand(secretEncryptCmd)
	secretCmd.AddCommand(secretEditCmd)
	secretCmd.AddCommand(secretIdentityCmd)
	secretCmd.AddCommand(secretRekeyCmd)
	rootCmd.AddCommand(secretCmd)
}

func runSecretList(cmd *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("secret list: get working directory: %w", err)
	}
	cfg, err := loadProjectConfig(cwd)
	if err != nil {
		return err
	}
	provider := secrets.NewRepoProvider(cwd, cfg.Secrets)
	entries := make([]output.SecretListEntry, 0, len(cfg.Secrets.Entries))
	for _, name := range provider.List() {
		entry := cfg.Secrets.Entries[name]
		entries = append(entries, output.SecretListEntry{Name: name, File: entry.File})
	}
	renderer := newTextJSONRenderer(cmd)
	defer renderer.Close()
	renderer.Emit(output.SecretListEvent{Entries: entries})
	return nil
}

func runSecretEncrypt(cmd *cobra.Command, args []string) error {
	name := args[0]

	data, err := readSecretPlaintext(cmd, name)
	if err != nil {
		return fmt.Errorf("secret encrypt: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("secret encrypt: get working directory: %w", err)
	}
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
	if err := provider.Encrypt(name, data); err != nil {
		return fmt.Errorf("secret encrypt: %w", err)
	}
	if err := config.SaveFile(cfgPath, cfg); err != nil {
		return err
	}

	fmt.Printf("Encrypted secret %q to %s\n", name, entry.File)
	return nil
}

func readSecretPlaintext(cmd *cobra.Command, name string) ([]byte, error) {
	fromFile, _ := cmd.Flags().GetString("from-file")
	fromStdin, _ := cmd.Flags().GetBool("from-stdin")

	switch {
	case fromFile != "":
		data, err := os.ReadFile(fromFile)
		if err != nil {
			return nil, fmt.Errorf("read %q: %w", fromFile, err)
		}
		return data, nil
	case fromStdin:
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return bytes.TrimSuffix(bytes.TrimSuffix(data, []byte("\n")), []byte("\r")), nil
	case stdinIsTerminal():
		return promptSecretPlaintext(cmd, name)
	default:
		return nil, fmt.Errorf("no plaintext source: pass --from-file, --from-stdin, or run from a terminal")
	}
}

func stdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func promptSecretPlaintext(cmd *cobra.Command, name string) ([]byte, error) {
	fd := int(os.Stdin.Fd())
	prompt := func(label string) ([]byte, error) {
		fmt.Fprintf(cmd.ErrOrStderr(), "%s for %q: ", label, name)
		value, err := term.ReadPassword(fd)
		fmt.Fprintln(cmd.ErrOrStderr())
		return value, err
	}
	value, err := prompt("Enter value")
	if err != nil {
		return nil, fmt.Errorf("read terminal: %w", err)
	}
	confirm, err := prompt("Confirm value")
	if err != nil {
		return nil, fmt.Errorf("read terminal: %w", err)
	}
	if !bytes.Equal(value, confirm) {
		return nil, fmt.Errorf("values did not match")
	}
	return value, nil
}

func runSecretEdit(cmd *cobra.Command, args []string) error {
	name := args[0]
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("secret edit: get working directory: %w", err)
	}
	cfgPath := projectConfigPath(cwd)
	cfg, err := config.LoadOptional(cfgPath)
	if err != nil {
		return err
	}
	if _, ok := cfg.Secrets.Entries[name]; !ok {
		return fmt.Errorf("secret edit: secret %q is not defined", name)
	}

	applySecretOverrides(cmd, cfg)

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

func runSecretIdentityGenerate(cmd *cobra.Command, _ []string) error {
	outPath, _ := cmd.Flags().GetString("out")
	if outPath == "" {
		return fmt.Errorf("secret identity generate: --out is required")
	}
	if _, err := os.Stat(outPath); err == nil {
		return fmt.Errorf("secret identity generate: %q already exists", outPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("secret identity generate: stat %q: %w", outPath, err)
	}

	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return fmt.Errorf("secret identity generate: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("secret identity generate: mkdir %q: %w", filepath.Dir(outPath), err)
	}
	contents := fmt.Sprintf("# created: %s\n# public key: %s\n%s\n", time.Now().Format(time.RFC3339), identity.Recipient(), identity)
	if err := os.WriteFile(outPath, []byte(contents), 0o600); err != nil {
		return fmt.Errorf("secret identity generate: write %q: %w", outPath, err)
	}

	fmt.Printf("Wrote identity to %s\n", outPath)
	fmt.Printf("Public recipient: %s\n", identity.Recipient())
	return nil
}

func runSecretIdentityRecipient(_ *cobra.Command, args []string) error {
	path := args[0]
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("secret identity recipient: open %q: %w", path, err)
	}
	defer func() {
		_ = f.Close()
	}()
	recipients, err := identityRecipients(f)
	if err != nil {
		return fmt.Errorf("secret identity recipient: %w", err)
	}
	for _, recipient := range recipients {
		fmt.Println(recipient)
	}
	return nil
}

func runSecretRekey(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("secret rekey: get working directory: %w", err)
	}
	cfgPath := projectConfigPath(cwd)
	cfg, err := config.LoadOptional(cfgPath)
	if err != nil {
		return err
	}
	if len(cfg.Secrets.Entries) == 0 {
		return fmt.Errorf("secret rekey: no secrets configured")
	}
	if len(args) > 0 && secretOverridesChanged(cmd) {
		return fmt.Errorf("secret rekey: --identity and --recipient overrides require rekeying all configured secrets")
	}
	applySecretOverrides(cmd, cfg)

	names := args
	if len(names) == 0 {
		provider := secrets.NewRepoProvider(cwd, cfg.Secrets)
		names = provider.List()
	} else {
		for _, name := range names {
			if _, ok := cfg.Secrets.Entries[name]; !ok {
				return fmt.Errorf("secret rekey: secret %q is not defined", name)
			}
		}
	}

	provider := secrets.NewRepoProvider(cwd, cfg.Secrets)
	plaintexts := make(map[string][]byte, len(names))
	for _, name := range names {
		plaintext, err := provider.Resolve(context.Background(), name)
		if err != nil {
			return fmt.Errorf("secret rekey: %w", err)
		}
		plaintexts[name] = plaintext
	}
	for _, name := range names {
		plaintext := plaintexts[name]
		if err := provider.Encrypt(name, plaintext); err != nil {
			return fmt.Errorf("secret rekey: %w", err)
		}
		fmt.Printf("Rekeyed secret %q\n", name)
	}
	if secretOverridesChanged(cmd) {
		if err := config.SaveFile(cfgPath, cfg); err != nil {
			return err
		}
	}
	return nil
}

func applySecretOverrides(cmd *cobra.Command, cfg *config.Config) {
	recipients, _ := cmd.Flags().GetStringSlice("recipient")
	if len(recipients) > 0 {
		cfg.Secrets.Recipients = recipients
	}
	identity, _ := cmd.Flags().GetString("identity")
	if identity != "" {
		cfg.Secrets.Identity = identity
	}
}

func secretOverridesChanged(cmd *cobra.Command) bool {
	return cmd.Flags().Changed("recipient") || cmd.Flags().Changed("identity")
}

func identityRecipients(r io.Reader) ([]string, error) {
	identities, err := age.ParseIdentities(r)
	if err != nil {
		return nil, err
	}
	recipients := make([]string, 0, len(identities))
	for _, identity := range identities {
		switch identity := identity.(type) {
		case *age.X25519Identity:
			recipients = append(recipients, identity.Recipient().String())
		default:
			return nil, fmt.Errorf("unsupported identity type %T", identity)
		}
	}
	return recipients, nil
}

func sanitizeSecretName(name string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
	clean := re.ReplaceAllString(name, "-")
	if clean == "" {
		return "secret"
	}
	return clean
}
