package output

import (
	"strings"
	"time"
)

// maxTaskPreviewLines is the number of recent output lines kept for an active task.
const maxTaskPreviewLines = 3

// activeTask represents a task currently being executed.
type activeTask struct {
	id          string
	name        string
	actionPath  string
	target      string
	startAt     time.Time
	updatedAt   time.Time
	alert       bool
	recentLines []string
}

type activeActivity struct {
	key     string
	message string
	target  string
	startAt time.Time
}

// targetOutcome tracks a completed target's result for footer display.
type targetOutcome struct {
	target string
	failed bool
}

// TargetInfo holds the identity and transport details of a single target.
type TargetInfo struct {
	Name      string
	Transport string
	Address   string
}

// failedTask captures a failed task's identity for the final summary.
type failedTask struct {
	target     string
	actionPath string
	name       string
	message    string
	output     []string
}

// RunProjection is the pure in-memory fold of the event stream into the
// current run status. It holds no terminal width, styles, or bubbletea
// runtime, making it testable as pure data and shareable across sinks.
type RunProjection struct {
	Mode        string
	PlayName    string
	Playbook    string
	Targets     []string
	TargetInfo  []TargetInfo
	StartedAt   time.Time
	PlayStarted bool
	RunDir      string

	// bufferedRunStartDesc holds the RunStartDescriptor until target info
	// arrives via TargetStartEvent (so the header can include the target
	// roster inline). Set for both single- and multi-target runs.
	bufferedRunStartDesc *RunStartDescriptor

	OkCount      int
	ChangedCount int
	FailedCount  int
	SkippedCount int
	WarningCount int
	HadActivity  bool

	hosts          map[string]map[string]*activeTask
	hostOrder      []string
	hostColorIndex map[string]int
	taskOrder      map[string][]string
	activities     map[string]*activeActivity
	activityOrder  []string
	targetOutcomes []targetOutcome
	failedTasks    []failedTask
}

// CommitDescriptor is a structured description of a scroll-region output
// that a sink should render. It is a pure value — never a rendered string
// or a bubbletea command.
type CommitDescriptor interface {
	isCommitDescriptor()
}

// RunStartDescriptor describes the run start header.
type RunStartDescriptor struct {
	Mode         string
	PlaybookPath string
	PlaybookName string
	Targets      []string
	// TargetInfos carries the transport/address detail for each target. It is
	// populated once the buffered header is flushed (after TargetStartEvents
	// arrive) so the renderer can fold the target roster into the header block.
	TargetInfos []TargetInfo
	// SingleTarget is set only for single-target runs, carrying the target
	// identity from the first TargetStartEvent. When set, the renderer should
	// fold the header into one line: RUN  playbook → name (transport • address).
	SingleTarget *TargetInfo
}

// TaskFinishedDescriptor describes a completed task to render.
type TaskFinishedDescriptor struct {
	Target     string
	TaskName   string
	ActionPath string
	Status     string // ok, changed, failed, skipped
	Message    string
	Output     []string // failure output lines
	Elapsed    time.Duration
}

// CardDescriptor describes a static card (facts, plan, state, etc.) to render.
type CardDescriptor struct {
	Kind  string // facts, plan, state, validate, action_catalog, action_info, action_fetch
	Event Event  // the original event for the sink to format
}

// WarningDescriptor describes a warning to render.
type WarningDescriptor struct {
	Message string
}

func (RunStartDescriptor) isCommitDescriptor()     {}
func (TaskFinishedDescriptor) isCommitDescriptor() {}
func (CardDescriptor) isCommitDescriptor()         {}
func (WarningDescriptor) isCommitDescriptor()      {}

// NewRunProjection creates a RunProjection with zero state.
func NewRunProjection() *RunProjection {
	return &RunProjection{
		hosts:          make(map[string]map[string]*activeTask),
		hostColorIndex: make(map[string]int),
		taskOrder:      make(map[string][]string),
		activities:     make(map[string]*activeActivity),
	}
}

// NewRunProjectionWithOptions creates a RunProjection seeded from options.
func NewRunProjectionWithOptions(opts Options) *RunProjection {
	return &RunProjection{
		Mode:           normalizeRunMode(opts.Mode),
		RunDir:         opts.RunDir,
		hosts:          make(map[string]map[string]*activeTask),
		hostColorIndex: make(map[string]int),
		taskOrder:      make(map[string][]string),
		activities:     make(map[string]*activeActivity),
	}
}

