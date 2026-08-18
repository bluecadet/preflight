package output

import (
	"testing"

	"github.com/muesli/termenv"
)

func TestSgr256Foreground(t *testing.T) {
	tests := []struct {
		name   string
		code   string
		wantN  int
		wantOk bool
	}{
		{"host color escape", "\033[38;5;81m", 81, true},
		{"low index", "\033[38;5;0m", 0, true},
		{"base16 code is not 8-bit", "\033[90m", 0, false},
		{"standard color is not 8-bit", "\033[32m", 0, false},
		{"bold attribute passes through", "\033[1m", 0, false},
		{"empty string", "", 0, false},
		{"malformed suffix", "\033[38;5;81x", 0, false},
		{"non-numeric body", "\033[38;5;xxm", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, ok := sgr256Foreground(tt.code)
			if ok != tt.wantOk {
				t.Fatalf("sgr256Foreground(%q) ok = %v, want %v", tt.code, ok, tt.wantOk)
			}
			if ok && n != tt.wantN {
				t.Errorf("sgr256Foreground(%q) n = %d, want %d", tt.code, n, tt.wantN)
			}
		})
	}
}

func TestDowngradeSGR(t *testing.T) {
	hostBlue := DefaultPalette().HostColors[0].ANSI // "\033[38;5;81m"

	tests := []struct {
		name    string
		profile termenv.Profile
		code    string
		want    string
	}{
		{
			name:    "TrueColor leaves 8-bit host color unchanged",
			profile: termenv.TrueColor,
			code:    hostBlue,
			want:    hostBlue,
		},
		{
			name:    "ANSI256 leaves 8-bit host color unchanged",
			profile: termenv.ANSI256,
			code:    hostBlue,
			want:    hostBlue,
		},
		{
			name:    "ANSI (16-color) downconverts the host color",
			profile: termenv.ANSI,
			code:    hostBlue,
			want:    "\033[" + termenv.ANSI.Convert(termenv.ANSI256Color(81)).Sequence(false) + "m",
		},
		{
			name:    "Ascii is treated as ANSI, not stripped to no color",
			profile: termenv.Ascii,
			code:    hostBlue,
			want:    "\033[" + termenv.ANSI.Convert(termenv.ANSI256Color(81)).Sequence(false) + "m",
		},
		{
			name:    "base-16 codes pass through on every profile",
			profile: termenv.ANSI,
			code:    "\033[90m",
			want:    "\033[90m",
		},
		{
			name:    "bold attribute passes through unchanged",
			profile: termenv.ANSI,
			code:    "\033[1m",
			want:    "\033[1m",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := downgradeSGR(tt.profile, tt.code)
			if got != tt.want {
				t.Errorf("downgradeSGR(%v, %q) = %q, want %q", tt.profile, tt.code, got, tt.want)
			}
		})
	}
}

// TestDowngradeSGR_ANSIStaysWithinBase16 is a coarser sanity check that the
// 16-color downconversion never emits an 8-bit escape: every host color,
// run through the ANSI profile, must come out as a plain "\033[3Nm" or
// "\033[9Nm" code.
func TestDowngradeSGR_ANSIStaysWithinBase16(t *testing.T) {
	for i, c := range DefaultPalette().HostColors {
		got := downgradeSGR(termenv.ANSI, c.ANSI)
		if _, ok := sgr256Foreground(got); ok {
			t.Errorf("HostColors[%d]: downgraded to ANSI still 8-bit: %q", i, got)
		}
	}
}
