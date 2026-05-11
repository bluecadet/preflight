package runner

import (
	"fmt"
	"slices"
)

// DAG is a directed acyclic graph of tasks for dependency-ordered execution.
type DAG struct {
	tasks    map[string]*PlanTask // keyed by task ID
	nameToID map[string]string    // canonical dependency ref → task ID
	edges    map[string][]string  // task ID → list of task IDs it depends on
	order    []string             // topological order (task IDs)
}

// BuildDAG constructs a DAG from the given tasks. DependsOn values are resolved
// by canonical dependency refs prepared during planning. Returns an error if a
// dependency references an unknown task ref or if there is a cycle.
func BuildDAG(tasks []*PlanTask) (*DAG, error) {
	d := &DAG{
		tasks:    make(map[string]*PlanTask, len(tasks)),
		nameToID: make(map[string]string, len(tasks)),
		edges:    make(map[string][]string, len(tasks)),
	}

	// Index tasks by ID and canonical dependency ref.
	for _, t := range tasks {
		d.tasks[t.ID] = t
		key := t.Ref
		if key == "" {
			key = t.Name
		}
		if key != "" {
			if existing, dup := d.nameToID[key]; dup {
				return nil, fmt.Errorf("duplicate task name/ref %q: task IDs %s and %s", key, existing, t.ID)
			}
			d.nameToID[key] = t.ID
		}
		d.edges[t.ID] = nil // initialise even if no deps
	}

	// Build edges: resolve depends_on refs/names → IDs.
	for _, t := range tasks {
		for _, depRef := range t.DependsOn {
			depID, ok := d.nameToID[depRef]
			if !ok {
				return nil, fmt.Errorf("task %q depends on unknown task %q", t.Name, depRef)
			}
			d.edges[t.ID] = append(d.edges[t.ID], depID)
		}
	}

	// Topological sort (Kahn's algorithm).
	order, err := topoSort(tasks, d.edges)
	if err != nil {
		return nil, err
	}
	d.order = order

	return d, nil
}

// DAG returns the plan's validated dependency graph, rebuilding it only for
// hand-constructed test plans or older callers that did not come from Plan().
func (p *ExecutionPlan) DAG() (*DAG, error) {
	if p == nil {
		return nil, fmt.Errorf("nil execution plan")
	}
	if p.dag != nil {
		return p.dag, nil
	}
	dag, err := BuildDAG(p.Tasks)
	if err != nil {
		return nil, err
	}
	p.dag = dag
	return dag, nil
}

// TopologicalOrder returns tasks in dependency-first execution order.
func (d *DAG) TopologicalOrder() []*PlanTask {
	result := make([]*PlanTask, 0, len(d.order))
	for _, id := range d.order {
		result = append(result, d.tasks[id])
	}
	return result
}

// DependencyIDs resolves a task's dependency refs to stable task IDs.
func (d *DAG) DependencyIDs(task *PlanTask) ([]string, error) {
	if task == nil {
		return nil, nil
	}
	dependsOn := make([]string, 0, len(task.DependsOn))
	for _, depRef := range task.DependsOn {
		depID, ok := d.nameToID[depRef]
		if !ok {
			return nil, fmt.Errorf("task %q depends on unknown task %q", task.Name, depRef)
		}
		dependsOn = append(dependsOn, depID)
	}
	slices.Sort(dependsOn)
	return dependsOn, nil
}

// topoSort returns task IDs in topological order using Kahn's algorithm.
// Returns an error if a cycle is detected.
func topoSort(tasks []*PlanTask, edges map[string][]string) ([]string, error) {
	// incoming[id] = number of dependencies that must run before id.
	incoming := make(map[string]int, len(tasks))
	for _, t := range tasks {
		incoming[t.ID] = len(edges[t.ID])
	}

	// dependents[depID] = list of tasks that depend on depID.
	dependents := make(map[string][]string, len(tasks))
	for _, t := range tasks {
		for _, depID := range edges[t.ID] {
			dependents[depID] = append(dependents[depID], t.ID)
		}
	}

	// Seed queue with tasks that have no dependencies.
	queue := make([]string, 0, len(tasks))
	for _, t := range tasks {
		if incoming[t.ID] == 0 {
			queue = append(queue, t.ID)
		}
	}

	order := make([]string, 0, len(tasks))
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		order = append(order, cur)

		// Reduce in-degree of tasks that depend on cur.
		for _, dep := range dependents[cur] {
			incoming[dep]--
			if incoming[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if len(order) != len(tasks) {
		return nil, fmt.Errorf("cycle detected in task dependencies")
	}

	return order, nil
}
