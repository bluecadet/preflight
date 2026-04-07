package action

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

type fakeGitClient struct {
	checkouts []fakeCheckout
}

type fakeCheckout struct {
	repoURL  string
	revision string
	sha      string
	files    map[string]string
}

func (f *fakeGitClient) Checkout(_ context.Context, repoURL, revision, dstDir string) (string, error) {
	for _, checkout := range f.checkouts {
		if checkout.repoURL != repoURL || checkout.revision != revision {
			continue
		}
		for rel, contents := range checkout.files {
			path := filepath.Join(dstDir, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return "", err
			}
			if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
				return "", err
			}
		}
		return checkout.sha, nil
	}
	return "", os.ErrNotExist
}

func TestGitResolverResolveUsesPinnedCacheFromLockfile(t *testing.T) {
	cacheDir := t.TempDir()
	lockfilePath := filepath.Join(t.TempDir(), LockfileName)
	ref := "github.com/acme/actions/signage@v1.2.3"
	sha := "0123456789abcdef0123456789abcdef01234567"
	pinnedRef := "github.com/acme/actions/signage@" + sha

	writeCachedAction(t, cacheDir, pinnedRef, `
name: signage
tasks:
  - name: run
    shell:
      cmd: echo
`)

	lockfile := &Lockfile{Actions: make(map[string]LockEntry)}
	if err := lockfile.Pin(ref, sha); err != nil {
		t.Fatalf("Pin: %v", err)
	}
	if err := lockfile.Save(lockfilePath); err != nil {
		t.Fatalf("Save: %v", err)
	}

	resolver := newGitResolverWithClient(cacheDir, lockfilePath, &fakeGitClient{})
	action, err := resolver.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if action == nil || action.Name != "signage" {
		t.Fatalf("expected signage action, got %#v", action)
	}
}

func TestGitResolverResolvePinnedRefFromCacheWithoutLockfile(t *testing.T) {
	cacheDir := t.TempDir()
	ref := "github.com/acme/actions/signage@0123456789abcdef0123456789abcdef01234567"
	writeCachedAction(t, cacheDir, ref, `
name: signage
tasks:
  - name: run
    shell:
      cmd: echo
`)

	resolver := newGitResolverWithClient(cacheDir, "", &fakeGitClient{})
	action, err := resolver.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if action == nil || action.Name != "signage" {
		t.Fatalf("expected signage action, got %#v", action)
	}
}

func TestGitResolverResolveReturnsCacheMiss(t *testing.T) {
	resolver := newGitResolverWithClient(t.TempDir(), filepath.Join(t.TempDir(), LockfileName), &fakeGitClient{})
	_, err := resolver.Resolve(context.Background(), "github.com/acme/actions/signage@v1")
	if !IsRemoteCacheMiss(err) {
		t.Fatalf("expected remote cache miss, got %v", err)
	}
}

func TestGitResolverFetchCachesActionAndUpdatesLockfile(t *testing.T) {
	cacheDir := t.TempDir()
	projectDir := t.TempDir()
	lockfilePath := filepath.Join(projectDir, LockfileName)
	ref := "github.com/acme/actions/signage@v1.2.3"
	sha := "0123456789abcdef0123456789abcdef01234567"

	client := &fakeGitClient{
		checkouts: []fakeCheckout{{
			repoURL:  "https://github.com/acme/actions.git",
			revision: "v1.2.3",
			sha:      sha,
			files: map[string]string{
				"signage/action.yml": `
name: signage
tasks:
  - name: child
    uses: github.com/acme/child/setup@v9
`,
				"signage/files/config.txt": "hello",
			},
		}},
	}

	resolver := newGitResolverWithClient(cacheDir, lockfilePath, client)
	result, err := resolver.Fetch(context.Background(), ref)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if result.Entry.SHA != sha {
		t.Fatalf("SHA: got %q want %q", result.Entry.SHA, sha)
	}
	pinnedRef := "github.com/acme/actions/signage@" + sha
	if result.Entry.Pinned != pinnedRef {
		t.Fatalf("Pinned: got %q want %q", result.Entry.Pinned, pinnedRef)
	}
	cachedDir, err := actionDirForRef(cacheDir, pinnedRef)
	if err != nil {
		t.Fatalf("actionDirForRef: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cachedDir, "files", "config.txt")); err != nil {
		t.Fatalf("expected cached sibling file: %v", err)
	}

	lockfile, err := LoadLockfile(lockfilePath)
	if err != nil {
		t.Fatalf("LoadLockfile: %v", err)
	}
	entry, ok := lockfile.Lookup(ref)
	if !ok {
		t.Fatalf("expected lock entry for %q", ref)
	}
	if entry.Pinned != pinnedRef {
		t.Fatalf("lockfile pinned: got %q want %q", entry.Pinned, pinnedRef)
	}
}

type fakeFetchResolver struct {
	results map[string]*FetchResult
	calls   []string
}

func (r *fakeFetchResolver) Name() string { return "fake-fetch" }

func (r *fakeFetchResolver) Resolve(_ context.Context, ref string) (*Action, error) {
	// Non-remote refs (local/embedded) are resolvable without fetching.
	if !IsRemoteRef(ref) {
		return &Action{}, nil
	}
	return nil, nil
}

func (r *fakeFetchResolver) Fetch(_ context.Context, ref string) (*FetchResult, error) {
	r.calls = append(r.calls, ref)
	return r.results[ref], nil
}

func TestFetchRefsRecursesThroughNestedRemoteUses(t *testing.T) {
	rootRef := "github.com/acme/actions/root@v1"
	childRef := "github.com/acme/actions/child@v2"
	resolver := &fakeFetchResolver{
		results: map[string]*FetchResult{
			rootRef: {
				Entry: LockEntry{Ref: rootRef, SHA: "a", Pinned: "github.com/acme/actions/root@a"},
				Action: &Action{Tasks: []Task{
					{Name: "local", Uses: "preflight/autologin"},
					{Name: "child", Uses: childRef},
				}},
			},
			childRef: {
				Entry:  LockEntry{Ref: childRef, SHA: "b", Pinned: "github.com/acme/actions/child@b"},
				Action: &Action{},
			},
		},
	}

	entries, err := FetchRefs(context.Background(), Chain{resolver}, []string{rootRef})
	if err != nil {
		t.Fatalf("FetchRefs: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 fetched entries, got %d", len(entries))
	}
	if len(resolver.calls) != 2 {
		t.Fatalf("expected 2 fetch calls, got %d", len(resolver.calls))
	}
	if resolver.calls[0] != rootRef || resolver.calls[1] != childRef {
		t.Fatalf("unexpected fetch order: %#v", resolver.calls)
	}
}

func writeCachedAction(t *testing.T, cacheDir, ref, yaml string) {
	t.Helper()
	dir, err := actionDirForRef(cacheDir, ref)
	if err != nil {
		t.Fatalf("actionDirForRef(%q): %v", ref, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "action.yml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", filepath.Join(dir, "action.yml"), err)
	}
}
