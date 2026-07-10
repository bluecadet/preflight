package target

import (
	"errors"
	"strings"
	"testing"
)

func TestModuleSupportError_EachClassMessageShape(t *testing.T) {
	cases := []struct {
		name string
		err  *ModuleSupportError
		// mustContain are substrings the uniform wording must include.
		mustContain []string
		class       ModuleSupportClass
	}{
		{
			name:        "unknown_module",
			err:         NewUnknownModuleError("nope"),
			mustContain: []string{`module "nope" is not a known module`},
			class:       ClassUnknownModule,
		},
		{
			name: "unsupported_on_runtime",
			err:  NewUnsupportedOnRuntimeError("registry", RuntimeKindPOSIXShell),
			mustContain: []string{
				`module "registry" is not supported on posix-shell`,
				"supported: windows-powershell",
			},
			class: ClassUnsupportedOnRuntime,
		},
		{
			name: "missing_prerequisite",
			err:  NewMissingPrerequisiteError("powershell", RuntimeKindPOSIXShell, "pwsh or powershell binary not found"),
			mustContain: []string{
				`module "powershell" is missing a prerequisite on posix-shell`,
				"pwsh or powershell binary not found",
			},
			class: ClassMissingPrerequisite,
		},
		{
			name:        "plugin_become",
			err:         NewPluginBecomeError("custom"),
			mustContain: []string{`plugin module "custom" does not support become`},
			class:       ClassPluginBecome,
		},
		{
			name:        "plugin_protocol",
			err:         NewPluginProtocolError("custom", "protocol version 0 not supported"),
			mustContain: []string{`plugin module "custom"`, "protocol version 0 not supported"},
			class:       ClassPluginProtocol,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.err.Error()
			for _, want := range tc.mustContain {
				if !strings.Contains(msg, want) {
					t.Errorf("error %q missing %q", msg, want)
				}
			}
			if tc.err.Class != tc.class {
				t.Errorf("class = %q, want %q", tc.err.Class, tc.class)
			}
			if tc.err.ReasonCode() != string(tc.class) {
				t.Errorf("ReasonCode = %q, want %q", tc.err.ReasonCode(), string(tc.class))
			}
		})
	}
}

func TestModuleSupportError_NoRemediationProse(t *testing.T) {
	// The spec bans did-you-mean and remediation prose.
	err := NewUnsupportedOnRuntimeError("registry", RuntimeKindPOSIXShell)
	msg := err.Error()
	for _, banned := range []string{"did you mean", "use ", "instead", "consider", "try "} {
		if strings.Contains(strings.ToLower(msg), banned) {
			t.Errorf("error contains remediation prose %q: %s", banned, msg)
		}
	}
}

func TestReasonCodeForError_ExtractsFromChain(t *testing.T) {
	mse := NewUnsupportedOnRuntimeError("registry", RuntimeKindPOSIXShell)
	wrapped := errors.Join(errors.New("context"), mse)
	if got := ReasonCodeForError(wrapped); got != string(ClassUnsupportedOnRuntime) {
		t.Errorf("ReasonCodeForError = %q, want %q", got, string(ClassUnsupportedOnRuntime))
	}
}

func TestReasonCodeForError_UntypedReturnsEmpty(t *testing.T) {
	if got := ReasonCodeForError(errors.New("plain")); got != "" {
		t.Errorf("ReasonCodeForError = %q, want empty", got)
	}
}
