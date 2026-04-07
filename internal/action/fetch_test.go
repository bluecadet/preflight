package action_test

import (
	"context"
	"testing"

	"github.com/bluecadet/preflight/internal/action"
)

// staticResolver resolves a fixed set of refs without requiring a fetch step.
// It simulates local or embedded resolvers that serve actions directly.
type staticResolver struct {
	actions map[string]*action.Action
}

func (r *staticResolver) Name() string { return "static" }
func (r *staticResolver) Resolve(_ context.Context, ref string) (*action.Action, error) {
	a, ok := r.actions[ref]
	if !ok {
		return nil, nil
	}
	return a, nil
}

// fetchTracker is a minimal Fetcher that records which refs were fetched and
// serves actions for known remote refs.
type fetchTracker struct {
	actions map[string]*action.Action
	fetched []string
}

func (f *fetchTracker) Name() string { return "fetch-tracker" }
func (f *fetchTracker) Resolve(_ context.Context, ref string) (*action.Action, error) {
	return nil, nil // let the static resolver handle non-remote refs
}
func (f *fetchTracker) Fetch(_ context.Context, ref string) (*action.FetchResult, error) {
	a, ok := f.actions[ref]
	if !ok {
		return nil, nil
	}
	f.fetched = append(f.fetched, ref)
	return &action.FetchResult{
		Entry:  action.LockEntry{Ref: ref, SHA: "sha-" + ref, Pinned: ref},
		Action: a,
	}, nil
}

func TestFetchRefs_LocalRootWithRemoteChild(t *testing.T) {
	remoteRef := "github.com/acme/actions/child@v1"
	localRef := "local/wrapper"

	localAction := &action.Action{
		Name:  "wrapper",
		Tasks: []action.Task{{Name: "child", Uses: remoteRef}},
	}
	remoteAction := &action.Action{
		Name:  "child",
		Tasks: []action.Task{{Name: "echo", Shell: map[string]any{"cmd": "echo hello"}}},
	}

	tracker := &fetchTracker{
		actions: map[string]*action.Action{remoteRef: remoteAction},
	}
	chain := action.Chain{
		&staticResolver{actions: map[string]*action.Action{localRef: localAction}},
		tracker,
	}

	entries, err := action.FetchRefs(context.Background(), chain, []string{localRef})
	if err != nil {
		t.Fatalf("FetchRefs: %v", err)
	}
	if len(entries) != 1 || entries[0].Ref != remoteRef {
		t.Errorf("expected lock entry for %q, got %v", remoteRef, entries)
	}
	if len(tracker.fetched) != 1 || tracker.fetched[0] != remoteRef {
		t.Errorf("expected fetch of %q, got %v", remoteRef, tracker.fetched)
	}
}

func TestFetchRefs_EmbeddedRootWithRemoteChild(t *testing.T) {
	remoteRef := "github.com/acme/actions/tool@v2"
	embeddedRef := "preflight/some-action"

	embeddedAction := &action.Action{
		Name:  "some-action",
		Tasks: []action.Task{{Name: "tool", Uses: remoteRef}},
	}
	remoteAction := &action.Action{
		Name:  "tool",
		Tasks: []action.Task{{Name: "run", Shell: map[string]any{"cmd": "tool.exe"}}},
	}

	tracker := &fetchTracker{
		actions: map[string]*action.Action{remoteRef: remoteAction},
	}
	chain := action.Chain{
		&staticResolver{actions: map[string]*action.Action{embeddedRef: embeddedAction}},
		tracker,
	}

	entries, err := action.FetchRefs(context.Background(), chain, []string{embeddedRef})
	if err != nil {
		t.Fatalf("FetchRefs: %v", err)
	}
	if len(entries) != 1 || entries[0].Ref != remoteRef {
		t.Errorf("expected lock entry for %q, got %v", remoteRef, entries)
	}
	if len(tracker.fetched) != 1 || tracker.fetched[0] != remoteRef {
		t.Errorf("expected fetch of %q, got %v", remoteRef, tracker.fetched)
	}
}

func TestFetchRefs_DeepChain_LocalThenRemoteThenRemote(t *testing.T) {
	localRef := "local/root"
	remote1Ref := "github.com/acme/actions/mid@v1"
	remote2Ref := "github.com/acme/actions/leaf@v1"

	localAction := &action.Action{
		Name:  "root",
		Tasks: []action.Task{{Name: "mid", Uses: remote1Ref}},
	}
	remote1Action := &action.Action{
		Name:  "mid",
		Tasks: []action.Task{{Name: "leaf", Uses: remote2Ref}},
	}
	remote2Action := &action.Action{
		Name:  "leaf",
		Tasks: []action.Task{{Name: "run", Shell: map[string]any{"cmd": "leaf.exe"}}},
	}

	tracker := &fetchTracker{
		actions: map[string]*action.Action{
			remote1Ref: remote1Action,
			remote2Ref: remote2Action,
		},
	}
	chain := action.Chain{
		&staticResolver{actions: map[string]*action.Action{localRef: localAction}},
		tracker,
	}

	entries, err := action.FetchRefs(context.Background(), chain, []string{localRef})
	if err != nil {
		t.Fatalf("FetchRefs: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 lock entries, got %d: %v", len(entries), entries)
	}
	if len(tracker.fetched) != 2 {
		t.Errorf("expected 2 fetches, got %v", tracker.fetched)
	}
}

func TestFetchRefs_NoDuplicateFetches(t *testing.T) {
	sharedRef := "github.com/acme/actions/shared@v1"
	local1Ref := "local/action1"
	local2Ref := "local/action2"

	sharedAction := &action.Action{
		Name:  "shared",
		Tasks: []action.Task{{Name: "run", Shell: map[string]any{"cmd": "shared.exe"}}},
	}

	tracker := &fetchTracker{
		actions: map[string]*action.Action{sharedRef: sharedAction},
	}
	chain := action.Chain{
		&staticResolver{actions: map[string]*action.Action{
			local1Ref: {Name: "action1", Tasks: []action.Task{{Name: "s", Uses: sharedRef}}},
			local2Ref: {Name: "action2", Tasks: []action.Task{{Name: "s", Uses: sharedRef}}},
		}},
		tracker,
	}

	_, err := action.FetchRefs(context.Background(), chain, []string{local1Ref, local2Ref})
	if err != nil {
		t.Fatalf("FetchRefs: %v", err)
	}
	if len(tracker.fetched) != 1 {
		t.Errorf("expected shared remote fetched exactly once, got %v", tracker.fetched)
	}
}
