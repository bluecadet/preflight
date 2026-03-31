package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/facts"
	"github.com/bluecadet/preflight/internal/module"
	"github.com/bluecadet/preflight/internal/target"
)

var factsCmd = &cobra.Command{
	Use:   "facts [target]",
	Short: "Gather facts for a target (default: local) and print as JSON",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runFacts,
}

func init() {
	rootCmd.AddCommand(factsCmd)
}

func runFacts(cmd *cobra.Command, args []string) error {
	if err := validateLocalTargets(cmd); err != nil {
		return err
	}
	if len(args) > 0 && !isLocalTarget(args[0]) {
		return fmt.Errorf("facts only supports %q or %q in local-only mode", "local", "localhost")
	}

	ctx, cancel, err := commandContext(cmd)
	if err != nil {
		return err
	}
	defer cancel()

	registry := module.Registry()
	tgt := target.NewLocalTarget(registry)

	g := facts.New(tgt)
	f, err := g.Gather(ctx)
	if err != nil {
		return fmt.Errorf("facts: %w", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(f.AsMap())
}
