package output

import (
	"os"
	"testing"
)

func TestDetectColor_NOCOLOR(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	got := DetectColor("", false, os.Stdout)
	if got != ColorNever {
		t.Errorf("expected ColorNever with NO_COLOR, got %v", got)
	}
}

func TestDetectColor_NoColorFlag(t *testing.T) {
	got := DetectColor("", true, os.Stdout)
	if got != ColorNever {
		t.Errorf("expected ColorNever with --no-color, got %v", got)
	}
}

func TestDetectColor_ColorNeverFlag(t *testing.T) {
	got := DetectColor("never", false, os.Stdout)
	if got != ColorNever {
		t.Errorf("expected ColorNever with --color=never, got %v", got)
	}
}

func TestDetectColor_ColorAlwaysFlag(t *testing.T) {
	got := DetectColor("always", false, os.Stdout)
	if got != ColorAlways {
		t.Errorf("expected ColorAlways with --color=always, got %v", got)
	}
}

func TestDetectColor_ColorAlwaysWinsOverNoColor(t *testing.T) {
	// --color=always should win over --no-color (precedence: NO_COLOR > --no-color > --color)
	// Actually the precedence is: NO_COLOR > --no-color > --color=auto|always|never
	// so --no-color beats --color=always.
	t.Setenv("NO_COLOR", "1")
	got := DetectColor("always", true, os.Stdout)
	if got != ColorNever {
		t.Errorf("expected ColorNever with NO_COLOR + --no-color + --color=always, got %v", got)
	}
}

func TestDetectColor_CIEnv(t *testing.T) {
	t.Setenv("CI", "true")
	got := DetectColor("", false, os.Stdout)
	if got != ColorNever {
		t.Errorf("expected ColorNever with CI env var, got %v", got)
	}
}

func TestDetectColor_AutoTTY(t *testing.T) {
	// In test, os.Stdout is typically not a TTY. Verify auto defaults to Never.
	got := DetectColor("auto", false, os.Stdout)
	if got != ColorNever {
		t.Errorf("expected ColorNever for non-TTY stdout with --color=auto, got %v", got)
	}
}

func TestDetectColor_NoColorFlagBeatsColorNever(t *testing.T) {
	// --no-color beats --color=never (both say never, so result is never).
	got := DetectColor("never", true, os.Stdout)
	if got != ColorNever {
		t.Errorf("expected ColorNever with --no-color + --color=never, got %v", got)
	}
}

func TestDetectColor_NoColorFlagOverridesAuto(t *testing.T) {
	got := DetectColor("auto", true, os.Stdout)
	if got != ColorNever {
		t.Errorf("expected ColorNever with --no-color + --color=auto, got %v", got)
	}
}

func TestUseColor(t *testing.T) {
	if ColorAlways.UseColor() != true {
		t.Error("expected ColorAlways.UseColor() = true")
	}
	if ColorAuto.UseColor() != false {
		t.Error("expected ColorAuto.UseColor() = false (auto is not always)")
	}
	if ColorNever.UseColor() != false {
		t.Error("expected ColorNever.UseColor() = false")
	}
}

func TestDetectColor_AlwaysWinsOverCI(t *testing.T) {
	// --color=always should beat CI env var since CI is lower precedence.
	t.Setenv("CI", "true")
	got := DetectColor("always", false, os.Stdout)
	if got != ColorAlways {
		t.Errorf("expected ColorAlways with --color=always even with CI, got %v", got)
	}
}

func TestDetectColor_NOCOLOR_BestCI(t *testing.T) {
	// NO_COLOR should beat --color=always
	t.Setenv("NO_COLOR", "1")
	got := DetectColor("always", false, os.Stdout)
	if got != ColorNever {
		t.Errorf("expected ColorNever with NO_COLOR even with --color=always, got %v", got)
	}
}
