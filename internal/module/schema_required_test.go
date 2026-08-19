package module_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/bluecadet/preflight/internal/module"
	schemafiles "github.com/bluecadet/preflight/schema"
)

type moduleParamContract struct {
	types    []reflect.Type
	required []string
}

func TestModuleSchemasMatchRuntimeRequiredParams(t *testing.T) {
	t.Parallel()

	contracts := map[string]moduleParamContract{
		"moduleDirectory":          {types: []reflect.Type{reflect.TypeFor[module.DirectoryParams]()}},
		"moduleEnvironment":        {types: []reflect.Type{reflect.TypeFor[module.EnvironmentParams]()}},
		"moduleFile":               {types: []reflect.Type{reflect.TypeFor[module.FileParams]()}},
		"moduleFirewallRule":       {types: []reflect.Type{reflect.TypeFor[module.FirewallRuleParams]()}},
		"modulePackage":            {required: []string{"packages"}},
		"modulePowerPlan":          {types: []reflect.Type{reflect.TypeFor[module.PowerPlanParams]()}},
		"modulePowershell":         {types: []reflect.Type{reflect.TypeFor[module.PowershellCheckParams](), reflect.TypeFor[module.PowershellApplyParams]()}},
		"moduleReboot":             {types: []reflect.Type{reflect.TypeFor[module.RebootParams]()}},
		"moduleRegistry":           {types: []reflect.Type{reflect.TypeFor[module.RegistryParams]()}},
		"moduleRemoveAppxPackages": {required: []string{"packages"}},
		"moduleScheduledTask":      {types: []reflect.Type{reflect.TypeFor[module.ScheduledTaskParams]()}},
		"moduleService":            {types: []reflect.Type{reflect.TypeFor[module.ServiceParams]()}},
		"moduleShell":              {types: []reflect.Type{reflect.TypeFor[module.ShellCheckParams](), reflect.TypeFor[module.ShellApplyParams]()}},
		"moduleShortcut":           {types: []reflect.Type{reflect.TypeFor[module.ShortcutParams]()}},
		"moduleSystemPackage":      {required: []string{"packages"}},
		"moduleUser":               {types: []reflect.Type{reflect.TypeFor[module.UserParams]()}},
		"moduleWait":               {types: []reflect.Type{reflect.TypeFor[module.WaitParams]()}},
		"moduleWindowsFeature":     {types: []reflect.Type{reflect.TypeFor[module.WindowsFeatureParams]()}},
		"moduleWingetPackage":      {required: []string{"packages"}},
	}

	for _, schemaName := range []string{"action.schema.json", "playbook.schema.json"} {
		t.Run(schemaName, func(t *testing.T) {
			data, err := schemafiles.FS.ReadFile(schemaName)
			if err != nil {
				t.Fatalf("ReadFile(%q): %v", schemaName, err)
			}

			var document struct {
				Defs map[string]struct {
					Required []string `json:"required"`
				} `json:"$defs"`
			}
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatalf("Unmarshal(%q): %v", schemaName, err)
			}

			for definition := range document.Defs {
				if !strings.HasPrefix(definition, "module") {
					continue
				}
				if _, ok := contracts[definition]; !ok {
					t.Errorf("schema definition %q has no runtime parameter contract", definition)
				}
			}

			for definition, contract := range contracts {
				schemaDefinition, ok := document.Defs[definition]
				if !ok {
					t.Errorf("runtime parameter contract %q has no schema definition", definition)
					continue
				}

				got := slices.Clone(schemaDefinition.Required)
				want := requiredParamNames(contract)
				slices.Sort(got)
				slices.Sort(want)
				if !slices.Equal(got, want) {
					t.Errorf("%s required params = %v, want %v", definition, got, want)
				}
			}
		})
	}
}

func requiredParamNames(contract moduleParamContract) []string {
	required := append([]string(nil), contract.required...)
	for _, paramType := range contract.types {
		for i := range paramType.NumField() {
			parts := strings.Split(paramType.Field(i).Tag.Get("param"), ",")
			if len(parts) > 1 && parts[1] == "required" && !slices.Contains(required, parts[0]) {
				required = append(required, parts[0])
			}
		}
	}
	return required
}
