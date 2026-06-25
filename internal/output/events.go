package output

// EventType identifies the kind of output event.
type EventType string

const (
	EventVersion        EventType = "version"
	EventRunStart       EventType = "run_start"
	EventPlayStart      EventType = "play_start"
	EventTaskStart      EventType = "task_start"
	EventTaskOutput     EventType = "task_output"
	EventTaskResult     EventType = "task_result"
	EventPlayEnd        EventType = "play_end"
	EventWarning        EventType = "warning"
	EventError          EventType = "error"
	EventFacts          EventType = "facts"
	EventPlan           EventType = "plan"
	EventState          EventType = "state"
	EventValidate       EventType = "validate"
	EventActionList     EventType = "action_list"
	EventActionInfo     EventType = "action_info"
	EventActionFetch    EventType = "action_fetch"
	EventPluginList     EventType = "plugin_list"
	EventInventoryList  EventType = "inventory_list"
	EventSecretList     EventType = "secret_list"
	EventActivityStart  EventType = "activity_start"
	EventActivityResult EventType = "activity_result"
	EventTargetStart    EventType = "target_start"
	EventTargetComplete EventType = "target_complete"
	EventTaskStarted    EventType = "task_started"
	EventTaskOK         EventType = "task_ok"
	EventTaskChanged    EventType = "task_changed"
	EventTaskSkipped    EventType = "task_skipped"
	EventTaskFailed     EventType = "task_failed"
	EventDiagnostic     EventType = "diagnostic"
	EventRunSummary     EventType = "run_summary"
)

// Event is the sealed interface implemented by all renderer event types.
type Event interface{ isEvent() }

// VersionEvent is the first event in every run log.
type VersionEvent struct {
	SchemaVersion    string
	PreflightVersion string
	PlaybookName     string
}

// RunStartEvent signals the start of a run.
type RunStartEvent struct {
	Mode         string
	PlaybookPath string
	PlaybookName string
	Targets      []string
	DryRun       bool
	Tags         []string
	SkipTags     []string
}

// PlayStartEvent signals the start of a play (legacy, use RunStartEvent instead).
type PlayStartEvent struct{ PlayName string }

type TaskStartEvent struct {
	TaskName   string
	TaskID     string
	ActionPath string
	Target     string
}

type TaskOutputEvent struct {
	TaskName string
	TaskID   string
	Target   string
	Lines    []string
}

type TaskResultEvent struct {
	TaskName   string
	TaskID     string
	ActionPath string
	Target     string
	Status     string
	Message    string
	Output     []string
}

// PlayEndEvent is the legacy per-target completion event.
type PlayEndEvent struct {
	Target       string
	OKCount      int
	ChangedCount int
	FailedCount  int
	SkippedCount int
}

type WarningEvent struct{ Message string }

type ErrorEvent struct{ Message string }

type ActivityStartEvent struct {
	Target  string
	Message string
}

type ActivityResultEvent struct {
	Target  string
	Message string
	Status  string
}

// FactsEvent carries gathered facts for a single target.
type FactsEvent struct {
	Target string
	Facts  map[string]any
}

// PlanTaskEntry describes a single planned task for PlanEvent.
type PlanTaskEntry struct {
	Number int
	Module string
	Name   string
	When   string
	Tags   []string
}

// PlanEvent carries the resolved execution plan for a single target.
type PlanEvent struct {
	Target       string
	PlaybookName string
	Tasks        []PlanTaskEntry
}

// StateEvent carries the state comparison data for a single target.
type StateEvent struct {
	Target       string
	PlaybookName string
	StatePath    string
	LastApplied  string
	Comparisons  []StateComparison
}

// StateComparison is a single row in the state diff table.
type StateComparison struct {
	Status         string
	TaskName       string
	Module         string
	RecordedStatus string
}

// ValidationEvent carries the results of a validate command run.
type ValidationEvent struct {
	PlaybookPath    string
	PlaybookName    string
	TaskCount       int
	VisitedRefCount int
	ResolvedRefs    []string
	ErrorCount      int
}

// ActionCatalogEvent carries the available action refs grouped by source.
type ActionCatalogEvent struct {
	EmbeddedNamespace string
	EmbeddedRefs      []string
	LocalDir          string
	LocalRefs         []string
}

// ActionInputEntry describes one input row for ActionInfoEvent.
type ActionInputEntry struct {
	Name        string
	Type        string
	Description string
	Required    bool
	Default     string
}

