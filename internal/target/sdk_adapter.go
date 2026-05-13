package target

import (
	"context"

	"github.com/bluecadet/preflight/pkg/plugin/sdk"
)

type sdkModuleAdapter struct {
	name string
	mod  Module
}

func NewSDKModuleAdapter(name string, mod Module) sdk.StreamingModule {
	return &sdkModuleAdapter{name: name, mod: mod}
}

func (a *sdkModuleAdapter) Name() string    { return a.name }
func (a *sdkModuleAdapter) Version() string { return "" }

func (a *sdkModuleAdapter) Check(args map[string]any) (sdk.CheckResult, error) {
	return a.CheckStreaming(args, nil)
}

func (a *sdkModuleAdapter) CheckStreaming(args map[string]any, out sdk.OutputFunc) (sdk.CheckResult, error) {
	var targetOut OutputFunc
	if out != nil {
		targetOut = OutputFunc(out)
	}
	res, err := a.mod.Check(context.TODO(), args, targetOut)
	return sdk.CheckResult{
		NeedsChange: res.NeedsChange,
		Message:     res.Message,
	}, err
}

func (a *sdkModuleAdapter) Apply(args map[string]any) (sdk.ApplyResult, error) {
	return a.ApplyStreaming(args, nil)
}

func (a *sdkModuleAdapter) ApplyStreaming(args map[string]any, out sdk.OutputFunc) (sdk.ApplyResult, error) {
	var targetOut OutputFunc
	if out != nil {
		targetOut = OutputFunc(out)
	}
	res, err := a.mod.Apply(context.TODO(), args, targetOut)
	return sdk.ApplyResult{
		Message: res.Message,
	}, err
}
