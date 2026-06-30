package winutil

import (
	"strings"
	"testing"
)

func TestParseBool(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    bool
		wantErr string
	}{
		{name: "bool true", input: true, want: true},
		{name: "bool false", input: false, want: false},
		{name: "string true", input: " true ", want: true},
		{name: "string false", input: "false", want: false},
		{name: "numeric true", input: "1", want: true},
		{name: "numeric false", input: "0", want: false},
		{name: "yes", input: "yes", want: true},
		{name: "no", input: "no", want: false},
		{name: "bytes yes", input: []byte("YES"), want: true},
		{name: "bytes no", input: []byte(" no "), want: false},
		{name: "invalid string", input: "maybe", wantErr: "expected bool"},
		{name: "invalid type", input: 1, wantErr: "expected bool"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseBool(tt.input)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ParseBool(%#v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeRegistryParams_BoolStringValues(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  int64
	}{
		{"true", 1},
		{"True", 1},
		{"TRUE", 1},
		{"false", 0},
		{"False", 0},
		{"FALSE", 0},
	} {
		params, err := NormalizeRegistryParams(map[string]any{
			"values": []any{
				map[string]any{
					"name": "Enabled",
					"type": "dword",
					"data": tc.input,
				},
			},
		})
		if err != nil {
			t.Fatalf("input %q: unexpected error: %v", tc.input, err)
		}
		values := params["values"].([]map[string]any)
		if got := values[0]["data"]; got != tc.want {
			t.Fatalf("input %q: got data=%v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestNormalizeWingetParams_LegacyArgs(t *testing.T) {
	params, err := NormalizeWingetParams(map[string]any{
		"id":   "Microsoft.VisualStudio.2022.Community",
		"args": []any{"--override", "--quiet --wait --norestart"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	packages, ok := params["packages"].([]any)
	if !ok || len(packages) != 1 {
		t.Fatalf("expected one normalized winget package, got %#v", params["packages"])
	}
	spec, ok := packages[0].(map[string]any)
	if !ok {
		t.Fatalf("expected normalized package object, got %#v", packages[0])
	}
	args, ok := spec["args"].([]any)
	if !ok || len(args) != 2 {
		t.Fatalf("expected normalized args, got %#v", spec["args"])
	}
	if args[0] != "--override" || args[1] != "--quiet --wait --norestart" {
		t.Fatalf("unexpected normalized args: %#v", args)
	}
}

func TestNormalizeRegistryParams_TypedListAndLegacyMap(t *testing.T) {
	legacy, err := NormalizeRegistryParams(map[string]any{
		"values": map[string]any{
			"ToastEnabled": false,
		},
	})
	if err != nil {
		t.Fatalf("unexpected legacy-map error: %v", err)
	}
	legacyValues, ok := legacy["values"].([]map[string]any)
	if !ok || len(legacyValues) != 1 {
		t.Fatalf("expected normalized legacy values, got %#v", legacy["values"])
	}
	if legacyValues[0]["type"] != "dword" || legacyValues[0]["data"] != int64(0) {
		t.Fatalf("unexpected normalized legacy value: %#v", legacyValues[0])
	}

	typed, err := NormalizeRegistryParams(map[string]any{
		"values": []any{
			map[string]any{
				"name": "LongPathsEnabled",
				"type": "dword",
				"data": "1",
			},
			map[string]any{
				"name":   "StaleValue",
				"ensure": "absent",
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected typed-list error: %v", err)
	}
	typedValues, ok := typed["values"].([]map[string]any)
	if !ok || len(typedValues) != 2 {
		t.Fatalf("expected normalized typed values, got %#v", typed["values"])
	}
	if typedValues[0]["type"] != "dword" || typedValues[0]["data"] != int64(1) {
		t.Fatalf("unexpected typed value[0]: %#v", typedValues[0])
	}
	if typedValues[1]["ensure"] != "absent" {
		t.Fatalf("unexpected typed value[1]: %#v", typedValues[1])
	}
}

func TestNormalizeRegistryParams_BinaryPatch(t *testing.T) {
	params, err := NormalizeRegistryParams(map[string]any{
		"values": []any{
			map[string]any{
				"name": "Settings",
				"type": "binary",
				"patch": []any{
					map[string]any{
						"offset": 8,
						"data":   "3",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	values := params["values"].([]map[string]any)
	if values[0]["type"] != "binary" {
		t.Fatalf("expected binary type, got %#v", values[0]["type"])
	}
	patch := values[0]["patch"].([]map[string]any)
	if patch[0]["offset"] != int64(8) || patch[0]["data"] != int64(3) {
		t.Fatalf("unexpected normalized patch: %#v", patch[0])
	}
}

func TestNormalizeRegistryParams_BinaryPatchValidation(t *testing.T) {
	_, err := NormalizeRegistryParams(map[string]any{
		"values": []any{
			map[string]any{
				"name":  "Settings",
				"type":  "dword",
				"patch": []any{map[string]any{"offset": 8, "data": 3}},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "patch is only supported for binary values") {
		t.Fatalf("expected binary-only patch error, got %v", err)
	}

	_, err = NormalizeRegistryParams(map[string]any{
		"values": []any{
			map[string]any{
				"name":  "Settings",
				"type":  "binary",
				"data":  []any{1, 2, 3},
				"patch": []any{map[string]any{"offset": 8, "data": 3}},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "data cannot be combined with patch") {
		t.Fatalf("expected data plus patch error, got %v", err)
	}
}

func TestNormalizeRegistryProviderPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "short HKLM no colon", in: `HKLM\SOFTWARE\PreflightTest\Run`, want: `Registry::HKEY_LOCAL_MACHINE\SOFTWARE\PreflightTest\Run`},
		{name: "short HKLM with colon", in: `HKLM:\SOFTWARE\App`, want: `Registry::HKEY_LOCAL_MACHINE\SOFTWARE\App`},
		{name: "long hive no provider", in: `HKEY_LOCAL_MACHINE\SOFTWARE\App`, want: `Registry::HKEY_LOCAL_MACHINE\SOFTWARE\App`},
		{name: "HKCU with colon", in: `HKCU:\Software\Example`, want: `Registry::HKEY_CURRENT_USER\Software\Example`},
		{name: "HKU short", in: `HKU:\S-1-5-21\Software`, want: `Registry::HKEY_USERS\S-1-5-21\Software`},
		{name: "HKCR short", in: `HKCR\.txt`, want: `Registry::HKEY_CLASSES_ROOT\.txt`},
		{name: "HKCC short", in: `HKCC`, want: `Registry::HKEY_CURRENT_CONFIG`},
		{name: "hive only no subpath", in: `HKLM`, want: `Registry::HKEY_LOCAL_MACHINE`},
		{name: "already provider qualified", in: `Registry::HKEY_LOCAL_MACHINE\SOFTWARE\App`, want: `Registry::HKEY_LOCAL_MACHINE\SOFTWARE\App`},
		{name: "case-insensitive prefix", in: `hklm:\Software\App`, want: `Registry::HKEY_LOCAL_MACHINE\Software\App`},
		{name: "unknown prefix untouched", in: `C:\Temp\file`, want: `C:\Temp\file`},
		{name: "empty untouched", in: ``, want: ``},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeRegistryProviderPath(tc.in); got != tc.want {
				t.Fatalf("normalizeRegistryProviderPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeRegistryParams_NormalizesPath(t *testing.T) {
	params, err := NormalizeRegistryParams(map[string]any{
		"path": `HKLM\SOFTWARE\PreflightTest\Run`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if params["path"] != `Registry::HKEY_LOCAL_MACHINE\SOFTWARE\PreflightTest\Run` {
		t.Fatalf("expected provider-qualified path, got %#v", params["path"])
	}
}

func TestNormalizeRegistryParams_PathMustBeString(t *testing.T) {
	_, err := NormalizeRegistryParams(map[string]any{
		"path": 123,
	})
	if err == nil || !strings.Contains(err.Error(), "registry path must be a string") {
		t.Fatalf("expected path type error, got %v", err)
	}
}

func TestNormalizeRegistryParams_TrimsUser(t *testing.T) {
	params, err := NormalizeRegistryParams(map[string]any{
		"path": `HKCU:\Software\Example`,
		"user": " kiosk ",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if params["user"] != "kiosk" {
		t.Fatalf("expected trimmed user, got %#v", params["user"])
	}
}

func TestNormalizeRegistryParams_UserMustBeString(t *testing.T) {
	_, err := NormalizeRegistryParams(map[string]any{
		"path": `HKCU:\Software\Example`,
		"user": 123,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "registry user must be a string") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeScheduledTaskParams_AliasesAndDefaults(t *testing.T) {
	params, err := NormalizeScheduledTaskParams(map[string]any{
		"name":     "Preflight Reboot",
		"path":     "Preflight",
		"command":  `C:\Windows\System32\shutdown.exe`,
		"user":     "SYSTEM",
		"trigger":  "daily",
		"enabled":  "false",
		"delay":    "30s",
		"start_at": "04:30",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if params["execute"] != `C:\Windows\System32\shutdown.exe` {
		t.Fatalf("expected command alias to populate execute, got %#v", params["execute"])
	}
	if params["run_as"] != "SYSTEM" {
		t.Fatalf("expected user alias to populate run_as, got %#v", params["run_as"])
	}
	if params["path"] != `\Preflight\` {
		t.Fatalf("expected normalized task path, got %#v", params["path"])
	}
	if params["enabled"] != false {
		t.Fatalf("expected normalized enabled=false, got %#v", params["enabled"])
	}
	if params["delay"] != "PT30S" {
		t.Fatalf("expected normalized delay PT30S, got %#v", params["delay"])
	}
}

func TestValidateScheduledTaskParams_TriggerRules(t *testing.T) {
	if err := ValidateScheduledTaskParams(map[string]any{
		"ensure":  "present",
		"execute": `C:\Windows\System32\shutdown.exe`,
		"trigger": "once",
	}); err == nil {
		t.Fatal("expected once trigger without start_at to fail")
	}

	if err := ValidateScheduledTaskParams(map[string]any{
		"ensure":  "present",
		"execute": `C:\Windows\System32\shutdown.exe`,
		"trigger": "daily",
		"delay":   "PT30S",
	}); err == nil {
		t.Fatal("expected daily trigger with delay to fail")
	}

	if err := ValidateScheduledTaskParams(map[string]any{
		"ensure":   "present",
		"execute":  `C:\Windows\System32\shutdown.exe`,
		"trigger":  "once",
		"start_at": "2026-04-01T04:30:00",
	}); err != nil {
		t.Fatalf("expected valid once trigger, got %v", err)
	}
}

func TestNormalizeFirewallPorts(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    string
		wantErr string
	}{
		{name: "nil", input: nil, want: ""},
		{name: "int", input: 80, want: "80"},
		{name: "string", input: "443", want: "443"},
		{name: "list", input: []any{80, "443", 8080}, want: "80,443,8080"},
		{name: "list with nil", input: []any{80, nil}, wantErr: "ports[1] must not be null"},
		{name: "invalid", input: true, wantErr: "ports must be a string, number, or list"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeFirewallPorts(tt.input)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeFirewallPorts(%#v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
