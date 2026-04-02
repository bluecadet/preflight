package cmd

import (
	"fmt"
	"os"
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
		fmt.Fprintln(os.Stdout, output.NewPresenter(os.Stdout).Notice("info", "No plugins found."))
		return nil
	}

	sort.Slice(plugins, func(i, j int) bool {
		if plugins[i].Name == plugins[j].Name {
			return plugins[i].Path < plugins[j].Path
		}
		return plugins[i].Name < plugins[j].Name
	})

	presenter := output.NewPresenter(os.Stdout)
	rows := make([][]string, 0, len(plugins))
	for _, plugin := range plugins {
		status := "ready"
		if !plugin.Initialized {
			status = "error"
		}
		rows = append(rows, []string{
			plugin.Name,
			plugin.Version,
			presenter.StatusBadge(status),
			plugin.Path,
		})
	}

	fmt.Fprintln(os.Stdout, presenter.JoinBlocks(
		presenter.Title("Plugins", "Discovered plugin binaries and initialization status"),
		presenter.Section("Plugins", presenter.Table(
			[]string{"NAME", "VERSION", "STATUS", "PATH"},
			rows,
		)),
	))
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
		presenter := output.NewPresenter(os.Stdout)
		initialize := presenter.StatusBadge("ready")
		if plugin.Initialized {
			initialize = presenter.StatusBadge("ready")
		} else {
			initialize = presenter.StatusBadge("error") + " " + plugin.ErrorMessage
		}
		fmt.Fprintln(os.Stdout, presenter.JoinBlocks(
			presenter.Title("Plugin details", "Discovery metadata and initialization health"),
			presenter.Section("Plugin", presenter.KeyValues([]output.KeyValue{
				{Label: "Name", Value: plugin.Name},
				{Label: "Version", Value: plugin.Version},
				{Label: "Path", Value: plugin.Path},
				{Label: "Source", Value: plugin.Source},
				{Label: "Initialize", Value: initialize},
			})),
		))
		return nil
	}

	return fmt.Errorf("plugin info: plugin %q not found", name)
}