// ActionInfoEvent carries metadata about a single action ref.
type ActionInfoEvent struct {
	Ref         string
	Name        string
	Version     string
	Description string
	Author      string
	Inputs      []ActionInputEntry
	TaskNames   []string
}

// ActionFetchEntry describes one fetched remote action ref.
type ActionFetchEntry struct {
	Ref string
	SHA string
}

// ActionFetchEvent carries the refs fetched by action fetch.
type ActionFetchEvent struct {
	Entries []ActionFetchEntry
}

type PluginListEntry struct {
	Name    string
	Version string
	Status  string
	Path    string
}

type PluginListEvent struct {
	Entries []PluginListEntry
}

type InventoryHostEntry struct {
	Name      string
	Address   string
	Transport string
	Port      int
	Groups    []string
}

type InventoryListEvent struct {
	Hosts []InventoryHostEntry
}

type SecretListEntry struct {
	Name string
	File string
}

type SecretListEvent struct {
	Entries []SecretListEntry
}

func (RunStartEvent) isEvent()  {}
func (PlayStartEvent) isEvent() {}

// TargetStartEvent signals the start of work on a single target.
type TargetStartEvent struct {
	Target    string
	Transport string
	Address   string
}

// TargetCompleteEvent signals all tasks for a target have completed.
type TargetCompleteEvent struct {
	Target       string
	Outcome      string
	OKCount      int
	ChangedCount int
	FailedCount  int
	SkippedCount int
	ElapsedMs    int64
}

// TaskStartedEvent signals the start of a single task.
type TaskStartedEvent struct {
	Target     string
	TaskID     string
	TaskName   string
	Module     string
	ActionPath string
}

// TaskOKEvent signals a task completed with status "ok".
type TaskOKEvent struct {
	Target    string
	TaskID    string
	TaskName  string
	ElapsedMs int64
}

// TaskChangedEvent signals a task completed with status "changed".
type TaskChangedEvent struct {
	Target    string
	TaskID    string
	TaskName  string
	ElapsedMs int64
}

// TaskSkippedEvent signals a task was skipped.
type TaskSkippedEvent struct {
	Target   string
	TaskID   string
	TaskName string
	Reason   string
}

// TaskFailedEvent signals a task failed.
type TaskFailedEvent struct {
	Target      string
	TaskID      string
	TaskName    string
	ElapsedMs   int64
	ExitCode    int
	Output      []string
	FailMessage string
}

// DiagnosticEvent carries the error body following a task failure or target_unreachable.
// It is always paired with a preceding failure identity event.
type DiagnosticEvent struct {
	Target  string
	TaskID  string
	Summary string
	Detail  string
	Source  string
}

// TargetCounts holds per-target outcome counts for RunSummaryEvent.
type TargetCounts struct {
	OK          int `json:"ok"`
	Failed      int `json:"failed"`
	Unreachable int `json:"unreachable"`
}

// RunSummaryEvent is the final event in a run.
type RunSummaryEvent struct {
	Status        string
	TargetTallies TargetCounts
	OKCount       int
	ChangedCount  int
	FailedCount   int
	SkippedCount  int
	ElapsedMs     int64
}

func (VersionEvent) isEvent()        {}
func (TargetStartEvent) isEvent()    {}
func (TargetCompleteEvent) isEvent() {}
func (TaskStartedEvent) isEvent()    {}
func (TaskOKEvent) isEvent()         {}
func (TaskChangedEvent) isEvent()    {}
func (TaskSkippedEvent) isEvent()    {}
func (TaskFailedEvent) isEvent()     {}
func (DiagnosticEvent) isEvent()     {}
func (RunSummaryEvent) isEvent()     {}
func (TaskStartEvent) isEvent()      {}
func (TaskOutputEvent) isEvent()     {}
func (TaskResultEvent) isEvent()     {}
func (PlayEndEvent) isEvent()        {}
func (WarningEvent) isEvent()        {}
func (ErrorEvent) isEvent()          {}
func (ActivityStartEvent) isEvent()  {}
func (ActivityResultEvent) isEvent() {}
func (FactsEvent) isEvent()          {}
func (PlanEvent) isEvent()           {}
func (StateEvent) isEvent()          {}
func (ValidationEvent) isEvent()     {}
func (ActionCatalogEvent) isEvent()  {}
func (ActionInfoEvent) isEvent()     {}
func (ActionFetchEvent) isEvent()    {}
func (PluginListEvent) isEvent()     {}
func (InventoryListEvent) isEvent()  {}
func (SecretListEvent) isEvent()     {}
