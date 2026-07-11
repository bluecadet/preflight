package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestTextRenderer_SupportGateEvent(t *testing.T) {
	var buf bytes.Buffer
	r := newTextRenderer(&buf)
	r.Emit(SupportGateEvent{
		Target:  "posix-host",
		Runtime: "posix-shell",
		Reason:  "unsupported_on_runtime",
		Violations: []SupportGateViolation{
			{TaskName: "install tools", Module: "system_package", Reason: "unsupported_on_runtime", Message: `module "system_package" is not supported on posix-shell (supported: windows-powershell)`},
			{TaskName: "manage svc", Module: "service", Reason: "unsupported_on_runtime", Message: `module "service" is not supported on posix-shell (supported: windows-powershell)`},
		},
	})
	out := buf.String()

	// Summary line naming the runtime and the count.
	if !strings.Contains(out, "support gate: 2 task(s) cannot run on posix-host (posix-shell)") {
		t.Errorf("missing summary line:\n%s", out)
	}
	// One line per violation, naming the task and module.
	if !strings.Contains(out, `install tools: module "system_package" is not supported on posix-shell`) {
		t.Errorf("missing first violation line:\n%s", out)
	}
	if !strings.Contains(out, `manage svc: module "service" is not supported on posix-shell`) {
		t.Errorf("missing second violation line:\n%s", out)
	}
}
