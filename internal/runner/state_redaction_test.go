package runner

import (
	"context"
	"testing"

	"github.com/bluecadet/preflight/internal/action"
)

func TestSummarizeParamsDoesNotRedactOrdinaryPathValues(t *testing.T) {
	params := map[string]any{
		"path": `C:\Exhibits\Lobby`,
		"url":  "https://example.com/app",
	}

	summary, ok := SummarizeParams(params).(map[string]any)
	if !ok {
		t.Fatalf("expected map summary, got %T", SummarizeParams(params))
	}
	if summary["path"] != `C:\Exhibits\Lobby` {
		t.Fatalf("expected path to remain visible, got %#v", summary["path"])
	}
	if summary["url"] != "https://example.com/app" {
		t.Fatalf("expected url to remain visible, got %#v", summary["url"])
	}
}

func TestStateParamSummaryRedactsSecretRefUnderNeutralKey(t *testing.T) {
	source := map[string]any{
		"cmd": "secret:db-password",
	}
	resolved := map[string]any{
		"cmd": "hunter2",
	}

	summary, ok := StateParamSummary(source, resolved, nil, nil).(map[string]any)
	if !ok {
		t.Fatalf("expected map summary, got %T", StateParamSummary(source, resolved, nil, nil))
	}
	if summary["cmd"] != "[redacted]" {
		t.Fatalf("expected secret-derived neutral key to be redacted, got %#v", summary["cmd"])
	}
}

func TestStateParamHashIgnoresSecretContentChanges(t *testing.T) {
	source := map[string]any{
		"cmd": "secret:db-password",
	}
	first := map[string]any{
		"cmd": "hunter2",
	}
	second := map[string]any{
		"cmd": "correct horse battery staple",
	}

	if got, want := StateParamHash(source, first, nil, nil), StateParamHash(source, second, nil, nil); got != want {
		t.Fatalf("expected secret-derived hashes to match, got %q != %q", got, want)
	}
}

func TestStateParamSummaryRedactsPasswordSecretRef(t *testing.T) {
	source := map[string]any{
		"password": "secret:db-password",
	}
	resolved := map[string]any{
		"password": "hunter2",
	}

	summary, ok := StateParamSummary(source, resolved, nil, nil).(map[string]any)
	if !ok {
		t.Fatalf("expected map summary, got %T", StateParamSummary(source, resolved, nil, nil))
	}
	if summary["password"] != "[redacted]" {
		t.Fatalf("expected password to be redacted, got %#v", summary["password"])
	}
}

func TestStateParamSummaryRedactsBecomePassword(t *testing.T) {
	source := map[string]any{
		"user":     "kiosk",
		"password": "secret:become-password",
	}
	resolved := map[string]any{
		"user":     "kiosk",
		"password": "hunter2",
	}

	summary := NormalizeParamsForState(nil, nil, source, resolved)
	become := summary["become"].(map[string]any)
	if become["password"] != "[redacted]" {
		t.Fatalf("expected become password to be redacted, got %#v", become["password"])
	}
}

func TestBuildPlannedTaskStateKeepsBecomeSummaryUnwrapped(t *testing.T) {
	r := New(&mockTarget{}, emptyResolver(), Config{})
	pb := &action.Playbook{
		Name: "test",
		Tasks: []action.Task{
			{
				Name:   "echo",
				Become: map[string]any{"user": "kiosk"},
				Shell:  map[string]any{"cmd": "echo", "args": []any{"hello"}},
			},
		},
	}

	plan, err := r.Plan(context.Background(), pb)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	states, err := BuildPlannedTaskState(context.Background(), plan, &executionContext{}, nil)
	if err != nil {
		t.Fatalf("BuildPlannedTaskState returned error: %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("expected 1 planned state, got %d", len(states))
	}

	summary, ok := states[0].ParamSummary.(map[string]any)
	if !ok {
		t.Fatalf("expected map summary, got %T", states[0].ParamSummary)
	}
	become, ok := summary["become"].(map[string]any)
	if !ok {
		t.Fatalf("expected become map summary, got %#v", summary["become"])
	}
	if _, ok := become["become"]; ok {
		t.Fatalf("expected become summary to remain unwrapped, got %#v", become)
	}
	if become["user"] != "kiosk" {
		t.Fatalf("expected become user to be preserved, got %#v", become["user"])
	}
}
