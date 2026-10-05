package jsonschema

import (
	"errors"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/json/res"
	"github.com/stubbedev/maestro/internal/php"
)

// Ports tests/Composer/Test/Json/ComposerSchemaTest.php. assertAmbiguousRepositoryNotPossible
// is not ported: it is no test (PHPUnit never runs it) and would fail, as
// check() never returns false.

func resRetriever(uri string) (string, error) {
	if text, ok := res.Lookup(uri); ok {
		return text, nil
	}

	return "", errors.New("JSON schema not found at " + uri)
}

// expectedError is an error as ComposerSchemaTest::check() returns it
// (without pointer and context).
type expectedError struct {
	property, message, name string
	params                  *php.Array
}

// check ports ComposerSchemaTest::check: nil when valid.
func check(t *testing.T, json string) []Error {
	t.Helper()
	data, err := php.JSONDecode(json, false)
	if err != nil {
		t.Fatalf("invalid test JSON %s: %v", json, err)
	}
	schema := php.NewObject()
	schema.Set("$ref", res.ComposerSchemaURI)
	errs, err := NewValidator(resRetriever).Validate(data, schema)
	if err != nil {
		t.Fatal(err)
	}

	return errs
}

func assertErrors(t *testing.T, json string, expected []expectedError) {
	t.Helper()
	got := check(t, json)
	if len(got) != len(expected) {
		t.Fatalf("%s: got %d errors %+v, want %d", json, len(got), got, len(expected))
	}
	for i, e := range expected {
		g := got[i]
		if g.Property != e.property || g.Message != e.message || g.Constraint.Name != e.name || !php.LooseEquals(g.Constraint.Params, e.params) {
			t.Errorf("%s: error %d = %q %q %q %s, want %q %q %q %s", json, i, g.Property, g.Message, g.Constraint.Name,
				php.VarExport(g.Constraint.Params), e.property, e.message, e.name, php.VarExport(e.params))
		}
	}
}

func assertValid(t *testing.T, json string) {
	t.Helper()
	if errs := check(t, json); errs != nil {
		t.Errorf("%s: unexpected errors %+v", json, errs)
	}
}

func TestComposerSchema_NamePattern(t *testing.T) {
	const pattern = `^[a-z0-9]([_.-]?[a-z0-9]+)*/[a-z0-9](([_.]|-{1,2})?[a-z0-9]+)*$`
	expected := []expectedError{{"name", "Does not match the regex pattern " + pattern, "pattern", php.ArrayOf("pattern", pattern)}}

	assertErrors(t, `{"name": "vendor/-pack__age", "description": "description"}`, expected)
	assertErrors(t, `{"name": "Vendor/Package", "description": "description"}`, expected)
}

func TestComposerSchema_VersionPattern(t *testing.T) {
	const pattern = `^[vV]?\d+(?:[.-]\d+){0,3}[._-]?(?:(?:[sS][tT][aA][bB][lL][eE]|[bB][eE][tT][aA]|[bB]|[rR][cC]|[aA][lL][pP][hH][aA]|[aA]|[pP][aA][tT][cC][hH]|[pP][lL]|[pP])(?:(?:[.-]?\d+)*)?)?(?:[.-]?[dD][eE][vV]|\.x-dev)?(?:\+.*)?$|^dev-.*$`
	cases := []struct {
		version string
		valid   bool
	}{
		{"1.0.0", true},
		{"1.0.2", true},
		{"1.1.0", true},
		{"1.0.0-dev", true},
		{"1.0.0-Alpha", true},
		{"1.0.0-ALPHA", true},
		{"1.0.0-alphA", true},
		{"1.0.0-alpha3", true},
		{"1.0.0-Alpha3", true},
		{"1.0.0-ALPHA3", true},
		{"1.0.0-Beta", true},
		{"1.0.0-BETA", true},
		{"1.0.0-betA", true},
		{"1.0.0-beta232", true},
		{"1.0.0-Beta232", true},
		{"1.0.0-BETA232", true},
		{"10.4.13beta.2", true},
		{"1.0.0.RC.15-dev", true},
		{"1.0.0-RC", true},
		{"v2.0.4-p", true},
		{"dev-master", true},
		{"0.2.5.4", true},
		{"12345678-123456", true},
		{"20100102-203040-p1", true},
		{"2010-01-02.5", true},
		{"0.2.5.4-rc.2", true},
		{"dev-feature+issue-1", true},
		{"1.0.0-alpha.3.1+foo/-bar", true},
		{"00.01.03.04", true},
		{"041.x-dev", true},
		{"dev-foo bar", true},

		{"invalid", false},
		{"1.0be", false},
		{"1.0.0-meh", false},
		{"feature-foo", false},
		{"1.0 .2", false},
	}
	for _, c := range cases {
		json := `{"name": "vendor/package", "description": "description", "version": "` + c.version + `"}`
		if c.valid {
			assertValid(t, json)
		} else {
			assertErrors(t, json, []expectedError{{"version", "Does not match the regex pattern " + pattern, "pattern", php.ArrayOf("pattern", pattern)}})
		}
	}
}

