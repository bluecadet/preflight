package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/action"
	"github.com/bluecadet/preflight/internal/output"
)

var validateCmd = &cobra.Command{
	Use:   "validate <playbook>",
	Short: "Parse playbook and resolve all action refs without executing",
	Args:  cobra.ExactArgs(1),
	RunE:  runValidate,
}

func init() {
	addOutputFlags(validateCmd)
	rootCmd.AddCommand(validateCmd)
}

func runValidate(cmd *cobra.Command, args []string) error {
	playbookPath := getPlaybookPath(args)
	outFmt := getOutputFormat(cmd)
	renderer := output.Synchronized(output.NewWithOptions(outFmt, os.Stdout, getRendererOptions(cmd)))
	defer renderer.Close()

	pb, err := action.LoadPlaybookFile(playbookPath)
	if err != nil {
		return fmt.Errorf("playbook parse error: %w", err)
	}

	projectDir, _ := playbookDir(playbookPath)
	chain := newActionChain(projectDir)

	ctx := context.Background()

	var errs []error
	visited := make(map[string]bool)
	resolvedRefs := make(map[string]bool)

	var resolveRefs func(tasks []action.Task)
	resolveRefs = func(tasks []action.Task) {
		for _, task := range tasks {
			if task.Uses == "" {
				continue
			}
			ref := task.Uses
			if visited[ref] {
				continue
			}
			visited[ref] = true

			resolved, err := chain.Resolve(ctx, ref)
			if err != nil {
				errs = append(errs, fmt.Errorf("task %q: %w", task.Name, err))
				continue
			}
			resolvedRefs[ref] = true
			if resolved != nil {
				resolveRefs(resolved.Tasks)
			}
		}
	}

	resolveRefs(pb.Tasks)

	sortedResolvedRefs := make([]string, 0, len(resolvedRefs))
	for ref := range resolvedRefs {
		sortedResolvedRefs = append(sortedResolvedRefs, ref)
	}
	sort.Strings(sortedResolvedRefs)

	if len(errs) > 0 {
		for _, e := range errs {
			renderer.Emit(output.ErrorEvent{Message: e.Error()})
		}
		renderer.Emit(output.ValidationEvent{
			PlaybookPath:    playbookPath,
			PlaybookName:    pb.Name,
			TaskCount:       len(pb.Tasks),
			VisitedRefCount: len(visited),
			ResolvedRefs:    sortedResolvedRefs,
			ErrorCount:      len(errs),
		})
		return fmt.Errorf("validation failed with %d error(s)", len(errs))
	}

	renderer.Emit(output.ValidationEvent{
		PlaybookPath:    playbookPath,
		PlaybookName:    pb.Name,
		TaskCount:       len(pb.Tasks),
		VisitedRefCount: len(visited),
		ResolvedRefs:    sortedResolvedRefs,
	})
	return nil
}
