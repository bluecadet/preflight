//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/pscript"
	"github.com/bluecadet/preflight/internal/winutil"
)

type FirewallRuleModule struct{}

func (m *FirewallRuleModule) Check(ctx context.Context, params map[string]any) (bool, error) {
	return runValidatedWindowsCheck[FirewallRuleParams](ctx, params, pscript.FirewallRuleCheckScript, normalizeFirewallRuleParams)
}

func (m *FirewallRuleModule) Apply(ctx context.Context, params map[string]any) error {
	return runValidatedWindowsApply[FirewallRuleParams](ctx, params, pscript.FirewallRuleApplyScript, normalizeFirewallRuleParams)
}

func normalizeFirewallRuleParams(params map[string]any) (map[string]any, error) {
	ports, err := firewallPortsArg(params)
	if err != nil {
		return nil, err
	}
	normalized := winutil.CloneParams(params)
	normalized["ports"] = ports
	return normalized, nil
}
