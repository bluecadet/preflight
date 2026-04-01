package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/facts"
	"github.com/bluecadet/preflight/internal/output"
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

	concurrency, _ := cmd.Flags().GetInt("concurrency")
	var hosts []targeting.ResolvedHost
	if len(args) == 1 {
		selectors = []string{args[0]}
	}
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

	outFmt := getOutputFormat(cmd)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if len(hosts) == 1 {
		g := facts.New(hosts[0].Target)
		f, err := g.Gather(ctx)
		if err != nil {
			return fmt.Errorf("facts: %w", err)
		}
		if outFmt == output.FormatTUI {
			screen := output.Screen{
				Command: "facts",
				Subject: "target: " + hosts[0].Name,
				Status:  "ready",
				Summary: []output.ScreenStat{
					{Label: "hostname", Value: f.Hostname, Tone: "info"},
					{Label: "os", Value: f.OS.Name, Tone: "info"},
					{Label: "build", Value: strconv.Itoa(f.OS.Build), Tone: "info"},
					{Label: "disks", Value: strconv.Itoa(len(f.Disks)), Tone: "info"},
				},
				Content: output.ScreenContent{
					Kind:     output.ScreenKindDocument,
					Document: prettyJSON(f.AsMap()),
				},
			}
			return showScreen(cmd, screen)
		}
		return enc.Encode(f.AsMap())
	}

	result := make(map[string]any, len(hosts))
	factResults := make(map[string]*facts.Facts, len(hosts))
	var mu sync.Mutex
	if err := runHosts(ctx, hosts, concurrency, func(runCtx context.Context, host targeting.ResolvedHost) error {
		g := facts.New(host.Target)
		f, err := g.Gather(runCtx)
		if err != nil {
			return fmt.Errorf("facts for %s: %w", host.Name, err)
		}
		mu.Lock()
		result[host.Name] = f.AsMap()
		factResults[host.Name] = f
		mu.Unlock()
		return nil
	}); err != nil {
		return err
	}
	if outFmt == output.FormatTUI {
		tabs := make([]output.ScreenTab, 0, len(hosts))
		for _, host := range hosts {
			f := factResults[host.Name]
			tabs = append(tabs, output.ScreenTab{
				Label:  host.Name,
				Status: "ready",
				Meta:   f.OS.Name,
				Content: output.ScreenContent{
					Kind:     output.ScreenKindDocument,
					Document: prettyJSON(f.AsMap()),
					Summary: []output.ScreenStat{
						{Label: "hostname", Value: f.Hostname, Tone: "info"},
						{Label: "build", Value: strconv.Itoa(f.OS.Build), Tone: "info"},
						{Label: "disks", Value: strconv.Itoa(len(f.Disks)), Tone: "info"},
					},
				},
			})
		}
		return showScreen(cmd, output.Screen{
			Command: "facts",
			Subject: "targets: " + strconv.Itoa(len(hosts)),
			Status:  "ready",
			Tabs:    tabs,
		})
	}
	return enc.Encode(result)
}
