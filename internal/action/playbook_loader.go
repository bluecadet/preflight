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

	merged := &Playbook{
		Name:        current.Name,
		Description: current.Description,
		Vars:        make(map[string]any),
		Tasks:       make([]Task, 0, len(current.Tasks)),
	}

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
	}

	maps.Copy(merged.Vars, current.Vars)
	merged.Tasks = append(merged.Tasks, current.Tasks...)

	return merged, nil
}
