package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/facts"
	"github.com/bluecadet/preflight/internal/inventory"
	"github.com/bluecadet/preflight/internal/targeting"
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
	selectors, _ := cmd.Flags().GetStringSlice("target")
	if len(args) > 0 && len(selectors) > 0 {
		return fmt.Errorf("facts: positional target and --target cannot be used together")
	}
	if len(args) > 1 {
		return fmt.Errorf("facts: only one positional target is supported")
	}
	if err := validateConcurrency(cmd); err != nil {
		return err
	}

	ctx, cancel, err := commandContext(cmd)
	if err != nil {
		return err
	}
	defer cancel()

	registry, _, err := buildModuleRegistry("")
	if err != nil {
		return err
	}
	concurrency, _ := cmd.Flags().GetInt("concurrency")
	var hosts []targeting.ResolvedHost
	if len(args) == 1 {
		selectors = []string{args[0]}
	}
	if len(selectors) == 0 || selectorsAreLocal(selectors) {
		hosts = []targeting.ResolvedHost{targeting.ResolveLocalHost(registry, stateFilePath(cmd))}
	} else {
		invPath := inventoryFilePath(cmd, "")
		inv, err := inventory.ParseFile(invPath)
		if err != nil {
			return fmt.Errorf("facts: load inventory %q: %w", invPath, err)
		}
		hosts, err = targeting.ResolveHosts(ctx, inv, selectors, registry, nil, stateFilePath(cmd))
		if err != nil {
			return fmt.Errorf("facts: %w", err)
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if len(hosts) == 1 {
		g := facts.New(hosts[0].Target)
		f, err := g.Gather(ctx)
		if err != nil {
			return fmt.Errorf("facts: %w", err)
		}
		return enc.Encode(f.AsMap())
	}

	result := make(map[string]any, len(hosts))
	var mu sync.Mutex
	if err := runHosts(ctx, hosts, concurrency, func(runCtx context.Context, host targeting.ResolvedHost) error {
		g := facts.New(host.Target)
		f, err := g.Gather(runCtx)
		if err != nil {
			return fmt.Errorf("facts for %s: %w", host.Name, err)
		}
		mu.Lock()
		result[host.Name] = f.AsMap()
		mu.Unlock()
		return nil
	}); err != nil {
		return err
	}
	return enc.Encode(result)
}
