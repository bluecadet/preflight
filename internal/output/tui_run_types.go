package output

type taskLogLine struct {
	stream string
	line   string
}

type taskView struct {
	id       string
	name     string
	module   string
	status   string
	message  string
	running  bool
	logs     []taskLogLine
	logBytes int
}

type phaseView struct {
	name    string
	status  string
	running bool
}

type hostView struct {
	name         string
	playName     string
	phases       []phaseView
	tasks        map[string]*taskView
	taskOrder    []string
	totalTasks   int
	recap        recapCounts
	done         bool
	selectedTask int
}

type recapCounts struct {
	ok, changed, failed, skipped int
}
