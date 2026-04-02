package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
		fmt.Fprintln(os.Stdout, output.NewPresenter(os.Stdout).Notice("info", "No hosts found in inventory."))
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

	presenter := output.NewPresenter(os.Stdout)
	rows := make([][]string, 0, len(hosts))
	for _, h := range hosts {
		groups := strings.Join(hostGroups[h.Name], ", ")
		if groups == "" {
			groups = presenter.Muted("-")
		}
		rows = append(rows, []string{
			presenter.Host(h.Name),
			h.Address,
			string(h.Transport),
			fmt.Sprintf("%d", h.Port),
			groups,
		})
	}

	fmt.Fprintln(os.Stdout, presenter.JoinBlocks(
		presenter.Title("Inventory", "Resolved hosts from the current inventory file"),
		presenter.Section("Hosts", presenter.Table(
			[]string{"NAME", "ADDRESS", "TRANSPORT", "PORT", "GROUPS"},
			rows,
		)),
	))
	return nil
}
