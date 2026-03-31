package cmd

import (
	"fmt"
	"os"
	"sort"

	"github.com/claytercek/preflight/pkg/plugin/sdk"
	"github.com/spf13/cobra"
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

func init() {
	pluginCmd.AddCommand(pluginListCmd)
	rootCmd.AddCommand(pluginCmd)
}

func runPluginList(_ *cobra.Command, _ []string) error {
	// Use the directory of the current executable as the binary dir.
	binaryDir := ""
	if exe, err := os.Executable(); err == nil {
		for i := len(exe) - 1; i >= 0; i-- {
			if exe[i] == '/' || exe[i] == '\\' {
				binaryDir = exe[:i]
				break
			}
		}
	}

	plugins, err := sdk.Discover(binaryDir)
	if err != nil {
		return fmt.Errorf("plugin list: %w", err)
	}

	if len(plugins) == 0 {
		fmt.Println("No plugins found.")
		return nil
	}

	names := make([]string, 0, len(plugins))
	for name := range plugins {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Printf("%-24s %s\n", "NAME", "PATH")
	fmt.Printf("%-24s %s\n", "----", "----")
	for _, name := range names {
		fmt.Printf("%-24s %s\n", name, plugins[name])
	}

	return nil
}
