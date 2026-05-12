package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/config"
	"github.com/bluecadet/preflight/internal/output"
)

var inventoryCmd = &cobra.Command{
	Use:   "inventory",
	Short: "Manage and inspect the inventory",
}

var inventoryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all hosts from the project inventory",
	RunE:  runInventoryList,
}

func init() {
	addOutputFlags(inventoryListCmd)
	inventoryCmd.AddCommand(inventoryListCmd)
	rootCmd.AddCommand(inventoryCmd)
}

func runInventoryList(cmd *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("inventory list: get working directory: %w", err)
	}
	cfg, err := config.ParseFile(filepath.Join(cwd, config.FileName))
	if err != nil {
		return fmt.Errorf("inventory list: %w", err)
	}
	if cfg.Inventory == nil {
		return fmt.Errorf("inventory list: no inventory configured in %s", config.FileName)
	}

	hosts := cfg.Inventory.AllHosts()

	entries := make([]output.InventoryHostEntry, 0, len(hosts))
	for _, h := range hosts {
		groups := append([]string(nil), h.Groups...)
		sort.Strings(groups)
		entries = append(entries, output.InventoryHostEntry{
			Name:      h.Name,
			Address:   h.Address,
			Transport: string(h.Transport),
			Port:      h.Port,
			Groups:    groups,
		})
	}

	renderer := newTextJSONRenderer(cmd)
	defer renderer.Close()
	renderer.Emit(output.InventoryListEvent{Hosts: entries})

	return nil
}
