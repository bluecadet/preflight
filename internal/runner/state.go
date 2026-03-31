package runner

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/claytercek/preflight/internal/target"
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
func ParamHash(params map[string]interface{}) string {
	data, err := json.Marshal(params)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}
