package ui

import "testing"

func TestGlyphs(t *testing.T) {
	for g, alts := range glyphs {
		if alts[0] == "" || alts[1] == "" {
			t.Errorf("glyph %d has no form", g)
		}
		for _, r := range alts[1] {
			if r > 0x7e {
				t.Errorf("glyph %d: ASCII form %q is not ASCII", g, alts[1])
			}
		}
	}
}

// The prompt glyph goes before the question's first line; its text stays.
func TestPrompt(t *testing.T) {
	want := "\n\r\n" + RoleAccent.Wrap(GlyphPrompt.String()) + " Name [<comment>x</comment>]: "
	if got := Prompt("\n\r\nName [<comment>x</comment>]: "); got != want {
		t.Errorf("Prompt = %q, want %q", got, want)
	}
}
