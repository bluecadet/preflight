package module

import "fmt"

// EnsureCheck dispatches to presentCheck or absentCheck based on the ensure value.
func EnsureCheck(module, ensure string, presentCheck, absentCheck func() (bool, error)) (bool, error) {
	switch ensure {
	case "present", "":
		return presentCheck()
	case "absent":
		return absentCheck()
	default:
		return false, fmt.Errorf("%s: unknown ensure value %q (want present|absent)", module, ensure)
	}
}

// EnsureApply dispatches to presentApply or absentApply based on the ensure value.
func EnsureApply(module, ensure string, presentApply, absentApply func() error) error {
	switch ensure {
	case "present", "":
		return presentApply()
	case "absent":
		return absentApply()
	default:
		return fmt.Errorf("%s: unknown ensure value %q (want present|absent)", module, ensure)
	}
}
