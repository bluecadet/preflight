package targettest

import (
	"context"
	"maps"

	"github.com/bluecadet/preflight/internal/target"
)

// Call records one Target.Execute invocation. Tests use it to verify the
// runner-to-target contract without coupling to a concrete transport.
type Call struct {
	TaskID  string
	Module  string
	Params  map[string]any
	Options target.ExecutionOptions
	DryRun  bool
}

// Fake is a scriptable target.Target for package tests that need to exercise
// runner or fact-gathering behavior without choosing local, SSH, or WinRM.
type Fake struct {
	InfoValue target.TargetInfo
	InfoErr   error

	Results []target.Result
	ExecErr error
	Output  []string

	PowerShellOut string
	PowerShellErr error

	Calls      []Call
	CloseCalls int
}

func (f *Fake) Execute(_ context.Context, taskID, module string, params map[string]any, opts target.ExecutionOptions, dryRun bool, onOutput target.OutputFunc) (target.Result, error) {
	f.Calls = append(f.Calls, Call{
		TaskID:  taskID,
		Module:  module,
		Params:  cloneMap(params),
		Options: cloneExecutionOptions(opts),
		DryRun:  dryRun,
	})
	if onOutput != nil {
		for _, line := range f.Output {
			onOutput(line)
		}
	}
	if f.ExecErr != nil {
		return target.Result{TaskID: taskID, Output: append([]string(nil), f.Output...)}, f.ExecErr
	}
	if len(f.Results) == 0 {
		return target.Result{TaskID: taskID, Status: target.StatusOK, Output: append([]string(nil), f.Output...)}, nil
	}
	idx := len(f.Calls) - 1
	if idx >= len(f.Results) {
		idx = len(f.Results) - 1
	}
	result := f.Results[idx]
	result.TaskID = taskID
	if len(f.Output) > 0 && len(result.Output) == 0 {
		result.Output = append([]string(nil), f.Output...)
	}
	return result, nil
}

func (f *Fake) Info(_ context.Context) (target.TargetInfo, error) {
	return f.InfoValue, f.InfoErr
}

func (f *Fake) Transport() target.Transport {
	if f.InfoValue.Transport != "" {
		return f.InfoValue.Transport
	}
	return target.TransportSSH
}

func (f *Fake) RunPowerShell(_ context.Context, _ string) (string, error) {
	return f.PowerShellOut, f.PowerShellErr
}

func (f *Fake) Close() error {
	f.CloseCalls++
	return nil
}

func cloneMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	maps.Copy(dst, src)
	return dst
}

func cloneExecutionOptions(src target.ExecutionOptions) target.ExecutionOptions {
	if src.Become == nil {
		return target.ExecutionOptions{}
	}
	become := *src.Become
	if src.Become.LoadProfile != nil {
		loadProfile := *src.Become.LoadProfile
		become.LoadProfile = &loadProfile
	}
	return target.ExecutionOptions{Become: &become}
}
