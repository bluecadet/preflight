package cmd

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/output"
	"github.com/bluecadet/preflight/pkg/plugin/sdk"
)

var pluginCmd = &cobra.Command{
	Use:   "plugin",
	Short: "Manage and inspect plugins",
}

var pluginListCmd = &cobra.Command{
	Use:   "list",
	Short: "List discovered plugins",
	RunE:  runPluginList,
}

var pluginInfoCmd = &cobra.Command{
	Use:   "info <name>",
	Short: "Inspect one discovered plugin",
	Args:  cobra.ExactArgs(1),
	RunE:  runPluginInfo,
}

func init() {
	pluginCmd.AddCommand(pluginListCmd)
	pluginCmd.AddCommand(pluginInfoCmd)
	rootCmd.AddCommand(pluginCmd)
}

func runPluginList(cmd *cobra.Command, _ []string) error {
	plugins, err := sdk.Inspect(sdk.DiscoveryOptions{BinaryDir: currentBinaryDir()})
	if err != nil {
		return fmt.Errorf("plugin list: %w", err)
	}

	if len(plugins) == 0 {
		fmt.Println("No plugins found.")
		return nil
	}

	sort.Slice(plugins, func(i, j int) bool {
		if plugins[i].Name == plugins[j].Name {
			return plugins[i].Path < plugins[j].Path
		}
		return plugins[i].Name < plugins[j].Name
	})

	if getOutputFormat(cmd) == output.FormatTUI {
		items := make([]output.ScreenItem, 0, len(plugins))
		for _, plugin := range plugins {
			status := "ready"
			summary := plugin.Version
			if !plugin.Initialized {
				status = "failed"
				summary = plugin.ErrorMessage
			}
			items = append(items, output.ScreenItem{
				Title:    plugin.Name,
				Status:   status,
				Subtitle: plugin.Version,
				Summary:  summary,
				Meta: []string{
					"path: " + plugin.Path,
				},
				DetailTitle: "Plugin",
				Detail: []output.ScreenLine{
					{Prefix: "inf", Text: "source: " + plugin.Source, Tone: "info"},
					{Prefix: "inf", Text: "path: " + plugin.Path, Tone: "info"},
					{Prefix: "inf", Text: "initialized: " + strconv.FormatBool(plugin.Initialized), Tone: status},
				},
				AutoExpand: !plugin.Initialized,
			})
		}
		return showScreen(cmd, output.Screen{
			Command: "plugin list",
			Subject: "binary dir: " + currentBinaryDir(),
			Status:  "ready",
			Summary: []output.ScreenStat{
				{Label: "plugins", Value: strconv.Itoa(len(plugins)), Tone: "info"},
			},
			Content: output.ScreenContent{
				Kind:  output.ScreenKindList,
				Items: items,
				Empty: "No plugins found.",
			},
		})
	}

	fmt.Printf("%-24s %-12s %-8s %s\n", "NAME", "VERSION", "STATUS", "PATH")
	fmt.Printf("%-24s %-12s %-8s %s\n", "----", "-------", "------", "----")
	for _, plugin := range plugins {
		status := "ready"
		if !plugin.Initialized {
			status = "error"
		}
		fmt.Printf("%-24s %-12s %-8s %s\n", plugin.Name, plugin.Version, status, plugin.Path)
	}

	return nil
}

func runPluginInfo(cmd *cobra.Command, args []string) error {
	plugins, err := sdk.Inspect(sdk.DiscoveryOptions{BinaryDir: currentBinaryDir()})
	if err != nil {
		return fmt.Errorf("plugin info: %w", err)
	}

	name := args[0]
	for _, plugin := range plugins {
		if plugin.Name != name {
			continue
		}
		if getOutputFormat(cmd) == output.FormatTUI {
			status := "ready"
			if !plugin.Initialized {
				status = "failed"
			}
			doc := strings.Join([]string{
				"Name        " + plugin.Name,
				"Version     " + plugin.Version,
				"Path        " + plugin.Path,
				"Source      " + plugin.Source,
				"Initialize  " + func() string {
					if plugin.Initialized {
						return "ok"
					}
					return "error (" + plugin.ErrorMessage + ")"
				}(),
			}, "\n")
			return showScreen(cmd, output.Screen{
				Command: "plugin info",
				Subject: plugin.Name,
				Status:  status,
				Content: output.ScreenContent{
					Kind:     output.ScreenKindDocument,
					Document: doc,
				},
			})
		}
		fmt.Printf("Name:        %s\n", plugin.Name)
		fmt.Printf("Version:     %s\n", plugin.Version)
		fmt.Printf("Path:        %s\n", plugin.Path)
		fmt.Printf("Source:      %s\n", plugin.Source)
		if plugin.Initialized {
			fmt.Println("Initialize:  ok")
		} else {
			fmt.Printf("Initialize:  error (%s)\n", plugin.ErrorMessage)
		}
		return nil
	}

	return fmt.Errorf("plugin info: plugin %q not found", name)
}
