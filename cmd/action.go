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
	"github.com/bluecadet/preflight/internal/output"
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
	presenter := output.NewPresenter(os.Stdout)
	embeddedRefs, err := listEmbeddedActions()
	if err != nil {
		return fmt.Errorf("action list: embedded: %w", err)
	}
	sort.Strings(embeddedRefs)

	cwd, _ := os.Getwd()
	localActionsDir := filepath.Join(cwd, "actions")
	localRefs, err := listLocalActions(localActionsDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("action list: local: %w", err)
	}
	sort.Strings(localRefs)
	if len(localRefs) == 0 {
		localRefs = []string{presenter.Muted("(none)")}
	}

	fmt.Fprintln(os.Stdout, presenter.JoinBlocks(
		presenter.Title("Actions", "Available embedded and local action references"),
		presenter.Section("Embedded stdlib", presenter.Bullets(embeddedRefs)),
		presenter.Section("Local actions", presenter.JoinBlocks(
			presenter.Muted(localActionsDir),
			presenter.Bullets(localRefs),
		)),
	))
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

	presenter := output.NewPresenter(os.Stdout)
	meta := []output.KeyValue{
		{Label: "Name", Value: a.Name},
		{Label: "Version", Value: a.Version},
		{Label: "Description", Value: a.Description},
	}
	if a.Author != "" {
		meta = append(meta, output.KeyValue{Label: "Author", Value: a.Author})
	}

	blocks := []string{
		presenter.Title("Action details", "Metadata, contract, and expanded task list"),
		presenter.Section("Action", presenter.KeyValues(meta)),
	}
	if len(a.Inputs) > 0 {
		keys := make([]string, 0, len(a.Inputs))
		for k := range a.Inputs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		rows := make([][]string, 0, len(keys))
		for _, k := range keys {
			inp := a.Inputs[k]
			required := presenter.Muted("optional")
			if inp.Required {
				required = presenter.StatusBadge("required")
			}
			def := "-"
			if inp.Default != nil {
				def = fmt.Sprintf("%v", inp.Default)
			}
			rows = append(rows, []string{k, inp.Description, required, def})
		}
		blocks = append(blocks, presenter.Section("Inputs", presenter.Table(
			[]string{"NAME", "DESCRIPTION", "REQUIRED", "DEFAULT"},
			rows,
		)))
	}

	if len(a.Outputs) > 0 {
		keys := make([]string, 0, len(a.Outputs))
		for k := range a.Outputs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		rows := make([][]string, 0, len(keys))
		for _, k := range keys {
			out := a.Outputs[k]
			rows = append(rows, []string{k, out.Description})
		}
		blocks = append(blocks, presenter.Section("Outputs", presenter.Table(
			[]string{"NAME", "DESCRIPTION"},
			rows,
		)))
	}

	taskItems := make([]string, 0, len(a.Tasks))
	for i, t := range a.Tasks {
		taskItems = append(taskItems, fmt.Sprintf("%d. %s", i+1, t.Name))
	}
	blocks = append(blocks, presenter.Section("Tasks", presenter.Bullets(taskItems)))

	fmt.Fprintln(os.Stdout, presenter.JoinBlocks(blocks...))
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
	presenter := output.NewPresenter(os.Stdout)
	for _, entry := range entries {
		fmt.Fprintln(os.Stdout, presenter.Notice("success", fmt.Sprintf("Fetched %s -> %s", entry.Ref, entry.SHA)))
	}
	return nil
}
