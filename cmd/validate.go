package cmd

import (
	"context"
	"fmt"
	"strings"

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
	for _, task := range pb.Tasks {
		if task.Uses == "" {
			continue
		}
		if _, err := chain.Resolve(ctx, task.Uses); err != nil {
			errs = append(errs, fmt.Errorf("task %q: %w", task.Name, err))
		}
	}

	if len(errs) > 0 {
		if getOutputFormat(cmd) == output.FormatTUI {
			lines := []string{fmt.Sprintf("%d validation errors", len(errs)), ""}
			for _, err := range errs {
				lines = append(lines, "- "+err.Error())
			}
			renderErr := showScreen(cmd, output.Screen{
				Command: "validate",
				Subject: playbookPath,
				Status:  "failed",
				Content: output.ScreenContent{
					Kind:     output.ScreenKindDocument,
					Document: strings.Join(lines, "\n"),
				},
			})
			if renderErr != nil {
				return renderErr
			}
			return quietError(fmt.Errorf("validation failed with %d error(s)", len(errs)))
		}
		for _, e := range errs {
			fmt.Printf("ERROR: %v\n", e)
		}
		return fmt.Errorf("validation failed with %d error(s)", len(errs))
	}

	if getOutputFormat(cmd) == output.FormatTUI {
		return showScreen(cmd, output.Screen{
			Command: "validate",
			Subject: playbookPath,
			Status:  "ok",
			Content: output.ScreenContent{
				Kind:     output.ScreenKindDocument,
				Document: "Playbook parsed successfully\nResolved all action references\nNo validation errors found",
			},
		})
	}

	fmt.Println("OK")
	return nil
}
