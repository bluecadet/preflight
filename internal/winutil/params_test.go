package winutil

import (
	"strings"
	"testing"
)

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