// Apply folds an event into the projection and returns structured commit
// descriptors for the scroll region. It never returns rendered strings
// or bubbletea commands.
func (p *RunProjection) Apply(event Event) []CommitDescriptor {
	// If the run-start header is still buffered (waiting for target info to
	// arrive via TargetStartEvent), flush it before any other event so the
	// header and its inline target roster precede task/activity output.
	if p.bufferedRunStartDesc != nil {
		switch event.(type) {
		case RunStartEvent, TargetStartEvent:
			// keep buffering until target info arrives
		default:
			descs := p.flushBufferedRunStart()
			return append(descs, p.applyEvent(event)...)
		}
	}
	return p.applyEvent(event)
}

func (p *RunProjection) applyEvent(event Event) []CommitDescriptor {
	switch e := event.(type) {
	case RunStartEvent:
		return p.applyRunStart(e)
	case TargetStartEvent:
		return p.applyTargetStart(e)
	case TargetCompleteEvent:
		p.applyTargetComplete(e)
		return nil
	case TaskStartedEvent:
		p.applyTaskStarted(e)
		return nil
	case ActivityStartEvent:
		p.applyActivityStart(e)
		return nil
	case ActivityResultEvent:
		p.applyActivityResult(e)
		return nil
	case TaskOutputEvent:
		p.applyTaskOutput(e)
		return nil
	case TaskOKEvent:
		return []CommitDescriptor{p.applyTaskFinished(e.Target, e.TaskID, e.TaskName, "", "ok", "", nil, e.ElapsedMs)}
	case TaskChangedEvent:
		return []CommitDescriptor{p.applyTaskFinished(e.Target, e.TaskID, e.TaskName, "", "changed", "", nil, e.ElapsedMs)}
	case TaskSkippedEvent:
		return []CommitDescriptor{p.applyTaskFinished(e.Target, e.TaskID, e.TaskName, "", "skipped", e.Reason, nil, 0)}
	case TaskFailedEvent:
		return p.applyTaskFailed(e)
	case SupportGateEvent:
		return []CommitDescriptor{WarningDescriptor{Message: e.LogMessage()}}
	case WarningEvent:
		p.WarningCount++
		return []CommitDescriptor{WarningDescriptor(e)}
	case FactsEvent:
		return []CommitDescriptor{CardDescriptor{Kind: "facts", Event: e}}
	case PlanEvent:
		return []CommitDescriptor{CardDescriptor{Kind: "plan", Event: e}}
	case StateEvent:
		return []CommitDescriptor{CardDescriptor{Kind: "state", Event: e}}
	case ValidationEvent:
		return []CommitDescriptor{CardDescriptor{Kind: "validate", Event: e}}
	case ActionCatalogEvent:
		return []CommitDescriptor{CardDescriptor{Kind: "action_catalog", Event: e}}
	case ActionInfoEvent:
		return []CommitDescriptor{CardDescriptor{Kind: "action_info", Event: e}}
	case ActionFetchEvent:
		return []CommitDescriptor{CardDescriptor{Kind: "action_fetch", Event: e}}
	case RunSummaryEvent:
		return nil
	default:
		return nil
	}
}

// Total returns the sum of all task outcomes.
func (p *RunProjection) Total() int {
	return p.OkCount + p.ChangedCount + p.FailedCount + p.SkippedCount
}

// IsCheckMode returns true if the run mode is check.
func (p *RunProjection) IsCheckMode() bool {
	return p.Mode == "check"
}

// ShouldShowHostLabels returns true when host labels should be shown
// in task/activity output.
func (p *RunProjection) ShouldShowHostLabels() bool {
	if p.PlayStarted {
		return len(p.Targets) != 1
	}
	return true
}

// DisplayTarget returns a display-friendly target name.
func (p *RunProjection) DisplayTarget(target string) string {
	if target == "" {
		return "local"
	}
	if len(p.Targets) == 1 && p.Targets[0] == "local" && target == "localhost" {
		return "local"
	}
	return target
}

// TargetCounts returns (done, failed) counts from target outcomes.
func (p *RunProjection) TargetCounts() (done, failed int) {
	for _, oc := range p.targetOutcomes {
		if oc.failed {
			failed++
		} else {
			done++
		}
	}
	return done, failed
}

// RunningTaskCount returns the total number of currently active tasks.
func (p *RunProjection) RunningTaskCount() int {
	count := 0
	for _, host := range p.hosts {
		count += len(host)
	}
	return count
}

// ActiveTargetCount returns the count of distinct targets with running
// tasks or activities.
func (p *RunProjection) ActiveTargetCount() int {
	targets := make(map[string]struct{})
	for _, a := range p.activities {
		targets[a.target] = struct{}{}
	}
	for _, host := range p.hosts {
		for _, t := range host {
			targets[t.target] = struct{}{}
		}
	}
	return len(targets)
}

// OrderedActivities returns activities in insertion order.
func (p *RunProjection) OrderedActivities() []*activeActivity {
	result := make([]*activeActivity, 0, len(p.activityOrder))
	for _, key := range p.activityOrder {
		if a := p.activities[key]; a != nil {
			result = append(result, a)
		}
	}
	return result
}

