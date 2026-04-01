package cmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/output"
	"github.com/bluecadet/preflight/internal/runner"
)

var planCmd = &cobra.Command{
	Use:   "plan <playbook>",
	Short: "Resolve and print the execution plan without running it",
	Args:  cobra.ExactArgs(1),
	RunE:  runPlan,
}

func init() {
	rootCmd.AddCommand(planCmd)
}

func runPlan(cmd *cobra.Command, args []string) error {
	playbookPath := getPlaybookPath(args)
	if err := validateLocalOnlyRunFlags(cmd); err != nil {
		return err
	}

	ctx, cancel, err := commandContext(cmd)
	if err != nil {
		return err
	}
	defer cancel()

	varFlags, _ := cmd.Flags().GetStringArray("var")
	vars := parseVars(varFlags)

	tags, _ := cmd.Flags().GetStringSlice("tags")
	skipTags, _ := cmd.Flags().GetStringSlice("skip-tags")

	pb, projectDir, projectCfg, secretsResolver, chain, err := loadPlaybookRunContext(playbookPath)
	if err != nil {
		return err
	}

	registry, _, err := buildModuleRegistry(projectDir)
	if err != nil {
		return err
	}
	hosts, err := resolveRunHosts(ctx, cmd, projectDir, registry, secretsResolver)
	if err != nil {
		return err
	}

	outFmt := getOutputFormat(cmd)
	screenTabs := make([]output.ScreenTab, 0, len(hosts))

	for idx, host := range hosts {
		cfg := runner.Config{
			DryRun:         false,
			Tags:           tags,
			SkipTags:       skipTags,
			ProjectVars:    projectCfg.Vars,
			InventoryVars:  host.Vars,
			Vars:           vars,
			TargetVars:     host.TargetVars,
			TargetName:     host.Name,
			Phase:          "plan",
			Secrets:        secretsResolver,
			ModuleRegistry: registry,
		}

		r := runner.New(host.Target, chain, cfg)
		plan, err := r.Plan(ctx, pb)
		if err != nil {
			return fmt.Errorf("plan for %s: %w", host.Name, err)
		}

		items := make([]output.ScreenItem, 0, len(plan.Tasks))
		for _, pt := range plan.Tasks {
			preview, err := runner.PreviewTask(pt, host.TargetVars)
			if err != nil {
				return fmt.Errorf("preview task %q for %s: %w", pt.Name, host.Name, err)
			}
			meta := []string{}
			if preview.When != "" {
				meta = append(meta, "when: "+preview.When)
			}
			if len(preview.Tags) > 0 {
				meta = append(meta, "tags: "+fmt.Sprintf("%v", preview.Tags))
			}
			items = append(items, output.ScreenItem{
				Title:    preview.Name,
				Status:   "pending",
				Subtitle: preview.Module,
				Summary:  preview.ID,
				Meta:     meta,
				DetailTitle: "Task",
				Detail: []output.ScreenLine{
					{Prefix: "inf", Text: "task id: " + preview.ID, Tone: "info"},
				},
			})
		}
		screenTabs = append(screenTabs, output.ScreenTab{
			Label:  host.Name,
			Status: "ready",
			Meta:   strconv.Itoa(len(plan.Tasks)) + " tasks",
			Content: output.ScreenContent{
				Kind:  output.ScreenKindList,
				Items: items,
				Summary: []output.ScreenStat{
					{Label: "tasks", Value: strconv.Itoa(len(plan.Tasks)), Tone: "info"},
				},
				Empty: "No tasks planned.",
			},
		})

		if outFmt == output.FormatTUI {
			continue
		}

		if idx > 0 {
			fmt.Println()
		}
		fmt.Printf("Target: %s\n", host.Name)
		fmt.Printf("Playbook: %s\n", plan.PlaybookName)
		fmt.Printf("Tasks (%d):\n", len(plan.Tasks))
		for i, pt := range plan.Tasks {
			preview, err := runner.PreviewTask(pt, host.TargetVars)
			if err != nil {
				return fmt.Errorf("preview task %q for %s: %w", pt.Name, host.Name, err)
			}
			fmt.Printf("  %d. [%s] %s", i+1, preview.Module, preview.Name)
			if preview.When != "" {
				fmt.Printf(" (when: %s)", preview.When)
			}
			if len(preview.Tags) > 0 {
				fmt.Printf(" [tags: %v]", preview.Tags)
			}
			fmt.Println()
		}
	}

	if outFmt == output.FormatTUI {
		screen := output.Screen{
			Command: "plan",
			Subject: "play: " + pb.Name,
			Status:  "ready",
			Summary: []output.ScreenStat{
				{Label: "targets", Value: strconv.Itoa(len(hosts)), Tone: "info"},
			},
		}
		if len(screenTabs) == 1 {
			screen.Content = screenTabs[0].Content
		} else {
			screen.Tabs = screenTabs
		}
		return showScreen(cmd, screen)
	}

	return nil
}
