package action

import (
	"fmt"
	"path/filepath"
	"strings"
)

// LockfileName is the project lockfile that pins remote action refs to exact
// commit SHAs.
const LockfileName = "preflight.lock"

// RemoteRef is a parsed remote action ref of the form
// host/org/repo[/path/to/action]@rev.
type RemoteRef struct {
	Original   string
	Repository string
	ActionPath string
	Revision   string
}

// ParseRemoteRef parses a remote action ref into repository, optional action
// path, and revision components.
func ParseRemoteRef(ref string) (*RemoteRef, error) {
	at := strings.LastIndex(ref, "@")
	if at <= 0 || at == len(ref)-1 {
		return nil, fmt.Errorf("remote ref %q must include @<revision>", ref)
	}

	pathPart := strings.Trim(ref[:at], "/")
	revision := strings.TrimSpace(ref[at+1:])
	if revision == "" {
		return nil, fmt.Errorf("remote ref %q must include a non-empty revision", ref)
	}

	segments := strings.Split(pathPart, "/")
	if len(segments) < 3 {
		return nil, fmt.Errorf("remote ref %q must include host/org/repo before @<revision>", ref)
	}
	if !strings.Contains(segments[0], ".") {
		return nil, fmt.Errorf("remote ref %q must start with a hostname", ref)
	}
	for _, segment := range segments[:3] {
		if segment == "" {
			return nil, fmt.Errorf("remote ref %q contains an empty repository path segment", ref)
		}
	}
	for _, segment := range segments[3:] {
		if segment == ".." || strings.ContainsAny(segment, `/\`) {
			return nil, fmt.Errorf("remote ref %q contains an invalid action path segment %q", ref, segment)
		}
	}

	parsed := &RemoteRef{
		Original:   ref,
		Repository: strings.Join(segments[:3], "/"),
		ActionPath: strings.Join(segments[3:], "/"),
		Revision:   revision,
	}
	return parsed, nil
}

// IsRemoteRef reports whether ref matches the supported remote action contract.
func IsRemoteRef(ref string) bool {
	_, err := ParseRemoteRef(ref)
	return err == nil
}

// PinnedRef returns the canonical SHA-pinned form of the ref while preserving
// the in-repo action path.
func (r *RemoteRef) PinnedRef(sha string) string {
	base := r.Repository
	if r.ActionPath != "" {
		base += "/" + r.ActionPath
	}
	return base + "@" + sha
}

// CloneURLs returns candidate HTTPS clone URLs for the remote repository.
func (r *RemoteRef) CloneURLs() []string {
	return []string{
		"https://" + r.Repository + ".git",
		"https://" + r.Repository,
	}
}

// SourceDir resolves the action directory inside a checked-out repository.
func (r *RemoteRef) SourceDir(checkoutDir string) string {
	if r.ActionPath == "" {
		return checkoutDir
	}
	return filepath.Join(checkoutDir, filepath.FromSlash(r.ActionPath))
}

// IsPinned reports whether the revision already looks like a Git commit SHA.
func (r *RemoteRef) IsPinned() bool {
	return isLikelyCommitSHA(r.Revision)
}

func isLikelyCommitSHA(revision string) bool {
	if len(revision) < 7 || len(revision) > 40 {
		return false
	}
	for _, ch := range revision {
		switch {
		case ch >= '0' && ch <= '9':
		case ch >= 'a' && ch <= 'f':
		case ch >= 'A' && ch <= 'F':
		default:
			return false
		}
	}
	return true
}
