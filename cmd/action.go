package cmd

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/action"
	"github.com/bluecadet/preflight/internal/stdlib"
)

var actionCmd = &cobra.Command{
	Use:   "action",
	Short: "Manage and inspect actions",
}

var actionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available actions from all resolvers",
	RunE:  runActionList,
}

var actionInfoCmd = &cobra.Command{
	Use:   "info <ref>",
	Short: "Print name, description, inputs, and outputs for an action",
	Args:  cobra.ExactArgs(1),
	RunE:  runActionInfo,
}

var actionFetchCmd = &cobra.Command{
	Use:   "fetch <ref>",
	Short: "Fetch a remote action ref into the local cache",
	Args:  cobra.ExactArgs(1),
	RunE:  runActionFetch,
}

func init() {
	actionCmd.AddCommand(actionListCmd)
	actionCmd.AddCommand(actionInfoCmd)
	actionCmd.AddCommand(actionFetchCmd)
	rootCmd.AddCommand(actionCmd)
}

func runActionList(_ *cobra.Command, _ []string) error {
	// List embedded stdlib actions.
	embeddedRefs, err := listEmbeddedActions()
	if err != nil {
		return fmt.Errorf("action list: embedded: %w", err)
	}
	sort.Strings(embeddedRefs)
	fmt.Println("Embedded actions (preflight/):")
	for _, ref := range embeddedRefs {
		fmt.Printf("  %s\n", ref)
	}

	// List local ./actions/ actions.
	cwd, _ := os.Getwd()
	localActionsDir := filepath.Join(cwd, "actions")
	localRefs, err := listLocalActions(localActionsDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("action list: local: %w", err)
	}
	sort.Strings(localRefs)
	fmt.Printf("\nLocal actions (%s):\n", localActionsDir)
	if len(localRefs) == 0 {
		fmt.Println("  (none)")
	}
	for _, ref := range localRefs {
		fmt.Printf("  %s\n", ref)
	}

	return nil
}

// listEmbeddedActions walks the embedded stdlib FS and returns preflight/ refs.
func listEmbeddedActions() ([]string, error) {
	var refs []string
	err := fs.WalkDir(stdlib.FS, "actions/preflight", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == "action.yml" {
			// actions/preflight/<name>/action.yml → preflight/<name>
			rel := strings.TrimPrefix(path, "actions/")
			ref := strings.TrimSuffix(rel, "/action.yml")
			refs = append(refs, ref)
		}
		return nil
	})
	return refs, err
}

// listLocalActions walks a local directory and returns action refs.
func listLocalActions(dir string) ([]string, error) {
	var refs []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == "action.yml" {
			rel, _ := filepath.Rel(dir, filepath.Dir(path))
			refs = append(refs, filepath.ToSlash(rel))
		}
		return nil
	})
	return refs, err
}

func runActionInfo(cmd *cobra.Command, args []string) error {
	ref := args[0]
	cwd, _ := os.Getwd()
	chain := newActionChain(cwd)

	a, err := chain.Resolve(context.Background(), ref)
	if err != nil {
		return err
	}

	fmt.Printf("Name:        %s\n", a.Name)
	fmt.Printf("Version:     %s\n", a.Version)
	fmt.Printf("Description: %s\n", a.Description)
	if a.Author != "" {
		fmt.Printf("Author:      %s\n", a.Author)
	}

	if len(a.Inputs) > 0 {
		fmt.Println("\nInputs:")
		keys := make([]string, 0, len(a.Inputs))
		for k := range a.Inputs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			inp := a.Inputs[k]
			req := ""
			if inp.Required {
				req = " (required)"
			}
			def := ""
			if inp.Default != nil {
				def = fmt.Sprintf(" [default: %v]", inp.Default)
			}
			fmt.Printf("  %-20s %s%s%s\n", k+":", inp.Description, req, def)
		}
	}

	fmt.Printf("\nTasks (%d):\n", len(a.Tasks))
	for i, t := range a.Tasks {
		fmt.Printf("  %d. %s\n", i+1, t.Name)
	}

	return nil
}

func runActionFetch(_ *cobra.Command, args []string) error {
	ref := args[0]
	if _, err := action.ParseRemoteRef(ref); err != nil {
		return fmt.Errorf("action fetch: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("action fetch: get working directory: %w", err)
	}

	entries, err := action.FetchRefs(context.Background(), newActionChain(cwd), []string{ref})
	if err != nil {
		return fmt.Errorf("action fetch: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Ref < entries[j].Ref
	})
	for _, entry := range entries {
		fmt.Printf("Fetched %s -> %s\n", entry.Ref, entry.SHA)
	}
	return nil
}
