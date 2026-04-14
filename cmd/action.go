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
	addOutputFlags(actionListCmd)
	addOutputFlags(actionInfoCmd)
	addOutputFlags(actionFetchCmd)
	actionCmd.AddCommand(actionListCmd)
	actionCmd.AddCommand(actionInfoCmd)
	actionCmd.AddCommand(actionFetchCmd)
	rootCmd.AddCommand(actionCmd)
}

func runActionList(cmd *cobra.Command, _ []string) error {
	// List embedded stdlib actions.
	embeddedRefs, err := listEmbeddedActions()
	if err != nil {
		return fmt.Errorf("action list: embedded: %w", err)
	}
	sort.Strings(embeddedRefs)

	// List local ./actions/ actions.
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("action list: get working directory: %w", err)
	}
	localActionsDir := filepath.Join(cwd, "actions")
	localRefs, err := listLocalActions(localActionsDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("action list: local: %w", err)
	}
	sort.Strings(localRefs)

	renderer := newRenderer(cmd)
	defer renderer.Close()

	renderer.Emit(output.ActionCatalogEvent{
		EmbeddedNamespace: "preflight/",
		EmbeddedRefs:      embeddedRefs,
		LocalDir:          localActionsDir,
		LocalRefs:         localRefs,
	})

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
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("action info: get working directory: %w", err)
	}
	chain := newActionChain(cwd)

	a, err := chain.Resolve(context.Background(), ref)
	if err != nil {
		return err
	}

	inputKeys := make([]string, 0, len(a.Inputs))
	for k := range a.Inputs {
		inputKeys = append(inputKeys, k)
	}
	sort.Strings(inputKeys)

	inputs := make([]output.ActionInputEntry, 0, len(inputKeys))
	for _, key := range inputKeys {
		input := a.Inputs[key]
		defaultValue := ""
		if input.Default != nil {
			defaultValue = fmt.Sprintf("%v", input.Default)
		}
		inputs = append(inputs, output.ActionInputEntry{
			Name:        key,
			Type:        input.Type,
			Description: input.Description,
			Required:    input.Required,
			Default:     defaultValue,
		})
	}

	taskNames := make([]string, 0, len(a.Tasks))
	for _, task := range a.Tasks {
		taskNames = append(taskNames, task.Name)
	}

	renderer := newRenderer(cmd)
	defer renderer.Close()

	renderer.Emit(output.ActionInfoEvent{
		Ref:         ref,
		Name:        a.Name,
		Version:     a.Version,
		Description: a.Description,
		Author:      a.Author,
		Inputs:      inputs,
		TaskNames:   taskNames,
	})

	return nil
}

func runActionFetch(cmd *cobra.Command, args []string) error {
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
	fetchedEntries := make([]output.ActionFetchEntry, 0, len(entries))
	for _, entry := range entries {
		fetchedEntries = append(fetchedEntries, output.ActionFetchEntry{
			Ref: entry.Ref,
			SHA: entry.SHA,
		})
	}

	renderer := newRenderer(cmd)
	defer renderer.Close()

	renderer.Emit(output.ActionFetchEvent{Entries: fetchedEntries})
	return nil
}
