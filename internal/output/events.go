package output

import "fmt"

// EventType identifies the kind of output event.
type EventType string

const (
	EventVersion        EventType = "version"
	EventRunStart       EventType = "run_start"
	EventTaskOutput     EventType = "task_output"
	EventWarning        EventType = "warning"
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

// Correlatable is an optional capability interface for events that carry
// a target host and/or task identifier. Sinks type-assert to extract them.
type Correlatable interface {
	CorrelationIDs() (target, taskID string)
}

// Leveled is an optional capability interface for events that carry
// a severity level ("error", "warn", "info"). Sinks type-assert with
// a fallback to "info".
type Leveled interface{ Level() string }

// Summarizable is an optional capability interface for events that carry
// a human-readable summary message for the run log. Sinks type-assert with a fallback
// to the empty string.
type Summarizable interface{ LogMessage() string }

// Event is the interface implemented by all renderer event types.
// Every event must expose its type via Type() and a redacted copy via Redact().
// The isEvent() marker seals the interface to this package.
type Event interface {
	Type() EventType
	// Redact returns a copy of the event with the given secrets scrubbed
	// from all text and map fields. Implementations must explicitly consider
	// whether the event carries secret-bearing fields and either redact or
	// return the receiver unchanged. There is no default implementation.
	Redact(secrets []string) Event
	isEvent()
}

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

type TaskOutputEvent struct {
	TaskName string
	TaskID   string
	Target   string
	Lines    []string
}

type WarningEvent struct{ Message string }

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

// Type returns the EventType for each event.

func (VersionEvent) Type() EventType        { return EventVersion }
func (RunStartEvent) Type() EventType       { return EventRunStart }
func (TaskOutputEvent) Type() EventType     { return EventTaskOutput }
func (WarningEvent) Type() EventType        { return EventWarning }
func (ActivityStartEvent) Type() EventType  { return EventActivityStart }
func (ActivityResultEvent) Type() EventType { return EventActivityResult }
func (FactsEvent) Type() EventType          { return EventFacts }
func (PlanEvent) Type() EventType           { return EventPlan }
func (StateEvent) Type() EventType          { return EventState }
func (ValidationEvent) Type() EventType     { return EventValidate }
func (ActionCatalogEvent) Type() EventType  { return EventActionList }
func (ActionInfoEvent) Type() EventType     { return EventActionInfo }
func (ActionFetchEvent) Type() EventType    { return EventActionFetch }
func (PluginListEvent) Type() EventType     { return EventPluginList }
func (InventoryListEvent) Type() EventType  { return EventInventoryList }
func (SecretListEvent) Type() EventType     { return EventSecretList }
func (TargetStartEvent) Type() EventType    { return EventTargetStart }
func (TargetCompleteEvent) Type() EventType { return EventTargetComplete }
func (TaskStartedEvent) Type() EventType    { return EventTaskStarted }
func (TaskOKEvent) Type() EventType         { return EventTaskOK }
func (TaskChangedEvent) Type() EventType    { return EventTaskChanged }
func (TaskSkippedEvent) Type() EventType    { return EventTaskSkipped }
func (TaskFailedEvent) Type() EventType     { return EventTaskFailed }
func (DiagnosticEvent) Type() EventType     { return EventDiagnostic }
func (RunSummaryEvent) Type() EventType     { return EventRunSummary }

// Correlatable implementations.

func (e TargetStartEvent) CorrelationIDs() (string, string)    { return e.Target, "" }
func (e TargetCompleteEvent) CorrelationIDs() (string, string) { return e.Target, "" }
func (e TaskStartedEvent) CorrelationIDs() (string, string)    { return e.Target, e.TaskID }
func (e TaskOKEvent) CorrelationIDs() (string, string)         { return e.Target, e.TaskID }
func (e TaskChangedEvent) CorrelationIDs() (string, string)    { return e.Target, e.TaskID }
func (e TaskSkippedEvent) CorrelationIDs() (string, string)    { return e.Target, e.TaskID }
func (e TaskFailedEvent) CorrelationIDs() (string, string)     { return e.Target, e.TaskID }
func (e DiagnosticEvent) CorrelationIDs() (string, string)     { return e.Target, e.TaskID }
func (e TaskOutputEvent) CorrelationIDs() (string, string)     { return e.Target, e.TaskID }
func (e ActivityStartEvent) CorrelationIDs() (string, string)  { return e.Target, "" }
func (e ActivityResultEvent) CorrelationIDs() (string, string) { return e.Target, "" }
func (e FactsEvent) CorrelationIDs() (string, string)          { return e.Target, "" }
func (e PlanEvent) CorrelationIDs() (string, string)           { return e.Target, "" }
func (e StateEvent) CorrelationIDs() (string, string)          { return e.Target, "" }

// Leveled implementations.

func (TaskFailedEvent) Level() string { return "error" }
func (DiagnosticEvent) Level() string { return "error" }
func (WarningEvent) Level() string    { return "warn" }

// Summarizable implementations.

func (e VersionEvent) LogMessage() string {
	if e.PreflightVersion != "" {
		return "preflight " + e.PreflightVersion
	}
	return "preflight"
}

func (e RunStartEvent) LogMessage() string {
	if len(e.Targets) == 1 {
		return "1 target"
	}
	return fmt.Sprintf("%d targets", len(e.Targets))
}

func (TargetStartEvent) LogMessage() string   { return "connecting" }
func (e TaskStartedEvent) LogMessage() string { return e.TaskName }
func (TaskOutputEvent) LogMessage() string    { return "" }

func (e TargetCompleteEvent) LogMessage() string {
	switch e.Outcome {
	case "ok":
		return "ok"
	case "failed":
		return "failed"
	case "unreachable":
		return "unreachable"
	default:
		return e.Outcome
	}
}

func (e TaskOKEvent) LogMessage() string      { return e.TaskName + " ok" }
func (e TaskChangedEvent) LogMessage() string { return e.TaskName + " changed" }
func (e TaskSkippedEvent) LogMessage() string { return e.TaskName + " skipped" }

func (e TaskFailedEvent) LogMessage() string { return e.TaskName + " failed" }

func (e DiagnosticEvent) LogMessage() string { return e.Summary }
func (e RunSummaryEvent) LogMessage() string { return e.Status }
func (e WarningEvent) LogMessage() string    { return e.Message }

func (e ActivityStartEvent) LogMessage() string  { return e.Message }
func (e ActivityResultEvent) LogMessage() string { return e.Message }
func (e FactsEvent) LogMessage() string          { return "" }
func (e PlanEvent) LogMessage() string           { return "" }
func (e StateEvent) LogMessage() string          { return "" }
func (e ValidationEvent) LogMessage() string     { return "" }
func (e ActionCatalogEvent) LogMessage() string  { return "" }
func (e ActionInfoEvent) LogMessage() string     { return "" }
func (e ActionFetchEvent) LogMessage() string    { return "" }
func (e PluginListEvent) LogMessage() string     { return "" }
func (e InventoryListEvent) LogMessage() string  { return "" }
func (e SecretListEvent) LogMessage() string     { return "" }

// Redact implementations.

// Redact implementations.
// Each event explicitly scrubs its own string, []string, and map[string]any fields.

func (e VersionEvent) Redact(secrets []string) Event {
	e.SchemaVersion = scrubString(e.SchemaVersion, secrets)
	e.PreflightVersion = scrubString(e.PreflightVersion, secrets)
	e.PlaybookName = scrubString(e.PlaybookName, secrets)
	return e
}

func (e RunStartEvent) Redact(secrets []string) Event {
	e.Mode = scrubString(e.Mode, secrets)
	e.PlaybookPath = scrubString(e.PlaybookPath, secrets)
	e.PlaybookName = scrubString(e.PlaybookName, secrets)
	e.Targets = scrubStrings(e.Targets, secrets)
	e.Tags = scrubStrings(e.Tags, secrets)
	e.SkipTags = scrubStrings(e.SkipTags, secrets)
	return e
}

func (e TaskOutputEvent) Redact(secrets []string) Event {
	e.TaskName = scrubString(e.TaskName, secrets)
	e.TaskID = scrubString(e.TaskID, secrets)
	e.Target = scrubString(e.Target, secrets)
	e.Lines = scrubStrings(e.Lines, secrets)
	return e
}

func (e WarningEvent) Redact(secrets []string) Event {
	e.Message = scrubString(e.Message, secrets)
	return e
}

func (e ActivityStartEvent) Redact(secrets []string) Event {
	e.Target = scrubString(e.Target, secrets)
	e.Message = scrubString(e.Message, secrets)
	return e
}

func (e ActivityResultEvent) Redact(secrets []string) Event {
	e.Target = scrubString(e.Target, secrets)
	e.Message = scrubString(e.Message, secrets)
	e.Status = scrubString(e.Status, secrets)
	return e
}

func (e FactsEvent) Redact(secrets []string) Event {
	e.Target = scrubString(e.Target, secrets)
	if len(secrets) > 0 {
		e.Facts = deepScrubMap(e.Facts, secrets)
	}
	return e
}

func (e PlanEvent) Redact(secrets []string) Event {
	e.Target = scrubString(e.Target, secrets)
	e.PlaybookName = scrubString(e.PlaybookName, secrets)
	// Tasks []PlanTaskEntry is a struct slice — its fields are not individually scrubbed.
	return e
}

func (e StateEvent) Redact(secrets []string) Event {
	e.Target = scrubString(e.Target, secrets)
	e.PlaybookName = scrubString(e.PlaybookName, secrets)
	e.StatePath = scrubString(e.StatePath, secrets)
	e.LastApplied = scrubString(e.LastApplied, secrets)
	// Comparisons []StateComparison is a struct slice.
	return e
}

func (e ValidationEvent) Redact(secrets []string) Event {
	e.PlaybookPath = scrubString(e.PlaybookPath, secrets)
	e.PlaybookName = scrubString(e.PlaybookName, secrets)
	e.ResolvedRefs = scrubStrings(e.ResolvedRefs, secrets)
	return e
}

func (e ActionCatalogEvent) Redact(secrets []string) Event {
	e.EmbeddedNamespace = scrubString(e.EmbeddedNamespace, secrets)
	e.EmbeddedRefs = scrubStrings(e.EmbeddedRefs, secrets)
	e.LocalDir = scrubString(e.LocalDir, secrets)
	e.LocalRefs = scrubStrings(e.LocalRefs, secrets)
	return e
}

func (e ActionInfoEvent) Redact(secrets []string) Event {
	e.Ref = scrubString(e.Ref, secrets)
	e.Name = scrubString(e.Name, secrets)
	e.Version = scrubString(e.Version, secrets)
	e.Description = scrubString(e.Description, secrets)
	e.Author = scrubString(e.Author, secrets)
	e.TaskNames = scrubStrings(e.TaskNames, secrets)
	// Inputs []ActionInputEntry is a struct slice.
	return e
}

func (e ActionFetchEvent) Redact(secrets []string) Event {
	// Entries []ActionFetchEntry is a struct slice.
	return e
}

func (e PluginListEvent) Redact(secrets []string) Event {
	// Entries []PluginListEntry is a struct slice.
	return e
}

func (e InventoryListEvent) Redact(secrets []string) Event {
	// Hosts []InventoryHostEntry is a struct slice.
	return e
}

func (e SecretListEvent) Redact(secrets []string) Event {
	// Entries []SecretListEntry is a struct slice.
	return e
}

func (e TargetStartEvent) Redact(secrets []string) Event {
	e.Target = scrubString(e.Target, secrets)
	e.Transport = scrubString(e.Transport, secrets)
	e.Address = scrubString(e.Address, secrets)
	return e
}

func (e TargetCompleteEvent) Redact(secrets []string) Event {
	e.Target = scrubString(e.Target, secrets)
	e.Outcome = scrubString(e.Outcome, secrets)
	return e
}

func (e TaskStartedEvent) Redact(secrets []string) Event {
	e.Target = scrubString(e.Target, secrets)
	e.TaskID = scrubString(e.TaskID, secrets)
	e.TaskName = scrubString(e.TaskName, secrets)
	e.Module = scrubString(e.Module, secrets)
	e.ActionPath = scrubString(e.ActionPath, secrets)
	return e
}

func (e TaskOKEvent) Redact(secrets []string) Event {
	e.Target = scrubString(e.Target, secrets)
	e.TaskID = scrubString(e.TaskID, secrets)
	e.TaskName = scrubString(e.TaskName, secrets)
	return e
}

func (e TaskChangedEvent) Redact(secrets []string) Event {
	e.Target = scrubString(e.Target, secrets)
	e.TaskID = scrubString(e.TaskID, secrets)
	e.TaskName = scrubString(e.TaskName, secrets)
	return e
}

func (e TaskSkippedEvent) Redact(secrets []string) Event {
	e.Target = scrubString(e.Target, secrets)
	e.TaskID = scrubString(e.TaskID, secrets)
	e.TaskName = scrubString(e.TaskName, secrets)
	e.Reason = scrubString(e.Reason, secrets)
	return e
}

func (e TaskFailedEvent) Redact(secrets []string) Event {
	e.Target = scrubString(e.Target, secrets)
	e.TaskID = scrubString(e.TaskID, secrets)
	e.TaskName = scrubString(e.TaskName, secrets)
	e.ActionPath = scrubString(e.ActionPath, secrets)
	e.FailMessage = scrubString(e.FailMessage, secrets)
	e.Reason = scrubString(e.Reason, secrets)
	e.Output = scrubStrings(e.Output, secrets)
	return e
}

func (e DiagnosticEvent) Redact(secrets []string) Event {
	e.Target = scrubString(e.Target, secrets)
	e.TaskID = scrubString(e.TaskID, secrets)
	e.Summary = scrubString(e.Summary, secrets)
	e.Source = scrubString(e.Source, secrets)
	return e
}

func (e RunSummaryEvent) Redact(secrets []string) Event {
	e.Status = scrubString(e.Status, secrets)
	return e
}

// TargetStartEvent signals the start of work on a single target.
type TargetStartEvent struct {
	Target    string
	Transport string
	Address   string
}

// TargetCompleteEvent signals all tasks for a target have completed.
type TargetCompleteEvent struct {
	Target          string
	Outcome         string
	OKCount         int
	ChangedCount    int
	FailedCount     int
	SkippedCount    int
	ElapsedMs       int64
	WinRMRoundTrips int64
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
	ActionPath  string
	ElapsedMs   int64
	ExitCode    int
	Output      []string
	FailMessage string
	Reason      string
}

// DiagnosticEvent carries the error body following a task failure or target_unreachable.
// It is always paired with a preceding failure identity event.
type DiagnosticEvent struct {
	Target  string
	TaskID  string
	Summary string
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
func (RunStartEvent) isEvent()       {}
func (TargetStartEvent) isEvent()    {}
func (TargetCompleteEvent) isEvent() {}
func (TaskStartedEvent) isEvent()    {}
func (TaskOKEvent) isEvent()         {}
func (TaskChangedEvent) isEvent()    {}
func (TaskSkippedEvent) isEvent()    {}
func (TaskFailedEvent) isEvent()     {}
func (DiagnosticEvent) isEvent()     {}
func (RunSummaryEvent) isEvent()     {}
func (TaskOutputEvent) isEvent()     {}
func (WarningEvent) isEvent()        {}
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
