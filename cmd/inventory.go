package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

	if getOutputFormat(cmd) == output.FormatTUI {
		items := make([]output.ScreenItem, 0, len(hosts))
		for _, h := range hosts {
			groups := strings.Join(hostGroups[h.Name], ", ")
			items = append(items, output.ScreenItem{
				Title:    h.Name,
				Status:   "ready",
				Subtitle: string(h.Transport),
				Summary:  h.Address,
				Meta: []string{
					"port: " + strconv.Itoa(h.Port),
					"groups: " + groups,
				},
				DetailTitle: "Host",
				Detail: []output.ScreenLine{
					{Prefix: "inf", Text: "address: " + h.Address, Tone: "info"},
					{Prefix: "inf", Text: "transport: " + string(h.Transport), Tone: "info"},
					{Prefix: "inf", Text: "port: " + strconv.Itoa(h.Port), Tone: "info"},
					{Prefix: "inf", Text: "groups: " + groups, Tone: "info"},
				},
				AutoExpand: len(items) == 0,
			})
		}
		return showScreen(cmd, output.Screen{
			Command: "inventory list",
			Subject: "inventory: " + invPath,
			Status:  "ready",
			Summary: []output.ScreenStat{
				{Label: "hosts", Value: strconv.Itoa(len(hosts)), Tone: "info"},
				{Label: "groups", Value: strconv.Itoa(len(inv.Groups)), Tone: "info"},
			},
			Content: output.ScreenContent{
				Kind:  output.ScreenKindList,
				Items: items,
				Empty: "No hosts found in inventory.",
			},
		})
	}

	fmt.Printf("%-20s %-20s %-10s %-6s %s\n", "NAME", "ADDRESS", "TRANSPORT", "PORT", "GROUPS")
	fmt.Printf("%-20s %-20s %-10s %-6s %s\n",
		strings.Repeat("-", 20),
		strings.Repeat("-", 20),
		strings.Repeat("-", 10),
		strings.Repeat("-", 6),
		strings.Repeat("-", 20),
	)

	for _, h := range hosts {
		groups := strings.Join(hostGroups[h.Name], ", ")
		fmt.Printf("%-20s %-20s %-10s %-6d %s\n",
			h.Name,
			h.Address,
			string(h.Transport),
			h.Port,
			groups,
		)
	}

	return nil
}
