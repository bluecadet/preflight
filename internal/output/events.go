package output

// EventType identifies the kind of output event.
type EventType string

const (
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
)

// Event is the sealed interface implemented by all renderer event types.
type Event interface{ isEvent() }

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

func (PlayStartEvent) isEvent()      {}
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
