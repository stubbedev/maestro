package json

import (
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// TestManipulatorAddLinkEscapesConstraint: a constraint with characters
// that are special in a JSON string stays one string value, whether the
// link is added or an existing one is updated, and a plain one is written
// between bare quotes as Composer writes it.
func TestManipulatorAddLinkEscapesConstraint(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, constraint := range []string{
			`1.0", "evil/pkg": "*`,
			`dev-a\b`,
			`dev-a\`,
			"dev-a\nb",
			"dev-a\x00b",
			`dev-feature/x`,
			`dev-é`,
			`^1.0 || ~2.0@dev`,
		} {
			contents := "{\n    \"require\": {\n        \"other/pkg\": \"*\"\n    }\n}"
			if existing {
				contents = "{\n    \"require\": {\n        \"vendor/pkg\": \"1.0\"\n    }\n}"
			}
			m, err := NewManipulator(contents)
			if err != nil {
				t.Fatal(err)
			}
			if ok, err := m.AddLink("require", "vendor/pkg", constraint, false); err != nil || !ok {
				t.Fatalf("AddLink(%q) = %v, %v", constraint, ok, err)
			}
			decoded, err := php.JSONDecode(m.Contents(), true)
			if err != nil {
				t.Fatalf("AddLink(%q) made invalid JSON: %v\n%s", constraint, err, m.Contents())
			}
			root, _ := decoded.(*php.Array)
			require, _ := root.Get("require")
			links, _ := require.(*php.Array)
			if got, _ := links.Get("vendor/pkg"); got != constraint {
				t.Errorf("AddLink(%q) stored %q\n%s", constraint, got, m.Contents())
			}
			if links.Has("evil/pkg") {
				t.Errorf("AddLink(%q) injected a key\n%s", constraint, m.Contents())
			}
		}
	}

	m, err := NewManipulator("{\n    \"require\": {\n        \"vendor/pkg\": \"1.0\"\n    }\n}")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddLink("require", "vendor/pkg", "dev-feature/x é", false); err != nil {
		t.Fatal(err)
	}
	if want := "{\n    \"require\": {\n        \"vendor/pkg\": \"dev-feature/x é\"\n    }\n}\n"; m.Contents() != want {
		t.Errorf("got %q, want %q", m.Contents(), want)
	}
}
