package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/inventory"
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
	inventoryCmd.AddCommand(inventoryListCmd)
	rootCmd.AddCommand(inventoryCmd)
}

func runInventoryList(cmd *cobra.Command, _ []string) error {
	invPath, _ := cmd.Flags().GetString("inventory")
	if invPath == "" {
		cwd, _ := os.Getwd()
		invPath = filepath.Join(cwd, "inventory.yml")
	}

	inv, err := inventory.ParseFile(invPath)
	if err != nil {
		return fmt.Errorf("inventory list: %w", err)
	}

	hosts := inv.AllHosts()
	if len(hosts) == 0 {
		fmt.Println("No hosts found in inventory.")
		return nil
	}

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

	// Compute column widths from actual data.
	nameW, addrW := len("NAME"), len("ADDRESS")
	for _, h := range hosts {
		if len(h.Name) > nameW {
			nameW = len(h.Name)
		}
		if len(h.Address) > addrW {
			addrW = len(h.Address)
		}
	}
	nameW += 2
	addrW += 2

	row := fmt.Sprintf("%%-%ds %%-%ds %%-10s %%-6s %%s\n", nameW, addrW)
	fmt.Printf(row, "NAME", "ADDRESS", "TRANSPORT", "PORT", "GROUPS")
	fmt.Printf(row,
		strings.Repeat("-", nameW-2),
		strings.Repeat("-", addrW-2),
		strings.Repeat("-", 10),
		strings.Repeat("-", 6),
		strings.Repeat("-", 20),
	)

	rowData := fmt.Sprintf("%%-%ds %%-%ds %%-10s %%-6d %%s\n", nameW, addrW)
	for _, h := range hosts {
		groups := strings.Join(hostGroups[h.Name], ", ")
		fmt.Printf(rowData, h.Name, h.Address, string(h.Transport), h.Port, groups)
	}

	return nil
}