// OrderedRunningTasks returns running tasks grouped by host in roster
// order, with tasks within each host in start order. The order is stable
// across redraws: it reflects the target roster from RunStartEvent rather
// than nondeterministic event arrival order.
func (p *RunProjection) OrderedRunningTasks() []*activeTask {
	var running []*activeTask
	for _, host := range p.hostOrder {
		for _, id := range p.taskOrder[host] {
			if task := p.hosts[host][id]; task != nil {
				running = append(running, task)
			}
		}
	}
	return running
}

// FailedTasks returns the list of failed tasks.
func (p *RunProjection) FailedTasks() []failedTask {
	return p.failedTasks
}

// TargetOutcomes returns the list of target outcomes.
func (p *RunProjection) TargetOutcomes() []targetOutcome {
	return p.targetOutcomes
}

// Elapsed returns the duration since the run started.
func (p *RunProjection) Elapsed() time.Duration {
	if p.StartedAt.IsZero() {
		return 0
	}
	return time.Since(p.StartedAt)
}

// IsSingleTarget returns true when the run has exactly one target.
func (p *RunProjection) IsSingleTarget() bool {
	return len(p.Targets) == 1
}

// HostColorIndex returns the stable color slot for a target, assigned by
// roster position at run start. Returns -1 when the target has no slot
// (unknown target, or before RunStart). The raw index is wrapped modulo
// the palette's host color count by the renderer.
func (p *RunProjection) HostColorIndex(target string) int {
	if idx, ok := p.hostColorIndex[target]; ok {
		return idx
	}
	return -1
}

func (p *RunProjection) applyRunStart(e RunStartEvent) []CommitDescriptor {
	if p.PlayStarted {
		return nil
	}
	p.PlayStarted = true
	p.Mode = normalizeRunMode(e.Mode)
	p.PlayName = e.PlaybookName
	p.Playbook = e.PlaybookPath
	p.Targets = append([]string(nil), e.Targets...)
	p.StartedAt = time.Now()

	// Seed host order from the resolved target roster so the in-progress
	// view preserves the order the operator specified (inventory/selector
	// order) rather than nondeterministic task-start arrival order.
	for _, target := range p.Targets {
		if p.hosts[target] == nil {
			p.hosts[target] = make(map[string]*activeTask)
			p.hostOrder = append(p.hostOrder, target)
		}
	}

	// Assign each target a stable color slot by roster position. Renderers
	// resolve the slot index to a concrete color from the palette's host
	// color rotation (wrapping modulo the palette size).
	for i, target := range p.Targets {
		p.hostColorIndex[target] = i
	}

	// Buffer the run-start header until target info arrives via
	// TargetStartEvent so the header can include the target roster inline.
	p.bufferedRunStartDesc = &RunStartDescriptor{
		Mode:         e.Mode,
		PlaybookPath: e.PlaybookPath,
		PlaybookName: e.PlaybookName,
		Targets:      e.Targets,
	}
	return nil
}

// flushBufferedRunStart emits the buffered run-start descriptor with the
// target info collected so far and clears the buffer.
func (p *RunProjection) flushBufferedRunStart() []CommitDescriptor {
	if p.bufferedRunStartDesc == nil {
		return nil
	}
	desc := p.bufferedRunStartDesc
	p.bufferedRunStartDesc = nil
	infos := make([]TargetInfo, len(p.TargetInfo))
	copy(infos, p.TargetInfo)
	desc.TargetInfos = infos
	return []CommitDescriptor{desc}
}

func (p *RunProjection) applyTargetStart(e TargetStartEvent) []CommitDescriptor {
	info := TargetInfo{
		Name:      e.Target,
		Transport: e.Transport,
		Address:   e.Address,
	}
	p.TargetInfo = append(p.TargetInfo, info)

	if p.bufferedRunStartDesc == nil {
		return nil
	}

	// Single-target: fold the first target's identity into the header line.
	if len(p.Targets) == 1 {
		p.bufferedRunStartDesc.SingleTarget = &p.TargetInfo[len(p.TargetInfo)-1]
		return p.flushBufferedRunStart()
	}

	// Multi-target: flush once all targets have reported so the roster can
	// be folded into the header block in arrival order.
	if len(p.TargetInfo) >= len(p.Targets) {
		return p.flushBufferedRunStart()
	}
	return nil
}

func (p *RunProjection) applyTargetComplete(e TargetCompleteEvent) {
	p.targetOutcomes = append(p.targetOutcomes, targetOutcome{
		target: e.Target,
		failed: e.Outcome == "failed",
	})
}

