package cmd

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

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

func runPluginList(_ *cobra.Command, _ []string) error {
	plugins, err := sdk.Inspect(sdk.DiscoveryOptions{BinaryDir: currentBinaryDir()})
	if err != nil {
		return fmt.Errorf("plugin list: %w", err)
	}

	if len(plugins) == 0 {
		fmt.Println("No plugins found.")
		return nil
	}

	names := make([]string, 0, len(plugins))
	index := make(map[string]sdk.PluginStatus, len(plugins))
	for _, plugin := range plugins {
		names = append(names, plugin.Name)
		index[plugin.Name] = plugin
	}
	sort.Strings(names)

	fmt.Printf("%-24s %-12s %-8s %s\n", "NAME", "VERSION", "STATUS", "PATH")
	fmt.Printf("%-24s %-12s %-8s %s\n", "----", "-------", "------", "----")
	for _, name := range names {
		plugin := index[name]
		status := "ready"
		if !plugin.Initialized {
			status = "error"
		}
		fmt.Printf("%-24s %-12s %-8s %s\n", name, plugin.Version, status, plugin.Path)
	}

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
