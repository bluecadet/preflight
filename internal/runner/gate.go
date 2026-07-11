package runner

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bluecadet/preflight/internal/output"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/template"
)

// GateRefusal is the typed error returned when the apply-start support gate
// refuses a run. The gate runs after Info() resolves the runtime kind and
// facts are gathered, before task 1: every task that will actually run is
// validated against the support matrix, and the whole run is refused with
// every violation listed. It is when-aware (when-false tasks are excluded)
// and ignore_errors-exempt (those tasks keep fail-and-continue at execution
// time). Per-task apply-time errors remain only for environment prerequisites
// the matrix cannot know.
type GateRefusal struct {
	RuntimeKind target.RuntimeKind
	Violations  []GateViolation
}

// GateViolation is one task-level support-matrix violation collected by the
// apply-start gate.
type GateViolation struct {
	TaskName string
	Module   string
	Err      error
}

// Error renders the refusal: a summary line naming the runtime, then one line
// per violation.
func (g *GateRefusal) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "gate: %d task(s) cannot run on this target (%s)", len(g.Violations), g.RuntimeKind)
	for _, v := range g.Violations {
		fmt.Fprintf(&b, "\n  task %q: %s", v.TaskName, v.Err)
	}
	return b.String()
}

// Event builds the run-log event for this refusal.
func (g *GateRefusal) Event(targetName string) output.SupportGateEvent {
	violations := make([]output.SupportGateViolation, len(g.Violations))
	gateReason := string(target.ClassUnsupportedOnRuntime)
	for i, v := range g.Violations {
		reason := target.ReasonCodeForError(v.Err)
		if reason == "" {
			reason = string(target.ClassUnsupportedOnRuntime)
		}
		if i == 0 {
			gateReason = reason
		}
		violations[i] = output.SupportGateViolation{
			TaskName: v.TaskName,
			Module:   v.Module,
			Reason:   reason,
			Message:  v.Err.Error(),
		}
	}
	return output.SupportGateEvent{
		Target:     targetName,
		Runtime:    string(g.RuntimeKind),
		Reason:     gateReason,
		Violations: violations,
	}
}

// gateApplyStart validates every task that will actually run against the
// support matrix, using the runtime kind resolved by Info(). It is when-aware
// — facts and vars are fixed by the time it runs, so when-false tasks are
// excluded — and ignore_errors-exempt, because those tasks keep
// fail-and-continue at execution time. A when-condition that errors is
// treated as "will run": the gate does not silently swallow when-errors, and
// an unsupported module on a maybe-runnable task is a safe refusal. Returns a
// non-nil *GateRefusal listing every violation when any runnable task is
// unsupported; nil when the run may proceed.
func (r *Runner) gateApplyStart(ordered []*PlanTask, kind target.RuntimeKind, rt *template.RuntimeContext) *GateRefusal {
	if kind == "" {
		// Runtime unresolved (a transport that does not populate RuntimeKind).
		// Skip the gate rather than refusing or panicking — keeps the gate
		// target-agnostic.
		return nil
	}
	reg := r.config.ModuleRegistry
	var violations []GateViolation
	for _, pt := range ordered {
		if pt.IgnoreErrors {
			continue
		}
		whenOK, err := evaluateTaskWhen(pt, rt)
		if err == nil && !whenOK {
			continue
		}
		if verr := target.ValidateModuleForRuntime(pt.Module, kind, reg); verr != nil {
			violations = append(violations, GateViolation{
				TaskName: pt.Name,
				Module:   pt.Module,
				Err:      verr,
			})
		}
	}
	if len(violations) == 0 {
		return nil
	}
	return &GateRefusal{RuntimeKind: kind, Violations: violations}
}

// Ensure GateRefusal satisfies the error interface.
var _ error = (*GateRefusal)(nil)

// errIsGateRefusal reports whether err is a *GateRefusal. Apply treats a gate
// refusal like a task-failure summary: it is a run outcome, not an internal
// error, so Run does not double-log it as an unexpected apply failure.
func errIsGateRefusal(err error) bool {
	var refusal *GateRefusal
	return errors.As(err, &refusal)
}
