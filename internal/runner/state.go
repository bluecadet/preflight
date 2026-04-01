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
	"strings"
	"time"

	"github.com/bluecadet/preflight/internal/secrets"
	"github.com/bluecadet/preflight/internal/target"
)

const stateVersion2 = 2

// TaskResult is the legacy per-task result shape kept for backward-compatible
// state loading.
type TaskResult struct {
	TaskID    string        `json:"task_id"`
	TaskName  string        `json:"task_name"`
	Status    target.Status `json:"status"`
	Timestamp time.Time     `json:"timestamp"`
	ParamHash string        `json:"param_hash"`
}

// TaskSnapshot is the v2 persisted state model used for comparison and audit.
type TaskSnapshot struct {
	TaskKey      string        `json:"task_key"`
	TaskName     string        `json:"task_name"`
	Module       string        `json:"module,omitempty"`
	DependsOn    []string      `json:"depends_on,omitempty"`
	TaskHash     string        `json:"task_hash,omitempty"`
	ParamHash    string        `json:"param_hash,omitempty"`
	ParamSummary any           `json:"param_summary,omitempty"`
	Status       target.Status `json:"status"`
	Message      string        `json:"message,omitempty"`
	Timestamp    time.Time     `json:"timestamp"`
}

// State holds persisted runner state written to disk after each apply.
type State struct {
	Version     int                     `json:"version,omitempty"`
	LastApplied time.Time               `json:"last_applied"`
	Tasks       map[string]TaskSnapshot `json:"tasks,omitempty"`
	Results     map[string]TaskResult   `json:"results,omitempty"`
}

type ComparisonStatus string

const (
	ComparisonStatusNew        ComparisonStatus = "NEW"
	ComparisonStatusChanged    ComparisonStatus = "CHANGED"
	ComparisonStatusUnchanged  ComparisonStatus = "UNCHANGED"
	ComparisonStatusRemoved    ComparisonStatus = "REMOVED"
	ComparisonStatusStatusOnly ComparisonStatus = "STATUS-ONLY"
)

type PlannedTaskState struct {
	TaskKey      string
	TaskName     string
	Module       string
	DependsOn    []string
	TaskHash     string
	ParamHash    string
	ParamSummary any
}

type TaskComparison struct {
	Status          ComparisonStatus
	TaskKey         string
	TaskName        string
	Module          string
	RecordedStatus  target.Status
	RecordedSummary any
	PlannedSummary  any
}

// LoadState reads a state file from path. If the file does not exist, an empty
// State is returned (not an error).
func LoadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &State{Version: stateVersion2, Tasks: make(map[string]TaskSnapshot)}, nil
		}
		return nil, fmt.Errorf("state: read %q: %w", path, err)
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("state: parse %q: %w", path, err)
	}
	s.normalise()
	return &s, nil
}

// Save writes the state to path as JSON. The file is written atomically by
// writing to a temp file and renaming it.
func (s *State) Save(path string) error {
	s.normalise()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("state: marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("state: mkdir %q: %w", filepath.Dir(path), err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("state: write %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("state: rename %q → %q: %w", tmp, path, err)
	}
	return nil
}

// Record preserves legacy result-only writes by promoting them to v2 snapshots.
func (s *State) Record(result TaskResult) {
	s.RecordTask(TaskSnapshot{
		TaskKey:   result.TaskID,
		TaskName:  result.TaskName,
		Status:    result.Status,
		Timestamp: result.Timestamp,
		ParamHash: result.ParamHash,
		TaskHash:  hashValue(map[string]any{"task_key": result.TaskID, "task_name": result.TaskName, "param_hash": result.ParamHash}),
	})
}

// RecordTask stores a v2 snapshot in the state, keyed by stable task key.
func (s *State) RecordTask(snapshot TaskSnapshot) {
	s.normalise()
	if snapshot.TaskKey == "" {
		return
	}
	if len(snapshot.DependsOn) > 0 {
		snapshot.DependsOn = append([]string{}, snapshot.DependsOn...)
	}
	s.Tasks[snapshot.TaskKey] = snapshot
}

func (s *State) normalise() {
	if s.Version == 0 {
		s.Version = stateVersion2
	}
	if s.Tasks == nil {
		s.Tasks = make(map[string]TaskSnapshot)
	}
	if len(s.Tasks) == 0 && len(s.Results) > 0 {
		for key, result := range s.Results {
			s.Tasks[key] = TaskSnapshot{
				TaskKey:   result.TaskID,
				TaskName:  result.TaskName,
				Status:    result.Status,
				Timestamp: result.Timestamp,
				ParamHash: result.ParamHash,
				TaskHash:  hashValue(map[string]any{"task_key": result.TaskID, "task_name": result.TaskName, "param_hash": result.ParamHash}),
			}
		}
	}
}

// ParamHash computes a SHA256 hash of the params map as a hex string.
func ParamHash(params map[string]any) string {
	return hashValue(params)
}

