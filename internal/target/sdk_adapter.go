package target

import (
	"context"

	"github.com/bluecadet/preflight/pkg/plugin/sdk"
)

// sdkModuleAdapter adapts an in-process target.Module to the sdk.Module
// interface so a built-in module can be served over the plugin JSON-RPC
// protocol (used by the local become subprocess path). Built-in modules operate
// directly on the target and do not use the handle's target ops; only Output is
// forwarded so streaming still works.
type sdkModuleAdapter struct {
	name string
	mod  Module
}

func NewSDKModuleAdapter(name string, mod Module) sdk.Module {
	return &sdkModuleAdapter{name: name, mod: mod}
}

func (a *sdkModuleAdapter) Name() string    { return a.name }
func (a *sdkModuleAdapter) Version() string { return "" }

func (a *sdkModuleAdapter) Check(args map[string]any, h sdk.Handle) (sdk.CheckResult, error) {
	var out OutputFunc
	if h != nil {
		out = OutputFunc(h.Output)
	}
	res, err := a.mod.Check(context.TODO(), args, out)
	return sdk.CheckResult{
		NeedsChange: res.NeedsChange,
		Message:     res.Message,
	}, err
}

func (a *sdkModuleAdapter) Apply(args map[string]any, h sdk.Handle) (sdk.ApplyResult, error) {
	var out OutputFunc
	if h != nil {
		out = OutputFunc(h.Output)
	}
	res, err := a.mod.Apply(context.TODO(), args, out)
	return sdk.ApplyResult{
		Message: res.Message,
	}, err
}
