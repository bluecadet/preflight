package runner

import (
	"strings"
	"testing"
)

func TestBuildDAG_DuplicateNameReturnsError(t *testing.T) {
	tasks := []*PlanTask{
		{ID: "task-0", Name: "install app"},
		{ID: "task-1", Name: "install app"},
	}
	_, err := BuildDAG(tasks)
	if err == nil {
		t.Fatal("expected error for duplicate task name, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate task name") {
		t.Errorf("error message should mention duplicate task name, got: %v", err)
	}
	if !strings.Contains(err.Error(), "task-0") || !strings.Contains(err.Error(), "task-1") {
		t.Errorf("error message should include both conflicting task IDs, got: %v", err)
	}
}

func TestBuildDAG_UniqueNamesBuildsCorrectly(t *testing.T) {
	tasks := []*PlanTask{
		{ID: "task-0", Name: "first"},
		{ID: "task-1", Name: "second"},
		{ID: "task-2", Name: "third"},
	}
	dag, err := BuildDAG(tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	order := dag.TopologicalOrder()
	if len(order) != 3 {
		t.Errorf("expected 3 tasks in order, got %d", len(order))
	}
}

func TestBuildDAG_DependsOnResolvesToCorrectID(t *testing.T) {
	tasks := []*PlanTask{
		{ID: "task-0", Name: "alpha"},
		{ID: "task-1", Name: "beta", DependsOn: []string{"alpha"}},
	}
	dag, err := BuildDAG(tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// beta depends on alpha, so alpha must appear before beta in the order.
	order := dag.TopologicalOrder()
	if len(order) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(order))
	}
	if order[0].ID != "task-0" {
		t.Errorf("expected alpha (task-0) first, got %s", order[0].ID)
	}
	if order[1].ID != "task-1" {
		t.Errorf("expected beta (task-1) second, got %s", order[1].ID)
	}

	// Verify the edge is stored using the ID, not the name.
	edges := dag.edges["task-1"]
	if len(edges) != 1 || edges[0] != "task-0" {
		t.Errorf("expected edge task-1 -> task-0, got %v", edges)
	}
}

func TestBuildDAG_UnknownDependencyReturnsError(t *testing.T) {
	tasks := []*PlanTask{
		{ID: "task-0", Name: "alpha", DependsOn: []string{"nonexistent"}},
	}
	_, err := BuildDAG(tasks)
	if err == nil {
		t.Fatal("expected error for unknown dependency, got nil")
	}
}

func TestBuildDAG_CycleReturnsError(t *testing.T) {
	tasks := []*PlanTask{
		{ID: "task-0", Name: "alpha", DependsOn: []string{"beta"}},
		{ID: "task-1", Name: "beta", DependsOn: []string{"alpha"}},
	}
	_, err := BuildDAG(tasks)
	if err == nil {
		t.Fatal("expected error for cycle, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error should mention cycle, got: %v", err)
	}
}
