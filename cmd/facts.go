package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/facts"
	"github.com/bluecadet/preflight/internal/targeting"
)

var factsCmd = &cobra.Command{
	Use:   "facts",
	Short: "Gather facts for a target (default: local) and print as JSON",
	Args:  cobra.NoArgs,
	RunE:  runFacts,
}

func init() {
	rootCmd.AddCommand(factsCmd)
}

func runFacts(cmd *cobra.Command, _ []string) error {
	selectors, _ := cmd.Flags().GetStringSlice("target")
	if err := validateConcurrency(cmd); err \!= nil {
		return err
	}

	ctx, cancel, err := commandContext(cmd)
	if err \!= nil {
		return err
	}
	defer cancel()

	concurrency, _ := cmd.Flags().GetInt("concurrency")
	var hosts []targeting.ResolvedHost
	if len(selectors) == 0 || selectorsAreLocal(selectors) {
		registry, _, err := buildModuleRegistry("")
		if err \!= nil {
			return err
		}
		hosts = []targeting.ResolvedHost{targeting.ResolveLocalHost(registry, stateFilePath(cmd))}
	} else {
		invPath := inventoryFilePath(cmd, "")
		inv, projectDir, _, secretsResolver, err := loadInventoryRunContext(invPath)
		if err \!= nil {
			return fmt.Errorf("facts: load inventory %q: %w", invPath, err)
		}
		registry, _, err := buildModuleRegistry(projectDir)
		if err \!= nil {
			return err
		}
		hosts, err = resolveInventoryHosts(ctx, inv, selectors, registry, secretsResolver, stateFilePath(cmd))
		if err \!= nil {
			return fmt.Errorf("facts: %w", err)
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if len(hosts) == 1 {
		g := facts.New(hosts[0].Target)
		f, err := g.Gather(ctx)
		if err \!= nil {
			return fmt.Errorf("facts: %w", err)
		}
		return enc.Encode(f.AsMap())
	}

	result := make(map[string]any, len(hosts))
	var mu sync.Mutex
	if err := runHosts(ctx, hosts, concurrency, func(runCtx context.Context, host targeting.ResolvedHost) error {
		g := facts.New(host.Target)
		f, err := g.Gather(runCtx)
		if err \!= nil {
			return fmt.Errorf("facts for %s: %w", host.Name, err)
		}
		mu.Lock()
		result[host.Name] = f.AsMap()
		mu.Unlock()
		return nil
	}); err \!= nil {
		return err
	}
	return enc.Encode(result)
}
