//go:build windows

package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/pscript"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/winutil"
)

type FirewallRuleModule struct{}

func (m *FirewallRuleModule) Check(ctx context.Context, params map[string]any, out target.OutputFunc) (target.CheckResult, error) {
	return runValidatedWindowsCheck[FirewallRuleParams](ctx, params, out, pscript.FirewallRuleCheckScript, normalizeFirewallRuleParams)
}

func (m *FirewallRuleModule) Apply(ctx context.Context, params map[string]any, out target.OutputFunc) (target.ApplyResult, error) {
	return runValidatedWindowsApply[FirewallRuleParams](ctx, params, out, pscript.FirewallRuleApplyScript, normalizeFirewallRuleParams)
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
