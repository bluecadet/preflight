package target

import (
	"context"
	"fmt"
	"strings"
)

// checkPOSIXUser checks the desired state of a local POSIX user.
//
// For ensure:absent, the user existing is a change. For ensure:present, a
// missing user is a change; an existing user is a change only when any
// desired supplementary group is not yet held. Password state is never
// probed — password drift on existing users is a stated limitation (see
// applyPOSIXUser and docs/reference/modules.md).
func checkPOSIXUser(ctx context.Context, backend posixShellBackend, params map[string]any) (CheckResult, error) {
	name, err := requireStringParam(params, "name", "user")
	if err != nil {
		return CheckResult{}, err
	}
	ensure, _ := params["ensure"].(string)
	if ensure == "" {
		ensure = "present"
	}
	groups, err := paramStringSlice(params, "groups")
	if err != nil {
		return CheckResult{}, err
	}

	exists, err := posixUserExists(ctx, backend, name)
	if err != nil {
		return CheckResult{}, err
	}

	switch ensure {
	case "absent":
		if exists {
			return CheckResult{NeedsChange: true, Message: "user exists, will remove"}, nil
		}
		return CheckResult{}, nil
	case "present":
		if !exists {
			return CheckResult{NeedsChange: true, Message: "user does not exist, will create"}, nil
		}
		if len(groups) == 0 {
			return CheckResult{}, nil
		}
		missing, err := posixMissingGroups(ctx, backend, name, groups)
		if err != nil {
			return CheckResult{}, err
		}
		if len(missing) > 0 {
			return CheckResult{NeedsChange: true, Message: fmt.Sprintf("user is not a member of group %q", missing[0])}, nil
		}
		return CheckResult{}, nil
	default:
		return CheckResult{}, fmt.Errorf("user: unknown ensure value %q (want present|absent)", ensure)
	}
}

// applyPOSIXUser converges a local POSIX user to its desired state.
//
// ensure:absent runs userdel. ensure:present creates a missing user with
// useradd, then sets the password via chpasswd on creation only — the
// password of an existing user is never touched (stated limitation: managed
// POSIX endpoints authenticate by SSH key). Group membership is additive:
// usermod -aG appends the desired groups without removing any the user
// already holds.
func applyPOSIXUser(ctx context.Context, backend posixShellBackend, params map[string]any) error {
	name, err := requireStringParam(params, "name", "user")
	if err != nil {
		return err
	}
	ensure, _ := params["ensure"].(string)
	if ensure == "" {
		ensure = "present"
	}
	password, _ := params["password"].(string)
	groups, err := paramStringSlice(params, "groups")
	if err != nil {
		return err
	}

	switch ensure {
	case "absent":
		return posixMustRun(ctx, backend, fmt.Sprintf("userdel %q", name))
	case "present":
		exists, err := posixUserExists(ctx, backend, name)
		if err != nil {
			return err
		}
		if !exists {
			if err := posixMustRun(ctx, backend, fmt.Sprintf("useradd %q", name)); err != nil {
				return err
			}
			// Password is set on creation only; existing-user password drift
			// is documented, not corrected.
			if password != "" {
				stdin := fmt.Appendf(nil, "%s:%s\n", name, password)
				if err := posixMustRunWithStdin(ctx, backend, "chpasswd", stdin); err != nil {
					return err
				}
			}
		}
		if len(groups) > 0 {
			// -aG appends without stripping existing membership, so this is
			// idempotent when the user already holds all desired groups.
			groupList := strings.Join(groups, ",")
			if err := posixMustRun(ctx, backend, fmt.Sprintf("usermod -aG %q %q", groupList, name)); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("user: unknown ensure value %q (want present|absent)", ensure)
	}
}

// posixUserExists reports whether a named local user exists, via id(1).
func posixUserExists(ctx context.Context, backend posixShellBackend, name string) (bool, error) {
	_, _, code, err := backend.RunPOSIXCommand(ctx, fmt.Sprintf("id %q >/dev/null 2>&1", name), nil)
	if err != nil {
		return false, err
	}
	return code == 0, nil
}

// posixMissingGroups returns the subset of desired groups the user does not
// currently hold, via id -nG (which lists the primary plus supplementary
// groups, space-separated).
func posixMissingGroups(ctx context.Context, backend posixShellBackend, name string, desired []string) ([]string, error) {
	stdout, stderr, code, err := backend.RunPOSIXCommand(ctx, fmt.Sprintf("id -nG %q", name), nil)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("user: id -nG exited with code %d: %s", code, strings.TrimSpace(stderr))
	}
	held := make(map[string]struct{}, 8)
	for g := range strings.FieldsSeq(stdout) {
		held[g] = struct{}{}
	}
	var missing []string
	for _, g := range desired {
		if _, ok := held[g]; !ok {
			missing = append(missing, g)
		}
	}
	return missing, nil
}
