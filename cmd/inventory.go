package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/inventory"
	"github.com/bluecadet/preflight/internal/output"
)

var inventoryCmd = &cobra.Command{
	Use:   "inventory",
	Short: "Manage and inspect the inventory",
}

var inventoryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all hosts from the inventory file",
	RunE:  runInventoryList,
}

func init() {
	addInventoryFlag(inventoryListCmd)
	addOutputFlags(inventoryListCmd)
	inventoryCmd.AddCommand(inventoryListCmd)
	rootCmd.AddCommand(inventoryCmd)
}

func runInventoryList(cmd *cobra.Command, _ []string) error {
	invPath, _ := cmd.Flags().GetString("inventory")
	if invPath == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("inventory list: get working directory: %w", err)
		}
		invPath = filepath.Join(cwd, "inventory.yml")
	}

	inv, err := inventory.ParseFile(invPath)
	if err != nil {
		return fmt.Errorf("inventory list: %w", err)
	}

	hosts := inv.AllHosts()

	// Collect group membership for display.
	hostGroups := make(map[string][]string)
	for groupName, g := range inv.Groups {
		if groupName == "all" {
			continue
		}
		for _, h := range g.Hosts {
			hostGroups[h.Name] = append(hostGroups[h.Name], groupName)
		}
	}

	entries := make([]output.InventoryHostEntry, 0, len(hosts))
	for _, h := range hosts {
		groups := append([]string(nil), hostGroups[h.Name]...)
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
