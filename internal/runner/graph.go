package runner

import "fmt"

// DAG is a directed acyclic graph of tasks for dependency-ordered execution.
type DAG struct {
	tasks    map[string]*PlanTask // keyed by task ID
	nameToID map[string]string    // task name → task ID
	edges    map[string][]string  // task ID → list of task IDs it depends on
	order    []string             // topological order (task IDs)
}

// BuildDAG constructs a DAG from the given tasks. DependsOn values are resolved
// by task name. Returns an error if a dependency references an unknown task name,
// if two tasks share the same name, or if there is a cycle.
func BuildDAG(tasks []*PlanTask) (*DAG, error) {
	d := &DAG{
		tasks:    make(map[string]*PlanTask, len(tasks)),
		nameToID: make(map[string]string, len(tasks)),
		edges:    make(map[string][]string, len(tasks)),
	}

	// Index tasks by ID and name.
	for _, t := range tasks {
		d.tasks[t.ID] = t
		if existing, dup := d.nameToID[t.Name]; dup {
			return nil, fmt.Errorf("duplicate task name %q: task IDs %s and %s", t.Name, existing, t.ID)
		}
		d.nameToID[t.Name] = t.ID
		d.edges[t.ID] = nil // initialise even if no deps
	}

	// Build edges: resolve depends_on names → IDs.
	for _, t := range tasks {
		for _, depName := range t.DependsOn {
			depID, ok := d.nameToID[depName]
			if !ok {
				return nil, fmt.Errorf("task %q depends on unknown task %q", t.Name, depName)
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

// TopologicalOrder returns tasks in dependency-first execution order.
func (d *DAG) TopologicalOrder() []*PlanTask {
	result := make([]*PlanTask, 0, len(d.order))
	for _, id := range d.order {
		result = append(result, d.tasks[id])
	}
	return result
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