func TestComposerSchema_OptionalAbandonedProperty(t *testing.T) {
	assertValid(t, `{"name": "vendor/package", "description": "description", "abandoned": true}`)
}

func TestComposerSchema_RequireTypes(t *testing.T) {
	assertErrors(t, `{"name": "vendor/package", "description": "description", "require": {"a": ["b"]} }`, []expectedError{
		{"require.a", "Array value found, but a string is required", "type", php.ArrayOf("found", "array", "expected", "a string")},
	})
}

func TestComposerSchema_MinimumStabilityValues(t *testing.T) {
	expected := []expectedError{{
		"minimum-stability",
		`Does not have a value in the enumeration ["dev","alpha","beta","rc","RC","stable"]`,
		"enum",
		php.ArrayOf("enum", php.ListOf("dev", "alpha", "beta", "rc", "RC", "stable")),
	}}

	for _, invalid := range []string{"", "dummy", "devz"} {
		assertErrors(t, `{ "name": "vendor/package", "description": "generic description", "minimum-stability": "`+invalid+`" }`, expected)
	}
	for _, valid := range []string{"dev", "alpha", "beta", "rc", "RC", "stable"} {
		assertValid(t, `{ "name": "vendor/package", "description": "generic description", "minimum-stability": "`+valid+`" }`)
	}
}

func TestComposerSchema_ReservedPolicyCustomListNamesAreRejected(t *testing.T) {
	for _, listName := range []string{"ignore-foo", "ignoremalware", "package", "license", "security", "minimum-release-age"} {
		json := `{"name": "vendor/package", "description": "description", "config": {"policy": {"` + listName + `": {"block": true}}}}`
		errs := check(t, json)
		if errs == nil {
			t.Errorf("Expected schema validation to reject reserved policy list name %q", listName)
			continue
		}
		encoded := encodeErrors(errs)
		if !strings.Contains(encoded, listName) {
			t.Errorf("%s: %s does not mention %q", json, encoded, listName)
		}
	}
}

// encodeErrors is json_encode($errors, JSON_UNESCAPED_SLASHES) of the
// errors as check() returns them.
func encodeErrors(errs []Error) string {
	list := php.NewArray()
	for _, e := range errs {
		list.Append(php.ArrayOf("property", e.Property, "message", e.Message,
			"constraint", php.ArrayOf("name", e.Constraint.Name, "params", e.Constraint.Params)))
	}
	s, _ := php.JSONEncode(list, php.JSONUnescapedSlashes)

	return s
}

func TestComposerSchema_RegularPolicyCustomListNameIsAccepted(t *testing.T) {
	assertValid(t, `{"name": "vendor/package", "description": "description", "config": {"policy": {"company-policy": {"block": true}}}}`)
}

func TestComposerSchema_IgnoreUnreachablePolicyKeyIsAccepted(t *testing.T) {
	assertValid(t, `{"name": "vendor/package", "description": "description", "config": {"policy": {"ignore-unreachable": true}}}`)
}
