package cmd

import (
	"context"
	"fmt"
	"os"

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

	// Resolve every action ref referenced in the playbook.
	var errs []error
	presenter := output.NewPresenter(os.Stdout)
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
			fmt.Fprintln(os.Stdout, presenter.Notice("error", e.Error()))
		}
		return fmt.Errorf("validation failed with %d error(s)", len(errs))
	}

	fmt.Fprintln(os.Stdout, presenter.Notice("success", "Playbook validated successfully."))
	return nil
}
