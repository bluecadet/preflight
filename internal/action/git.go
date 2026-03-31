package action

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// GitResolver fetches action definitions from remote Git repositories.
// The ref format is "host/org/repo[/path/to/action]@version" or
// "host/org/repo[/path/to/action]@sha".
type GitResolver struct {
	CacheDir     string
	LockfilePath string
	client       gitClient
}

type gitClient interface {
	Checkout(ctx context.Context, repoURL, revision, dstDir string) (string, error)
}

type goGitClient struct{}

// NewGitResolver creates a GitResolver that caches fetched actions in cacheDir
// and consults the project lockfile at lockfilePath.
func NewGitResolver(cacheDir, lockfilePath string) *GitResolver {
	return newGitResolverWithClient(cacheDir, lockfilePath, goGitClient{})
}

func newGitResolverWithClient(cacheDir, lockfilePath string, client gitClient) *GitResolver {
	return &GitResolver{
		CacheDir:     cacheDir,
		LockfilePath: lockfilePath,
		client:       client,
	}
}

func (r *GitResolver) Name() string { return "git" }

func (r *GitResolver) Resolve(_ context.Context, ref string) (*Action, error) {
	remote, err := ParseRemoteRef(ref)
	if err != nil {
		return nil, nil
	}

	resolvedRef, err := r.pinnedRefForResolve(remote)
	if err != nil {
		return nil, err
	}

	action, err := loadActionFromCache(r.CacheDir, resolvedRef)
	if err != nil {
		return nil, fmt.Errorf("git resolver: %w", err)
	}
	if action == nil {
		return nil, &RemoteCacheMissError{Ref: ref}
	}
	return action, nil
}

