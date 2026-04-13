package module

import (
	"context"
)

type RebootParams struct {
	Condition string `param:"condition" default:"if_needed"`
	Timeout   int    `param:"timeout" default:"60"`
}

type RebootModule struct{}

func (m *RebootModule) Check(_ context.Context, params map[string]any) (bool, error) {
	var p RebootParams
	if err := Decode(params, &p); err != nil {
		return false, err
	}
	if p.Condition == "always" {
		return true, nil
	}
	return rebootPending()
}

func (m *RebootModule) Apply(_ context.Context, params map[string]any) error {
	var p RebootParams
	if err := Decode(params, &p); err != nil {
		return err
	}
	return applyReboot(p.Timeout)
}
