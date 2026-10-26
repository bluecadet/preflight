package winutil

import (
	"fmt"
	"strings"
)

// EnvironmentUser returns the profile owner an environment task targets, or ""
// for the runner's own identity. A blank user (for example a templated optional
// input left at its default) means the runner's identity; a non-blank user
// requires scope: user.
func EnvironmentUser(params map[string]any) (string, error) {
	raw, ok := params["user"]
	if !ok || raw == nil {
		return "", nil
	}
	user, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("environment: user must be a string, got %T", raw)
	}
	user = strings.TrimSpace(user)
	if user == "" {
		return "", nil
	}
	scope, _ := params["scope"].(string)
	if !strings.EqualFold(strings.TrimSpace(scope), "user") {
		return "", fmt.Errorf("environment: user requires scope: user")
	}
	return user, nil
}
