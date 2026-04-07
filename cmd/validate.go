package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/action"
)

var validateCmd = &cobra.Command{
	Use:   "validate <playbook>",
	Short: "Parse playbook and resolve all action refs without executing",
	Args:  cobra.ExactArgs(1),
	RunE:  runValidate,
}

func init() {
	rootCmd.AddCommand(validateCmd)
}

func runValidate(cmd *cobra.Command, args []string) error {
	playbookPath := getPlaybookPath(args)

	pb, err := action.LoadPlaybookFile(playbookPath)
	if err != nil {
		return fmt.Errorf("playbook parse error: %w", err)
	}

	projectDir, _ := playbookDir(playbookPath)
	chain := newActionChain(projectDir)

	ctx := context.Background()

	var errs []error
	visited := make(map[string]bool)
	totalRefs := 0

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
			totalRefs++
			if resolved != nil {
				resolveRefs(resolved.Tasks)
			}
		}
	}

	resolveRefs(pb.Tasks)

	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Printf("ERROR: %v\n", e)
		}
		return fmt.Errorf("validation failed with %d error(s)", len(errs))
	}

	taskCount := len(pb.Tasks)
	fmt.Printf("Validated: %s (%d tasks, %d action refs resolved)\n", playbookPath, taskCount, totalRefs)
	return nil
}