func BuildPlannedTaskState(ctx context.Context, plan *ExecutionPlan, execCtx *executionContext, resolver *secrets.Resolver) ([]PlannedTaskState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, fmt.Errorf("state: nil execution plan")
	}
	if execCtx == nil {
		execCtx = &executionContext{}
	}

	tasks := make([]PlannedTaskState, 0, len(plan.Tasks))
	nameToKey := make(map[string]string, len(plan.Tasks))
	type renderedTask struct {
		name   string
		params map[string]any
	}
	renderedTasks := make([]renderedTask, 0, len(plan.Tasks))
	for _, task := range plan.Tasks {
		params, taskName, err := renderTaskParams(task, execCtx)
		if err != nil {
			return nil, fmt.Errorf("state: task %q: %w", task.Name, err)
		}
		renderedTasks = append(renderedTasks, renderedTask{
			name:   taskName,
			params: params,
		})
		nameToKey[task.Name] = task.ID
	}

	for idx, task := range plan.Tasks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rendered := renderedTasks[idx]
		params := rendered.params
		if resolver != nil && resolver.HasProviders() {
			resolved, err := resolver.ResolveMap(ctx, params)
			if err != nil {
				return nil, fmt.Errorf("state: task %q: %w", rendered.name, err)
			}
			params = resolved
		}

		dependsOn := make([]string, 0, len(task.DependsOn))
		for _, depName := range task.DependsOn {
			if depKey, ok := nameToKey[depName]; ok {
				dependsOn = append(dependsOn, depKey)
			}
		}
		slices.Sort(dependsOn)

		paramHash := ParamHash(params)
		tasks = append(tasks, PlannedTaskState{
			TaskKey:      task.ID,
			TaskName:     rendered.name,
			Module:       task.Module,
			DependsOn:    dependsOn,
			ParamHash:    paramHash,
			ParamSummary: SummarizeParams(params),
			TaskHash: hashValue(map[string]any{
				"task_key":   task.ID,
				"task_name":  rendered.name,
				"module":     task.Module,
				"depends_on": dependsOn,
				"param_hash": paramHash,
			}),
		})
	}
	return tasks, nil
}

func ComparePlannedTasks(planned []PlannedTaskState, state *State) []TaskComparison {
	if state == nil {
		state = &State{}
	}
	state.normalise()

	comparisons := make([]TaskComparison, 0, len(planned)+len(state.Tasks))
	seen := make(map[string]struct{}, len(planned))

	for _, task := range planned {
		seen[task.TaskKey] = struct{}{}
		recorded, ok := state.Tasks[task.TaskKey]
		if !ok {
			comparisons = append(comparisons, TaskComparison{
				Status:         ComparisonStatusNew,
				TaskKey:        task.TaskKey,
				TaskName:       task.TaskName,
				Module:         task.Module,
				PlannedSummary: task.ParamSummary,
			})
			continue
		}

		status := ComparisonStatusUnchanged
		switch {
		case recorded.TaskHash != "" && task.TaskHash != recorded.TaskHash:
			status = ComparisonStatusChanged
		case recorded.TaskHash == "" && recorded.ParamHash != task.ParamHash:
			status = ComparisonStatusChanged
		case recorded.Status == target.StatusFailed || recorded.Status == target.StatusSkipped:
			status = ComparisonStatusStatusOnly
		}

		comparisons = append(comparisons, TaskComparison{
			Status:          status,
			TaskKey:         task.TaskKey,
			TaskName:        task.TaskName,
			Module:          task.Module,
			RecordedStatus:  recorded.Status,
			RecordedSummary: recorded.ParamSummary,
			PlannedSummary:  task.ParamSummary,
		})
	}

	removedKeys := make([]string, 0, len(state.Tasks))
	for taskKey := range state.Tasks {
		if _, ok := seen[taskKey]; ok {
			continue
		}
		removedKeys = append(removedKeys, taskKey)
	}
	slices.Sort(removedKeys)
	for _, taskKey := range removedKeys {
		recorded := state.Tasks[taskKey]
		comparisons = append(comparisons, TaskComparison{
			Status:          ComparisonStatusRemoved,
			TaskKey:         taskKey,
			TaskName:        recorded.TaskName,
			Module:          recorded.Module,
			RecordedStatus:  recorded.Status,
			RecordedSummary: recorded.ParamSummary,
		})
	}

	return comparisons
}

// SummarizeParams produces a redacted, JSON-friendly summary of parameters for
// state diff output.
func SummarizeParams(params map[string]any) any {
	if params == nil {
		return nil
	}
	return summarizeValue("", params)
}

func summarizeValue(key string, value any) any {
	if secretishKey(key) {
		return secrets.RedactString("")
	}

	switch t := value.(type) {
	case string:
		if secrets.IsRef(t) {
			return secrets.RedactString(t)
		}
		return t
	case map[string]any:
		out := make(map[string]any, len(t))
		for childKey, childValue := range t {
			out[childKey] = summarizeValue(childKey, childValue)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = summarizeValue(key, item)
		}
		return out
	default:
		return t
	}
}

func containsSecretValue(value any) bool {
	switch t := value.(type) {
	case string:
		return secrets.IsRef(t)
	case map[string]any:
		for key, child := range t {
			if secretishKey(key) || containsSecretValue(child) {
				return true
			}
		}
	case []any:
		return slices.ContainsFunc(t, containsSecretValue)
	}
	return false
}

func secretishKey(key string) bool {
	lower := strings.ToLower(key)
	for _, token := range []string{"password", "secret", "token", "private_key", "credential", "_from"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func hashValue(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}