// Fetch downloads the remote action into the pinned cache and updates the
// project lockfile.
func (r *GitResolver) Fetch(ctx context.Context, ref string) (*FetchResult, error) {
	remote, err := ParseRemoteRef(ref)
	if err != nil {
		return nil, nil
	}

	if cached, entry, err := r.loadCachedResult(remote); err == nil && cached != nil {
		return &FetchResult{Entry: entry, Action: cached}, nil
	} else if err != nil && !IsRemoteCacheMiss(err) {
		return nil, err
	}

	if r.client == nil {
		return nil, fmt.Errorf("git resolver: no git client configured")
	}

	tmpDir, err := os.MkdirTemp("", "preflight-action-fetch-*")
	if err != nil {
		return nil, fmt.Errorf("git resolver: create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	sha, err := r.checkoutRemote(ctx, remote, tmpDir)
	if err != nil {
		return nil, err
	}

	pinnedRef := remote.PinnedRef(sha)
	srcDir := remote.SourceDir(tmpDir)
	actionDir, err := actionDirForRef(r.CacheDir, pinnedRef)
	if err != nil {
		return nil, fmt.Errorf("git resolver: %w", err)
	}

	cachedFile, err := actionFileForRef(r.CacheDir, pinnedRef)
	if err != nil {
		return nil, fmt.Errorf("git resolver: %w", err)
	}
	if _, err := os.Stat(cachedFile); err != nil {
		if !errorsIsNotExist(err) {
			return nil, fmt.Errorf("git resolver: stat cached action %q: %w", pinnedRef, err)
		}
		if _, err := os.Stat(filepath.Join(srcDir, "action.yml")); err != nil {
			return nil, fmt.Errorf("git resolver: action %q does not contain action.yml: %w", ref, err)
		}
		if err := copyDir(srcDir, actionDir); err != nil {
			return nil, fmt.Errorf("git resolver: cache %q: %w", ref, err)
		}
	}

	action, err := loadActionFromCache(r.CacheDir, pinnedRef)
	if err != nil {
		return nil, fmt.Errorf("git resolver: %w", err)
	}
	if action == nil {
		return nil, &RemoteCacheMissError{Ref: pinnedRef}
	}

	entry := LockEntry{Ref: ref, SHA: sha, Pinned: pinnedRef}
	if err := r.saveLockEntry(entry); err != nil {
		return nil, err
	}

	return &FetchResult{Entry: entry, Action: action}, nil
}

// RemoteCacheMissError reports that a remote action is not available from the
// current cache and lockfile state.
type RemoteCacheMissError struct {
	Ref string
}

func (e *RemoteCacheMissError) Error() string {
	return fmt.Sprintf("remote action %q is not cached; run 'preflight action fetch %s' or 'preflight apply ...' to fetch it", e.Ref, e.Ref)
}

// IsRemoteCacheMiss reports whether err is a remote cache miss.
func IsRemoteCacheMiss(err error) bool {
	var miss *RemoteCacheMissError
	return errors.As(err, &miss)
}

func (r *GitResolver) pinnedRefForResolve(remote *RemoteRef) (string, error) {
	if remote.IsPinned() {
		return remote.Original, nil
	}

	lockfile, err := r.loadLockfile()
	if err != nil {
		return "", err
	}
	entry, ok := lockfile.Lookup(remote.Original)
	if !ok {
		return "", &RemoteCacheMissError{Ref: remote.Original}
	}
	if entry.Pinned != "" {
		return entry.Pinned, nil
	}
	if entry.SHA == "" {
		return "", fmt.Errorf("git resolver: lock entry for %q is missing a pinned SHA", remote.Original)
	}
	return remote.PinnedRef(entry.SHA), nil
}

func (r *GitResolver) loadCachedResult(remote *RemoteRef) (*Action, LockEntry, error) {
	pinnedRef, err := r.pinnedRefForResolve(remote)
	if err != nil {
		return nil, LockEntry{}, err
	}

	action, err := loadActionFromCache(r.CacheDir, pinnedRef)
	if err != nil {
		return nil, LockEntry{}, fmt.Errorf("git resolver: %w", err)
	}
	if action == nil {
		return nil, LockEntry{}, &RemoteCacheMissError{Ref: remote.Original}
	}

	entry := LockEntry{Ref: remote.Original, SHA: remote.Revision, Pinned: pinnedRef}
	if !remote.IsPinned() {
		lockfile, err := r.loadLockfile()
		if err != nil {
			return nil, LockEntry{}, err
		}
		if locked, ok := lockfile.Lookup(remote.Original); ok {
			entry = locked
		}
	}
	if entry.SHA == "" && remote.IsPinned() {
		entry.SHA = remote.Revision
		entry.Pinned = remote.Original
	}
	return action, entry, nil
}

func (r *GitResolver) loadLockfile() (*Lockfile, error) {
	if strings.TrimSpace(r.LockfilePath) == "" {
		return &Lockfile{Actions: make(map[string]LockEntry)}, nil
	}
	lockfile, err := LoadLockfile(r.LockfilePath)
	if err != nil {
		return nil, fmt.Errorf("git resolver: %w", err)
	}
	return lockfile, nil
}

func (r *GitResolver) saveLockEntry(entry LockEntry) error {
	if strings.TrimSpace(r.LockfilePath) == "" {
		return nil
	}
	lockfile, err := r.loadLockfile()
	if err != nil {
		return err
	}
	if err := lockfile.Pin(entry.Ref, entry.SHA); err != nil {
		return fmt.Errorf("git resolver: %w", err)
	}
	if err := lockfile.Save(r.LockfilePath); err != nil {
		return fmt.Errorf("git resolver: %w", err)
	}
	return nil
}

func (r *GitResolver) checkoutRemote(ctx context.Context, remote *RemoteRef, dstDir string) (string, error) {
	var lastErr error
	for _, repoURL := range remote.CloneURLs() {
		sha, err := r.client.Checkout(ctx, repoURL, remote.Revision, dstDir)
		if err == nil {
			return sha, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no clone URLs generated for %q", remote.Original)
	}
	return "", fmt.Errorf("git resolver: fetch %q: %w", remote.Original, lastErr)
}

func (goGitClient) Checkout(ctx context.Context, repoURL, revision, dstDir string) (string, error) {
	repo, err := git.PlainCloneContext(ctx, dstDir, false, &git.CloneOptions{
		URL: repoURL,
	})
	if err != nil {
		return "", err
	}

	hash, err := resolveGitRevision(repo, revision)
	if err != nil {
		return "", err
	}

	worktree, err := repo.Worktree()
	if err != nil {
		return "", err
	}
	if err := worktree.Checkout(&git.CheckoutOptions{
		Hash:  *hash,
		Force: true,
	}); err != nil {
		return "", err
	}

	return hash.String(), nil
}

func resolveGitRevision(repo *git.Repository, revision string) (*plumbing.Hash, error) {
	candidates := []plumbing.Revision{
		plumbing.Revision(revision),
		plumbing.Revision("refs/tags/" + revision),
		plumbing.Revision("refs/heads/" + revision),
		plumbing.Revision("refs/remotes/origin/" + revision),
		plumbing.Revision("origin/" + revision),
	}
	for _, candidate := range candidates {
		hash, err := repo.ResolveRevision(candidate)
		if err == nil {
			return hash, nil
		}
	}
	return nil, fmt.Errorf("unknown revision %q", revision)
}

func errorsIsNotExist(err error) bool {
	return os.IsNotExist(err)
}
