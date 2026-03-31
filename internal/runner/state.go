package runner

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/bluecadet/preflight/internal/secrets"
	"github.com/bluecadet/preflight/internal/target"
)

// TaskResult records the outcome of a single task execution.
type TaskResult struct {
	TaskID    string        `json:"task_id"`
	TaskName  string        `json:"task_name"`
	Status    target.Status `json:"status"`
	Timestamp time.Time     `json:"timestamp"`
	ParamHash string        `json:"param_hash"` // SHA256 of params JSON
}

// State holds persisted runner state written to disk after each apply.
type State struct {
	LastApplied time.Time             `json:"last_applied"`
	Results     map[string]TaskResult `json:"results"` // keyed by task ID
}

type ComparisonStatus string

const (
	ComparisonStatusNew     ComparisonStatus = "NEW"
	ComparisonStatusChanged ComparisonStatus = "CHANGED"
	ComparisonStatusKnown   ComparisonStatus = "KNOWN"
	ComparisonStatusRemoved ComparisonStatus = "REMOVED"
)

type PlannedTaskState struct {
	TaskID    string
	TaskName  string
	ParamHash string
}

type TaskComparison struct {
	Status         ComparisonStatus
	TaskID         string
	TaskName       string
	RecordedStatus target.Status
}

// LoadState reads a state file from path. If the file does not exist, an empty
// State is returned (not an error).
func LoadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &State{Results: make(map[string]TaskResult)}, nil
		}
		return nil, fmt.Errorf("state: read %q: %w", path, err)
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("state: parse %q: %w", path, err)
	}
	if s.Results == nil {
		s.Results = make(map[string]TaskResult)
	}
	return &s, nil
}

// Save writes the state to path as JSON. The file is written atomically by
// writing to a temp file and renaming it.
func (s *State) Save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("state: marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("state: mkdir %q: %w", filepath.Dir(path), err)
	}
	// Write to a temp file in the same directory, then rename for atomicity.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("state: write %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("state: rename %q → %q: %w", tmp, path, err)
	}
	return nil
}

// Record stores a TaskResult in the state, keyed by TaskID.
func (s *State) Record(result TaskResult) {
	if s.Results == nil {
		s.Results = make(map[string]TaskResult)
	}
	s.Results[result.TaskID] = result
}

// ParamHash computes a SHA256 hash of the params map as a hex string.
// This is a helper for callers that want to populate TaskResult.ParamHash.
func ParamHash(params map[string]any) string {
	data, err := json.Marshal(params)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

func BuildPlannedTaskState(ctx context.Context, plan *ExecutionPlan, resolver *secrets.Resolver) ([]PlannedTaskState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, fmt.Errorf("state: nil execution plan")
	}

	tasks := make([]PlannedTaskState, 0, len(plan.Tasks))
	for _, task := range plan.Tasks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		params := task.Params
		if resolver != nil && resolver.HasProviders() {
			resolved, err := resolver.ResolveMap(ctx, task.Params)
			if err != nil {
				return nil, fmt.Errorf("state: task %q: %w", task.Name, err)
			}
			params = resolved
		}

		tasks = append(tasks, PlannedTaskState{
			TaskID:    task.ID,
			TaskName:  task.Name,
			ParamHash: ParamHash(params),
		})
	}
	return tasks, nil
}

func ComparePlannedTasks(planned []PlannedTaskState, state *State) []TaskComparison {
	if state == nil {
		state = &State{Results: make(map[string]TaskResult)}
	}
	if state.Results == nil {
		state.Results = make(map[string]TaskResult)
	}

	comparisons := make([]TaskComparison, 0, len(planned)+len(state.Results))
	seen := make(map[string]struct{}, len(planned))

	for _, task := range planned {
		seen[task.TaskID] = struct{}{}
		recorded, ok := state.Results[task.TaskID]
		if !ok {
			comparisons = append(comparisons, TaskComparison{
				Status:   ComparisonStatusNew,
				TaskID:   task.TaskID,
				TaskName: task.TaskName,
			})
			continue
		}

		status := ComparisonStatusKnown
		if recorded.ParamHash != task.ParamHash {
			status = ComparisonStatusChanged
		}
		comparisons = append(comparisons, TaskComparison{
			Status:         status,
			TaskID:         task.TaskID,
			TaskName:       task.TaskName,
			RecordedStatus: recorded.Status,
		})
	}

	removedIDs := make([]string, 0, len(state.Results))
	for taskID := range state.Results {
		if _, ok := seen[taskID]; ok {
			continue
		}
		removedIDs = append(removedIDs, taskID)
	}
	slices.Sort(removedIDs)
	for _, taskID := range removedIDs {
		recorded := state.Results[taskID]
		comparisons = append(comparisons, TaskComparison{
			Status:         ComparisonStatusRemoved,
			TaskID:         taskID,
			TaskName:       recorded.TaskName,
			RecordedStatus: recorded.Status,
		})
	}

	return comparisons
}
