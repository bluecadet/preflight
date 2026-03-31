package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/claytercek/preflight/internal/facts"
	"github.com/claytercek/preflight/internal/module"
	"github.com/claytercek/preflight/internal/target"
	"github.com/spf13/cobra"
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
	// For now only local target is supported; a future version will look up
	// the target name in inventory and build the appropriate transport.
	if len(args) > 0 && args[0] != "local" && args[0] != "localhost" {
		fmt.Fprintf(os.Stderr, "warning: remote targets not yet implemented; using local\n")
	}

	registry := module.Registry()
	tgt := target.NewLocalTarget(registry)

	g := facts.New(tgt)
	f, err := g.Gather(context.Background())
	if err != nil {
		return fmt.Errorf("facts: %w", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(f.AsMap())
}
