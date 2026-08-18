package output

import (
	"strconv"
	"testing"
)

// isBase16OrDefault reports whether idx is either "" (terminal default
// foreground) or a base-16 palette index ("0"-"15"). Neutral UI roles must
// stay within this range so they inherit the terminal theme instead of
// hardcoding a 256-color grey that may not read well against every
// background.
func isBase16OrDefault(idx string) bool {
	if idx == "" {
		return true
	}
	n, err := strconv.Atoi(idx)
	if err != nil {
		return false
	}
	return n >= 0 && n <= 15
}

func TestDefaultPalette_NeutralRolesStayBase16(t *testing.T) {
	p := DefaultPalette()

	roles := map[string]ColorRole{
		"Muted":     p.Muted,
		"Skipped":   p.Skipped,
		"Divider":   p.Divider,
		"Output":    p.Output,
		"Elapsed":   p.Elapsed,
		"Label":     p.Label,
		"Key":       p.Key,
		"Value":     p.Value,
		"TableHead": p.TableHead,
	}
	for name, r := range roles {
		if !isBase16OrDefault(r.Light) {
			t.Errorf("%s.Light = %q: not base-16 or default foreground", name, r.Light)
		}
		if !isBase16OrDefault(r.Dark) {
			t.Errorf("%s.Dark = %q: not base-16 or default foreground", name, r.Dark)
		}
	}
}

func TestDefaultPalette_ANSIDerivedFromIndex(t *testing.T) {
	p := DefaultPalette()

	// Roles with a set Light/Dark index must have an ANSI escape that
	// matches what ansiForeground would derive from that index, so the two
	// fields can never silently drift apart.
	roles := map[string]ColorRole{
		"Muted":   p.Muted,
		"Skipped": p.Skipped,
		"Divider": p.Divider,
		"Elapsed": p.Elapsed,
	}
	for name, r := range roles {
		want := ansiForeground(r.Light)
		if r.ANSI != want {
			t.Errorf("%s.ANSI = %q, want %q derived from Light=%q", name, r.ANSI, want, r.Light)
		}
	}

	// Roles with default foreground (empty Light/Dark) must have no ANSI
	// color escape.
	defaultFG := map[string]ColorRole{
		"Output":    p.Output,
		"Label":     p.Label,
		"Key":       p.Key,
		"Value":     p.Value,
		"TableHead": p.TableHead,
	}
	for name, r := range defaultFG {
		if r.ANSI != "" {
			t.Errorf("%s.ANSI = %q, want \"\" for default foreground", name, r.ANSI)
		}
	}
}

func TestDefaultPalette_DimChromeVsSecondaryTextTiers(t *testing.T) {
	p := DefaultPalette()

	// Hard requirement: "dim chrome" (Divider, Elapsed) must stay visually
	// distinguishable from "secondary text" (Label, Key). Both tiers sit on
	// the same base-16 grey or default foreground, so the distinction must
	// come from a text attribute (Bold/Italic/Dim), not from color alone.
	chrome := []ColorRole{p.Divider, p.Elapsed}
	secondary := []ColorRole{p.Label, p.Key}

	for _, c := range chrome {
		if c.Bold || c.Italic || c.Dim {
			t.Errorf("dim chrome role has an attribute that could be mistaken for secondary text: %+v", c)
		}
	}
	for _, s := range secondary {
		if !s.Bold && !s.Italic && !s.Dim {
			t.Errorf("secondary text role has no attribute distinguishing it from dim chrome: %+v", s)
		}
		// Secondary text must not silently reuse the dim-chrome grey index
		// without an attribute-level distinction.
		if (s.Light == p.Divider.Light && s.Dark == p.Divider.Dark) && !s.Bold && !s.Dim && !s.Italic {
			t.Errorf("secondary text role collides with dim chrome tier: %+v", s)
		}
	}
}

func TestDefaultPalette_HostColors(t *testing.T) {
	p := DefaultPalette()

	if got := len(p.HostColors); got != 8 {
		t.Fatalf("expected 8 host colors, got %d", got)
	}

	// Host colors are color-only: no bold/italic (so no-color mode degrades
	// to plain text, matching the transport roles they replace).
	for i, c := range p.HostColors {
		if c.Light == "" || c.Dark == "" {
			t.Errorf("HostColors[%d]: expected Light and Dark set, got %+v", i, c)
		}
		if c.Bold {
			t.Errorf("HostColors[%d]: expected no Bold, got %+v", i, c)
		}
		if c.Italic {
			t.Errorf("HostColors[%d]: expected no Italic, got %+v", i, c)
		}
		if c.ANSI == "" {
			t.Errorf("HostColors[%d]: expected ANSI set for text renderer, got %+v", i, c)
		}
	}

	// Host colors must not collide with status outcome colors, so a host
	// badge is never mistaken for a status glyph (green/yellow/red/grey).
	status := []ColorRole{p.OK, p.Changed, p.Failed, p.Skipped}
	for i, hc := range p.HostColors {
		for j, sc := range status {
			if hc.Light == sc.Light && hc.Dark == sc.Dark {
				t.Errorf("HostColors[%d] collides with status color %d (Light=%s Dark=%s)",
					i, j, hc.Light, hc.Dark)
			}
		}
	}

	// All host colors must be visually distinct from each other.
	for i, a := range p.HostColors {
		for j, b := range p.HostColors {
			if i < j && a.Light == b.Light && a.Dark == b.Dark {
				t.Errorf("HostColors[%d] and [%d] are identical (%+v)", i, j, a)
			}
		}
	}
}

func TestSemanticPalette_HostColor(t *testing.T) {
	p := DefaultPalette()
	n := len(p.HostColors)

	// Slot 0 and slot 1 resolve to the first two colors.
	if c, ok := p.HostColor(0); !ok || c.ANSI != p.HostColors[0].ANSI {
		t.Errorf("HostColor(0) mismatch: %+v ok=%v", c, ok)
	}
	if c, ok := p.HostColor(1); !ok || c.ANSI != p.HostColors[1].ANSI {
		t.Errorf("HostColor(1) mismatch: %+v ok=%v", c, ok)
	}

	// Overflow wraps modulo the palette length: slot n == slot 0.
	if c, ok := p.HostColor(n); !ok || c.ANSI != p.HostColors[0].ANSI {
		t.Errorf("HostColor(%d) should wrap to slot 0: %+v ok=%v", n, c, ok)
	}
	if c, ok := p.HostColor(n + 3); !ok || c.ANSI != p.HostColors[3].ANSI {
		t.Errorf("HostColor(%d) should wrap to slot 3: %+v ok=%v", n+3, c, ok)
	}

	// Unknown target / before run start → false.
	if _, ok := p.HostColor(-1); ok {
		t.Error("HostColor(-1) should be ok=false")
	}

	// Empty palette → false.
	var empty SemanticPalette
	if _, ok := empty.HostColor(0); ok {
		t.Error("HostColor on empty palette should be ok=false")
	}
}
