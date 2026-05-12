package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/facts"
	"github.com/bluecadet/preflight/internal/output"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/targeting"
)

var factsCmd = &cobra.Command{
	Use:   "facts [target...]",
	Short: "Gather facts for one or more targets using the selected output format",
	Args:  cobra.ArbitraryArgs,
	RunE:  runFacts,
}

func init() {
	addTargetingFlags(factsCmd)
	addOutputFlags(factsCmd)
	addConcurrencyFlag(factsCmd)
	addTimeoutFlag(factsCmd)
	rootCmd.AddCommand(factsCmd)
}

func runFacts(cmd *cobra.Command, args []string) error {
	flagSelectors, _ := cmd.Flags().GetStringSlice("target")
	selectors := mergeSelectors(flagSelectors, args)
	if err := validateConcurrency(cmd); err != nil {
		return err
	}

	unsupported := []string{"tags", "skip-tags", "check"}
	for _, name := range unsupported {
		if f := cmd.Flags().Lookup(name); f != nil && f.Changed {
			return fmt.Errorf("facts: --%s is not applicable to the facts command", name)
		}
	}

	ctx, cancel, err := commandContext(cmd)
	if err != nil {
		return err
	}
	defer cancel()

	renderer := newRenderer(cmd)
	defer renderer.Close()

	concurrency, _ := cmd.Flags().GetInt("concurrency")
	statePath, err := stateFilePath(cmd)
	if err != nil {
		return fmt.Errorf("facts: %w", err)
	}
	projectDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("facts: get working directory: %w", err)
	}
	projectCfg, err := loadProjectConfig(projectDir)
	if err != nil {
		return fmt.Errorf("facts: %w", err)
	}
	registry, _, err := buildModuleRegistry(projectDir)
	if err != nil {
		return err
	}

	var hosts []targeting.ResolvedHost
	switch {
	case selectorsAreLocal(selectors):
		hosts = []targeting.ResolvedHost{targeting.ResolveLocalHost(registry, statePath)}
	case projectCfg.Inventory == nil:
		if len(selectors) > 0 {
			return fmt.Errorf("facts: no inventory configured in preflight.yml")
		}
		hosts = []targeting.ResolvedHost{targeting.ResolveLocalHost(registry, statePath)}
	default:
		hosts, err = resolveInventoryHosts(ctx, projectCfg.Inventory, selectors, registry, buildSecretsResolver(projectDir, projectCfg), statePath)
		if err != nil {
			return fmt.Errorf("facts: %w", err)
		}
	}
	if len(hosts) == 0 {
		if len(selectors) == 0 {
			return fmt.Errorf("facts: no hosts found in inventory")
		}
		return fmt.Errorf("facts: no hosts resolved from --target")
	}

	if len(hosts) == 1 {
		f, err := gatherFactsForHost(ctx, renderer, hosts[0].Name, hosts[0].Target)
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
		f, err := gatherFactsForHost(runCtx, renderer, host.Name, host.Target)
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

func gatherFactsForHost(ctx context.Context, renderer output.Renderer, hostName string, tgt target.Target) (*facts.Facts, error) {
	remote := renderer != nil && isRemoteFactsTarget(tgt)
	if remote {
		renderer.Emit(output.ActivityStartEvent{Target: hostName, Message: "connecting"})
	}

	collected, err := facts.New(tgt).Gather(ctx)
	if remote {
		status := "ok"
		if err != nil {
			status = "failed"
		}
		renderer.Emit(output.ActivityResultEvent{Target: hostName, Message: "connecting", Status: status})
	}
	return collected, err
}

func isRemoteFactsTarget(tgt target.Target) bool {
	type localMarker interface{ IsLocal() bool }
	if marker, ok := tgt.(localMarker); ok {
		return !marker.IsLocal()
	}
	return true
}
