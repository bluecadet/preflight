package cmd

import (
	"context"
	"fmt"

	"github.com/claytercek/preflight/internal/action"
	"github.com/spf13/cobra"
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

	pb, err := action.ParsePlaybookFile(playbookPath)
	if err != nil {
		return fmt.Errorf("playbook parse error: %w", err)
	}

	projectDir, _ := playbookDir(playbookPath)
	chain := action.DefaultChain(projectDir)

	ctx := context.Background()

	// Resolve every action ref referenced in the playbook.
	var errs []error
	for _, task := range pb.Tasks {
		if task.Uses == "" {
			continue
		}
		if _, err := chain.Resolve(ctx, task.Uses); err != nil {
			errs = append(errs, fmt.Errorf("task %q: %w", task.Name, err))
		}
	}

	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Printf("ERROR: %v\n", e)
		}
		return fmt.Errorf("validation failed with %d error(s)", len(errs))
	}

	fmt.Println("OK")
	return nil
}