func (p *RunProjection) applyTaskStarted(e TaskStartedEvent) {
	if e.Target == "" {
		return
	}
	if p.hosts[e.Target] == nil {
		p.hosts[e.Target] = make(map[string]*activeTask)
		p.hostOrder = append(p.hostOrder, e.Target)
	}

	p.hosts[e.Target][e.TaskID] = &activeTask{
		id:         e.TaskID,
		name:       e.TaskName,
		actionPath: e.ActionPath,
		target:     e.Target,
		startAt:    time.Now(),
		updatedAt:  time.Now(),
	}
	p.taskOrder[e.Target] = append(p.taskOrder[e.Target], e.TaskID)
}

func (p *RunProjection) applyActivityStart(e ActivityStartEvent) {
	p.HadActivity = true
	key := activityKey(e.Target, e.Message)
	if _, ok := p.activities[key]; ok {
		return
	}

	p.activities[key] = &activeActivity{
		key:     key,
		message: e.Message,
		target:  fallbackTarget(e.Target),
		startAt: time.Now(),
	}
	p.activityOrder = append(p.activityOrder, key)
}

func (p *RunProjection) applyActivityResult(e ActivityResultEvent) {
	key := activityKey(e.Target, e.Message)
	delete(p.activities, key)
	p.activityOrder = removeOrderedValue(p.activityOrder, key)
}

func (p *RunProjection) applyTaskOutput(e TaskOutputEvent) {
	if e.Target == "" || e.TaskID == "" {
		return
	}

	host := p.hosts[e.Target]
	if host == nil {
		return
	}
	task := host[e.TaskID]
	if task == nil {
		return
	}

	task.recentLines = append(task.recentLines, e.Lines...)
	task.updatedAt = time.Now()
	for _, line := range e.Lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "warning") || strings.Contains(lower, "error") || strings.Contains(lower, "stderr") {
			task.alert = true
			break
		}
	}
	if len(task.recentLines) > maxTaskPreviewLines {
		task.recentLines = task.recentLines[len(task.recentLines)-maxTaskPreviewLines:]
	}
}

func (p *RunProjection) applyTaskFinished(target, taskID, taskName, actionPath, status, message string, output []string, elapsedMs int64) TaskFinishedDescriptor {
	var elapsed time.Duration
	if elapsedMs > 0 {
		elapsed = time.Duration(elapsedMs) * time.Millisecond
	}

	if host := p.hosts[target]; host != nil {
		if task := host[taskID]; task != nil {
			if elapsed == 0 {
				elapsed = time.Since(task.startAt)
			}
			actionPath = task.actionPath
			delete(host, taskID)
		}
	}
	p.taskOrder[target] = removeOrderedValue(p.taskOrder[target], taskID)

	switch status {
	case "ok":
		p.OkCount++
	case "changed":
		p.ChangedCount++
	case "failed":
		p.FailedCount++
	case "skipped":
		p.SkippedCount++
	}

	return TaskFinishedDescriptor{
		Target:     target,
		TaskName:   taskName,
		ActionPath: actionPath,
		Status:     status,
		Message:    message,
		Output:     output,
		Elapsed:    elapsed,
	}
}

func (p *RunProjection) applyTaskFailed(e TaskFailedEvent) []CommitDescriptor {
	p.failedTasks = append(p.failedTasks, failedTask{
		target:     e.Target,
		actionPath: e.ActionPath,
		name:       e.TaskName,
		message:    e.FailMessage,
		output:     e.Output,
	})
	desc := p.applyTaskFinished(e.Target, e.TaskID, e.TaskName, "", "failed", e.FailMessage, e.Output, e.ElapsedMs)
	return []CommitDescriptor{desc}
}

// removeOrderedValue removes the first occurrence of target from values.
func removeOrderedValue(values []string, target string) []string {
	for i, value := range values {
		if value == target {
			return append(values[:i], values[i+1:]...)
		}
	}
	return values
}

// activityKey returns a unique key for deduplicating activities.
func activityKey(target, message string) string {
	return fallbackTarget(target) + "\x00" + strings.TrimSpace(message)
}

// visibleLiveEntries splits activities and tasks into visible and hidden
// groups based on the supplied limit.
func visibleLiveEntries(activities []*activeActivity, tasks []*activeTask, limit int) ([]*activeActivity, []*activeTask, int) {
	if limit <= 0 {
		return activities, tasks, 0
	}
	remaining := limit
	visibleActivities := activities
	if len(visibleActivities) > remaining {
		hidden := len(visibleActivities) - remaining + len(tasks)
		return visibleActivities[:remaining], nil, hidden
	}

	remaining -= len(visibleActivities)
	visibleTasks := tasks
	if len(visibleTasks) > remaining {
		hidden := len(visibleTasks) - remaining
		return visibleActivities, visibleTasks[:remaining], hidden
	}
	return visibleActivities, visibleTasks, 0
}
