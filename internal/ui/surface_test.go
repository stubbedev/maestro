package ui

import (
	"slices"
	"testing"
)

func TestSurfaceStyle(t *testing.T) {
	for _, tc := range []struct {
		surface    Surface
		text, want string
	}{
		{Free, "MIT", "<maestro-success>MIT</maestro-success>"},
		{Free, "", ""},
		{Free, "a </comment> b", "a </comment> b"},
		{Free, "<2.0 || >=3", "<maestro-success><2.0 || >=3</maestro-success>"},
		{Frozen, "MIT", "MIT"},
	} {
		if got := tc.surface.Style(RoleSuccess, tc.text); got != tc.want {
			t.Errorf("%s.Style(%q) = %q, want %q", tc.surface, tc.text, got, tc.want)
		}
	}
}

func TestFormats(t *testing.T) {
	fs := Formats{{"text", Free}, {"json", Frozen}}
	if got := fs.Names(); !slices.Equal(got, []string{"text", "json"}) {
		t.Errorf("Names() = %q", got)
	}
	for name, want := range map[any]Surface{"text": Free, "json": Frozen, "xml": Frozen, nil: Frozen} {
		if got := fs.Surface(name); got != want {
			t.Errorf("Surface(%v) = %s, want %s", name, got, want)
		}
	}
}
