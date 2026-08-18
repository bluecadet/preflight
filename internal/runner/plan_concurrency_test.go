package runner

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/bluecadet/preflight/internal/action"
	"github.com/bluecadet/preflight/internal/stdlib"
)

// TestPlanConcurrentHostsSharePlaybook guards the ownership model: a single
// *action.Playbook is loaded once and handed to one Runner per host, which are
// planned concurrently. Planning must therefore treat the playbook (and every
// Task reachable from it) as read-only. This fails under -race if planning
// writes derived state back onto the shared Tasks.
func TestPlanConcurrentHostsSharePlaybook(t *testing.T) {
	resolver := action.Chain{action.NewEmbeddedResolver(stdlib.FS)}
	shared := &action.Playbook{
		Name: "shared across hosts",
		Tasks: []action.Task{
			{
				Name:          "inline module task",
				InlineModules: map[string]map[string]any{"shell": {"cmd": "echo hello"}},
			},
			{
				Name:         "explicit module task",
				ModuleName:   "shell",
				ModuleParams: map[string]any{"cmd": "echo world"},
			},
			{
				Name: "uses task",
				Uses: "preflight/windows-machine",
				With: map[string]any{
					"computer_name": "Gallery-Kiosk-01",
					"timezone":      "Eastern Standard Time",
				},
			},
			{
				Name: "bare uses task with no module",
				Uses: "preflight/windows-power",
			},
		},
	}

	const hosts = 8
	summaries := make([]string, hosts)
	errs := make([]error, hosts)

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range hosts {
		wg.Go(func() {
			// Per-host Runner config, exactly as cmd/apply.go builds it.
			r := New(&mockTarget{}, resolver, Config{TargetName: fmt.Sprintf("host-%d", i)})
			<-start
			plan, err := r.Plan(context.Background(), shared)
			if err != nil {
				errs[i] = err
				return
			}
			summaries[i] = planSummary(plan)
		})
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("host %d: Plan returned error: %v", i, err)
		}
	}
	for i := 1; i < hosts; i++ {
		if summaries[i] != summaries[0] {
			t.Fatalf("host %d planned differently than host 0:\n%s\n---\n%s", i, summaries[i], summaries[0])
		}
	}
}

// planSummary renders the module resolution of every planned task so two plans
// can be compared for exact equality.
func planSummary(plan *ExecutionPlan) string {
	var b []byte
	for _, pt := range plan.Tasks {
		b = fmt.Appendf(b, "%s\t%s\t%s\t%v\n", pt.ID, pt.Name, pt.Module, pt.Params)
	}
	return string(b)
}
