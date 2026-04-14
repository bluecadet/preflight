package cmd

import (
	"fmt"
	"sort"

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
	addOutputFlags(pluginListCmd)
	pluginCmd.AddCommand(pluginListCmd)
	pluginCmd.AddCommand(pluginInfoCmd)
	rootCmd.AddCommand(pluginCmd)
}

func runPluginList(cmd *cobra.Command, _ []string) error {
	plugins, err := sdk.Inspect(sdk.DiscoveryOptions{BinaryDir: currentBinaryDir()})
	if err != nil {
		return fmt.Errorf("plugin list: %w", err)
	}

	sort.Slice(plugins, func(i, j int) bool {
		if plugins[i].Name == plugins[j].Name {
			return plugins[i].Path < plugins[j].Path
		}
		return plugins[i].Name < plugins[j].Name
	})

	entries := make([]output.PluginListEntry, 0, len(plugins))
	for _, plugin := range plugins {
		status := "ready"
		if !plugin.Initialized {
			status = "error"
		}
		entries = append(entries, output.PluginListEntry{
			Name:    plugin.Name,
			Version: plugin.Version,
			Status:  status,
			Path:    plugin.Path,
		})
	}

	renderer := newTextJSONRenderer(cmd)
	defer renderer.Close()
	renderer.Emit(output.PluginListEvent{Entries: entries})

	return nil
}

func runPluginInfo(_ *cobra.Command, args []string) error {
	plugins, err := sdk.Inspect(sdk.DiscoveryOptions{BinaryDir: currentBinaryDir()})
	if err != nil {
		return fmt.Errorf("plugin info: %w", err)
	}

	name := args[0]
	for _, plugin := range plugins {
		if plugin.Name != name {
			continue
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
