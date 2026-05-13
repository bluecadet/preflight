package module

import (
	"context"

	"github.com/bluecadet/preflight/internal/target"
)

type RebootParams struct {
	Condition string `param:"condition" default:"if_needed"`
	Timeout   int    `param:"timeout" default:"60"`
}

type RebootModule struct{}

func (m *RebootModule) Check(_ context.Context, params map[string]any, _ target.OutputFunc) (target.CheckResult, error) {
	var p RebootParams
	if err := Decode(params, &p); err != nil {
		return target.CheckResult{}, err
	}
	if p.Condition == "always" {
		return target.CheckResult{NeedsChange: true}, nil
	}
	needed, err := rebootPending()
	return target.CheckResult{NeedsChange: needed}, err
}

func (m *RebootModule) Apply(_ context.Context, params map[string]any, _ target.OutputFunc) (target.ApplyResult, error) {
	var p RebootParams
	if err := Decode(params, &p); err != nil {
		return target.ApplyResult{}, err
	}
	return target.ApplyResult{}, applyReboot(p.Timeout)
}
