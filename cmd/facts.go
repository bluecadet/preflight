package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/facts"
	"github.com/bluecadet/preflight/internal/output"
	"github.com/bluecadet/preflight/internal/targeting"
)

var factsCmd = &cobra.Command{
	Use:   "facts [target...]",
	Short: "Gather facts for one or more targets (default: local) using the selected output format",
	Args:  cobra.ArbitraryArgs,
	RunE:  runFacts,
}

func init() {
	rootCmd.AddCommand(factsCmd)
}

func runFacts(cmd *cobra.Command, args []string) error {
	flagSelectors, _ := cmd.Flags().GetStringSlice("target")
	selectors := mergeSelectors(flagSelectors, args)
	if err := validateConcurrency(cmd); err != nil {
		return err
	}

	ctx, cancel, err := commandContext(cmd)
	if err != nil {
		return err
	}
	defer cancel()

	outFmt := getOutputFormat(cmd)
	renderer := output.Synchronized(output.NewWithOptions(outFmt, os.Stdout, getRendererOptions(cmd)))
	defer renderer.Close()

	concurrency, _ := cmd.Flags().GetInt("concurrency")
	var hosts []targeting.ResolvedHost
	if len(selectors) == 0 || selectorsAreLocal(selectors) {
		registry, _, err := buildModuleRegistry("")
		if err != nil {
			return err
		}
		hosts = []targeting.ResolvedHost{targeting.ResolveLocalHost(registry, stateFilePath(cmd))}
	} else {
		invPath := inventoryFilePath(cmd, "")
		inv, projectDir, _, secretsResolver, err := loadInventoryRunContext(invPath)
		if err != nil {
			return fmt.Errorf("facts: load inventory %q: %w", invPath, err)
		}
		registry, _, err := buildModuleRegistry(projectDir)
		if err != nil {
			return err
		}
		hosts, err = resolveInventoryHosts(ctx, inv, selectors, registry, secretsResolver, stateFilePath(cmd))
		if err != nil {
			return fmt.Errorf("facts: %w", err)
		}
	}

	if len(hosts) == 1 {
		g := facts.New(hosts[0].Target)
		f, err := g.Gather(ctx)
		if err != nil {
			return fmt.Errorf("facts: %w", err)
		}
		renderer.Emit(output.FactsEvent{
			Target: hosts[0].Name,
			Facts:  f.AsMap(),
		})
		return nil
	}

	return runHosts(ctx, hosts, concurrency, func(runCtx context.Context, host targeting.ResolvedHost) error {
		g := facts.New(host.Target)
		f, err := g.Gather(runCtx)
		if err != nil {
			return fmt.Errorf("facts for %s: %w", host.Name, err)
		}
		renderer.Emit(output.FactsEvent{
			Target: host.Name,
			Facts:  f.AsMap(),
		})
		return nil
	})
}
