//go:build devtools

package output

import "testing"

func TestPreviewScenarios_AreUniqueAndComplex(t *testing.T) {
	scenarios := PreviewScenarios()
	if len(scenarios) < 10 {
		t.Fatalf("expected at least 10 preview scenarios, got %d", len(scenarios))
	}

	seen := make(map[string]struct{}, len(scenarios))
	for _, scenario := range scenarios {
		if scenario.Name == "" {
			t.Fatal("expected scenario names to be populated")
		}
		if _, exists := seen[scenario.Name]; exists {
			t.Fatalf("duplicate preview scenario %q", scenario.Name)
		}
		seen[scenario.Name] = struct{}{}
	}

	for _, required := range []string{
		"run-deep-playbook",
		"run-host-overflow",
		"screen-plan-tabs",
		"screen-facts-tabs",
		"screen-action-info",
	} {
		if _, ok := seen[required]; !ok {
			t.Fatalf("expected preview scenario %q to exist", required)
		}
	}
}
