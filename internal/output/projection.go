package output

// eventProjection normalizes renderer-relevant fields from an Event.
type eventProjection struct {
	kind EventType

	playName     string
	name         string
	namespace    string
	ref          string
	taskID       string
	task         string
	actionPath   string
	target       string
	status       string
	message      string
	errorMessage string
	taskCount    int
	okCount      int
	changedCount int
	failedCount  int
	skippedCount int
	lines        []string
	output       []string
	facts        map[string]any
	tasks        []PlanTaskEntry
	statePath    string
	lastApplied  string
	comparisons  []StateComparison
	playbookPath string
	visitedRefs  int
	resolvedRefs []string
	errorCount   int
	embeddedRefs []string
	localDir     string
	localRefs    []string
	version      string
	description  string
	author       string
	inputs       []ActionInputEntry
	taskNames    []string
	entries      []ActionFetchEntry
	plugins      []PluginListEntry
	hosts        []InventoryHostEntry
	secrets      []SecretListEntry
}

func projectEvent(event Event) (eventProjection, bool) {
	switch e := event.(type) {
	case PlayStartEvent:
		return eventProjection{kind: EventPlayStart, playName: e.PlayName}, true
	case TaskStartEvent:
		return eventProjection{kind: EventTaskStart, taskID: e.TaskID, task: e.TaskName, actionPath: e.ActionPath, target: e.Target}, true
	case TaskOutputEvent:
		return eventProjection{kind: EventTaskOutput, taskID: e.TaskID, task: e.TaskName, target: e.Target, lines: e.Lines}, true
	case TaskResultEvent:
		return eventProjection{kind: EventTaskResult, taskID: e.TaskID, task: e.TaskName, actionPath: e.ActionPath, target: e.Target, status: e.Status, message: e.Message, output: e.Output}, true
	case PlayEndEvent:
		return eventProjection{kind: EventPlayEnd, target: e.Target, okCount: e.OKCount, changedCount: e.ChangedCount, failedCount: e.FailedCount, skippedCount: e.SkippedCount}, true
	case WarningEvent:
		return eventProjection{kind: EventWarning, message: e.Message}, true
	case ErrorEvent:
		return eventProjection{kind: EventError, errorMessage: e.Message}, true
	case ActivityStartEvent:
		return eventProjection{kind: EventActivityStart, target: e.Target, message: e.Message}, true
	case ActivityResultEvent:
		return eventProjection{kind: EventActivityResult, target: e.Target, message: e.Message, status: e.Status}, true
	case FactsEvent:
		return eventProjection{kind: EventFacts, target: e.Target, facts: e.Facts}, true
	case PlanEvent:
		return eventProjection{kind: EventPlan, target: e.Target, playName: e.PlaybookName, tasks: e.Tasks}, true
	case StateEvent:
		return eventProjection{kind: EventState, target: e.Target, playName: e.PlaybookName, statePath: e.StatePath, lastApplied: e.LastApplied, comparisons: e.Comparisons}, true
	case ValidationEvent:
		return eventProjection{kind: EventValidate, playName: e.PlaybookName, playbookPath: e.PlaybookPath, taskCount: e.TaskCount, visitedRefs: e.VisitedRefCount, resolvedRefs: e.ResolvedRefs, errorCount: e.ErrorCount}, true
	case ActionCatalogEvent:
		return eventProjection{kind: EventActionList, namespace: e.EmbeddedNamespace, embeddedRefs: e.EmbeddedRefs, localDir: e.LocalDir, localRefs: e.LocalRefs}, true
	case ActionInfoEvent:
		return eventProjection{kind: EventActionInfo, ref: e.Ref, name: e.Name, version: e.Version, description: e.Description, author: e.Author, inputs: e.Inputs, taskNames: e.TaskNames}, true
	case ActionFetchEvent:
		return eventProjection{kind: EventActionFetch, entries: e.Entries}, true
	case PluginListEvent:
		return eventProjection{kind: EventPluginList, plugins: e.Entries}, true
	case InventoryListEvent:
		return eventProjection{kind: EventInventoryList, hosts: e.Hosts}, true
	case SecretListEvent:
		return eventProjection{kind: EventSecretList, secrets: e.Entries}, true
	default:
		return eventProjection{}, false
	}
}
