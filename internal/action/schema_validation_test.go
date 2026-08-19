package action

import (
	"strings"
	"testing"

	schemafiles "github.com/bluecadet/preflight/schema"
)

func TestValidatePlaybookYAML_SchemaFailure(t *testing.T) {
	err := ValidatePlaybookYAML([]byte(`
name: bad-playbook
tasks:
  - shell:
      cmd: echo
`))
	if err == nil {
		t.Fatal("expected schema validation error")
	}
	if !strings.Contains(err.Error(), "schema validation failed") {
		t.Fatalf("expected schema validation failure, got %v", err)
	}
}

func TestValidateSchemas_ConditionalRequiredModuleParams(t *testing.T) {
	validators := map[string]func([]byte) error{
		"action":   ValidateActionYAML,
		"playbook": ValidatePlaybookYAML,
	}
	tests := []struct {
		name    string
		task    string
		wantErr bool
	}{
		{
			name:    "environment value defaults to required",
			task:    "environment:\n      name: PREFLIGHT_TEST",
			wantErr: true,
		},
		{
			name: "absent environment omits value",
			task: "environment:\n      name: PREFLIGHT_TEST\n      ensure: absent",
		},
		{
			name:    "powershell requires script or file",
			task:    "powershell:\n      args: [quiet]",
			wantErr: true,
		},
		{
			name: "powershell accepts script",
			task: "powershell:\n      script: Write-Output ok",
		},
		{
			name: "powershell accepts file",
			task: "powershell:\n      file: setup.ps1",
		},
		{
			name:    "present package requires source",
			task:    "package:\n      packages:\n        - product_id: product-guid",
			wantErr: true,
		},
		{
			name: "absent package omits source",
			task: "package:\n      packages:\n        - product_id: product-guid\n          ensure: absent",
		},
		{
			name:    "present scheduled task requires execute",
			task:    "scheduled_task:\n      name: cleanup",
			wantErr: true,
		},
		{
			name: "absent scheduled task omits execute",
			task: "scheduled_task:\n      name: cleanup\n      ensure: absent",
		},
		{
			name: "absent scheduled task ignores trigger fields",
			task: "scheduled_task:\n      name: cleanup\n      ensure: absent\n      trigger: once",
		},
		{
			name:    "once scheduled task requires start time",
			task:    "scheduled_task:\n      name: cleanup\n      execute: cleanup.exe\n      trigger: once",
			wantErr: true,
		},
		{
			name:    "present registry value requires data or patch",
			task:    "registry:\n      path: HKLM:\\\\SOFTWARE\\\\Preflight\n      values:\n        - name: Enabled",
			wantErr: true,
		},
		{
			name: "absent registry value omits data",
			task: "registry:\n      path: HKLM:\\\\SOFTWARE\\\\Preflight\n      values:\n        - name: Enabled\n          ensure: absent",
		},
	}

	for validatorName, validate := range validators {
		for _, tt := range tests {
			t.Run(validatorName+"/"+tt.name, func(t *testing.T) {
				document := []byte("name: conditional-required\ntasks:\n  - name: test task\n    " + tt.task + "\n")
				err := validate(document)
				if tt.wantErr && err == nil {
					t.Fatal("expected schema validation error")
				}
				if !tt.wantErr && err != nil {
					t.Fatalf("unexpected schema validation error: %v", err)
				}
			})
		}
	}
}

func TestValidatePlaybookYAML_WingetArgsAllowed(t *testing.T) {
	err := ValidatePlaybookYAML([]byte(`
name: winget-args
tasks:
  - name: Install Visual Studio
    winget_package:
      packages:
        - id: Microsoft.VisualStudio.2022.Community
          args: ["--override", "--quiet --wait --norestart"]
`))
	if err != nil {
		t.Fatalf("expected winget args to validate, got %v", err)
	}
}

func TestValidateActionYAML_WingetArgsAllowed(t *testing.T) {
	err := ValidateActionYAML([]byte(`
name: winget-args
tasks:
  - name: Install Visual Studio
    winget_package:
      packages:
        - id: Microsoft.VisualStudio.2022.Community
          args: ["--override", "--quiet --wait --norestart"]
`))
	if err != nil {
		t.Fatalf("expected winget args to validate, got %v", err)
	}
}

func TestEmbeddedSchemasAvailable(t *testing.T) {
	t.Parallel()

	cases := []string{
		"action.schema.json",
		"playbook.schema.json",
		"inventory.schema.json",
		"config.schema.json",
	}

	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			embedded, err := schemafiles.FS.ReadFile(name)
			if err != nil {
				t.Fatalf("ReadFile(%q): %v", name, err)
			}
			if len(embedded) == 0 {
				t.Fatalf("schema %q was empty", name)
			}
		})
	}
}
