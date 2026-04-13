package module

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/bluecadet/preflight/internal/preflighterr"
	"github.com/mitchellh/mapstructure"
)

// Decode maps a raw params map into a typed struct using `param` tags.
// Tags: `param:"key,required"`, defaults via `default:"value"` struct tag.
func Decode[T any](params map[string]any, dest *T) error {
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:           dest,
		TagName:          "param",
		WeaklyTypedInput: true,
	})
	if err != nil {
		return &preflighterr.ModuleError{Module: "params", Op: "init param decoder", Err: err}
	}
	if err := decoder.Decode(params); err != nil {
		return &preflighterr.ModuleError{Module: "params", Op: "decode params", Err: err}
	}
	return validateStruct(dest)
}

func validateStruct[T any](dest *T) error {
	v := reflect.ValueOf(dest).Elem()
	t := v.Type()

	for i := range t.NumField() {
		field := t.Field(i)
		fv := v.Field(i)

		tag := field.Tag.Get("param")
		if tag == "" || tag == "-" {
			continue
		}

		parts := strings.Split(tag, ",")
		key := parts[0]
		required := len(parts) > 1 && parts[1] == "required"

		if required && fv.IsZero() {
			return &preflighterr.ValidationError{Field: key, Message: "required param is missing"}
		}

		if fv.IsZero() {
			if def, ok := field.Tag.Lookup("default"); ok {
				if err := setDefault(fv, def); err != nil {
					return &preflighterr.ValidationError{Field: key, Message: fmt.Sprintf("default value is invalid: %v", err), Err: err}
				}
			}
		}
	}
	return nil
}

func setDefault(fv reflect.Value, def string) error {
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(def)
	case reflect.Int, reflect.Int64:
		var n int64
		if _, err := fmt.Sscanf(def, "%d", &n); err != nil {
			return fmt.Errorf("invalid int default %q: %w", def, err)
		}
		fv.SetInt(n)
	case reflect.Bool:
		fv.SetBool(def == "true")
	default:
		return fmt.Errorf("unsupported default type %s", fv.Kind())
	}
	return nil
}

func RejectParams(module string, params map[string]any, keys ...string) error {
	for _, k := range keys {
		if _, ok := params[k]; ok {
			return &preflighterr.ModuleError{Module: module, Err: fmt.Errorf("%s is not supported", k)}
		}
	}
	return nil
}
