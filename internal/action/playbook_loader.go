package action

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// LoadPlaybookFile reads a playbook and recursively merges any imported
// playbooks depth-first. Imported vars are merged first, then overridden by the
// importing playbook's vars; imported tasks are prepended in listed order.
func LoadPlaybookFile(path string) (*Playbook, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("playbook: resolve %q: %w", path, err)
	}
	return loadPlaybookFile(absPath, nil)
}

func loadPlaybookFile(path string, chain []string) (*Playbook, error) {
	cleanPath := filepath.Clean(path)
	if idx := slices.Index(chain, cleanPath); idx >= 0 {
		cycle := append(append([]string{}, chain[idx:]...), cleanPath)
		return nil, fmt.Errorf("playbook: import cycle detected: %s", strings.Join(cycle, " -> "))
	}

	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("playbook: read %q: %w", cleanPath, err)
	}

	current, err := ParsePlaybook(data)
	if err != nil {
		return nil, fmt.Errorf("playbook: parse %q: %w", cleanPath, err)
	}

	// Start from a full copy of the importing playbook so any field with
	// plain "current wins" semantics rides along automatically, then reset
	// the fields that need merge semantics instead of straight assignment
	// (Vars, Tasks, Defaults) or that are meaningless post-merge (Import,
	// already fully resolved into the fields above by the time we return).
	merged := *current
	merged.Vars = make(map[string]any)
	merged.Tasks = make([]Task, 0, len(current.Tasks))
	merged.Defaults = TaskDefaults{}
	merged.Import = nil

	nextChain := append(append([]string{}, chain...), cleanPath)
	for _, rawImport := range current.Import {
		importPath := filepath.FromSlash(rawImport)
		if !filepath.IsAbs(importPath) {
			importPath = filepath.Join(filepath.Dir(cleanPath), importPath)
		}

		absImportPath, err := filepath.Abs(importPath)
		if err != nil {
			return nil, fmt.Errorf("playbook: resolve import %q from %q: %w", rawImport, cleanPath, err)
		}

		imported, err := loadPlaybookFile(absImportPath, nextChain)
		if err != nil {
			return nil, fmt.Errorf("playbook: import %q from %q: %w", rawImport, cleanPath, err)
		}

		maps.Copy(merged.Vars, imported.Vars)
		merged.Tasks = append(merged.Tasks, imported.Tasks...)
		mergeTaskDefaults(&merged.Defaults, imported.Defaults)
	}

	maps.Copy(merged.Vars, current.Vars)
	merged.Tasks = append(merged.Tasks, current.Tasks...)
	mergeTaskDefaults(&merged.Defaults, current.Defaults)

	return &merged, nil
}

// mergeTaskDefaults layers override's fields onto dst using the same
// later-wins precedent as Vars above: keys present in override replace keys
// of the same name already in dst, everything else in dst is preserved.
func mergeTaskDefaults(dst *TaskDefaults, override TaskDefaults) {
	if len(override.Become) == 0 {
		return
	}
	if dst.Become == nil {
		dst.Become = make(map[string]any, len(override.Become))
	}
	maps.Copy(dst.Become, override.Become)
}
