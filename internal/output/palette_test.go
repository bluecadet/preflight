package output

import "testing"

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
