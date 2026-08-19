// Command preflight-plugin-testhandle is a test plugin used by
// internal/plugins to exercise the protocol handle API (RunCommand, PutFile,
// GetFile, Info, Output) and cancellation against the local target
// end-to-end.
package main

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/bluecadet/preflight/pkg/plugin/sdk"
)

// The plugin reads its actions from params so a single binary can drive
// multiple scenarios (ops round-trip, streaming, become-refusal is exercised
// host-side).
type handleModule struct{}

func (handleModule) Name() string    { return "testhandle" }
func (handleModule) Version() string { return "0.0.1" }

func (handleModule) Check(ctx context.Context, args map[string]any, h sdk.Handle) (sdk.CheckResult, error) {
	scenario, _ := args["scenario"].(string)
	switch scenario {
	case "ops":
		return runOpsScenario(ctx, args, h)
	case "streaming":
		return runStreamingScenario(args, h)
	case "cancel":
		return runCancelScenario(ctx, args)
	default:
		return sdk.CheckResult{NeedsChange: true, Message: "no-op"}, nil
	}
}

func (handleModule) Apply(_ context.Context, _ map[string]any, _ sdk.Handle) (sdk.ApplyResult, error) {
	return sdk.ApplyResult{Message: "applied"}, nil
}

// runOpsScenario drives RunCommand, PutFile, GetFile, and Info, validating
// each round trip and returning a CheckResult whose Message encodes the
// outcome so the host test can assert on it.
func runOpsScenario(ctx context.Context, args map[string]any, h sdk.Handle) (sdk.CheckResult, error) {
	// RunCommand: echo a marker the host can recognise.
	cmd, err := h.RunCommand(ctx, "printf pf-runcommand-ok")
	if err != nil {
		return sdk.CheckResult{}, err
	}
	if cmd.Stdout != "pf-runcommand-ok" {
		return sdk.CheckResult{Message: "runcommand stdout mismatch: " + strconv.Quote(cmd.Stdout)}, nil
	}
	if cmd.ExitCode != 0 {
		return sdk.CheckResult{Message: "runcommand exit: " + strconv.Itoa(cmd.ExitCode)}, nil
	}

	// PutFile: write bytes the host will read back.
	putPath, _ := args["put_path"].(string)
	if putPath == "" {
		putPath = "/tmp/pf-plugin-put"
	}
	if err := h.PutFile(ctx, putPath, []byte("plugin-put-bytes")); err != nil {
		return sdk.CheckResult{}, err
	}

	// GetFile: read it back and verify.
	got, err := h.GetFile(ctx, putPath)
	if err != nil {
		return sdk.CheckResult{}, err
	}
	if string(got) != "plugin-put-bytes" {
		return sdk.CheckResult{Message: "getfile mismatch: " + strconv.Quote(string(got))}, nil
	}

	// Info: the host delivered TargetInfo at initialize.
	info := h.Info()
	if info.RuntimeKind == "" {
		return sdk.CheckResult{Message: "info runtime_kind empty"}, nil
	}
	return sdk.CheckResult{NeedsChange: true, Message: "ops-ok:" + info.RuntimeKind + ":" + info.Family}, nil
}

// runCancelScenario blocks until the context handed to Check is cancelled,
// then writes a marker file on the controller filesystem before returning. The
// host test asserts that the marker appears, which is only possible if the
// host's cancellation actually crossed the process boundary and fired this
// context — a plugin that merely got killed would never write it.
func runCancelScenario(ctx context.Context, args map[string]any) (sdk.CheckResult, error) {
	markerPath, _ := args["marker_path"].(string)
	// Announce that Check is running, so the host test cancels at a point
	// where cancellation has somewhere to land.
	if startPath, _ := args["start_path"].(string); startPath != "" {
		if err := os.WriteFile(startPath, []byte("started"), 0o600); err != nil {
			return sdk.CheckResult{}, err
		}
	}
	select {
	case <-ctx.Done():
	case <-time.After(30 * time.Second):
		return sdk.CheckResult{Message: "context never cancelled"}, nil
	}
	if markerPath != "" {
		_ = os.WriteFile(markerPath, []byte(ctx.Err().Error()), 0o600)
	}
	return sdk.CheckResult{}, ctx.Err()
}

func runStreamingScenario(_ map[string]any, h sdk.Handle) (sdk.CheckResult, error) {
	h.Output("stream-1")
	h.Output("stream-2")
	return sdk.CheckResult{NeedsChange: true, Message: "streamed"}, nil
}

func main() {
	// Silence stderr noise in tests.
	_ = os.Stderr
	sdk.Serve(handleModule{}, sdk.ServeOptions{})
}
