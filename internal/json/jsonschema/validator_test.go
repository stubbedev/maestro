package jsonschema

import (
	"errors"
	"sync"
	"testing"

	"github.com/stubbedev/maestro/internal/json/res"
	"github.com/stubbedev/maestro/internal/php"
)

func refTo(uri string) *php.Object {
	o := php.NewObject()
	o.Set("$ref", uri)

	return o
}

// The embedded schemas are shared between validators; validating
// concurrently must neither race nor change them.
func TestValidatorConcurrent(t *testing.T) {
	doc, _ := php.JSONDecode(`{"name": "Foo/Bar", "require": {"a/b": 1}, "repositories": [{"type": "vcs"}], "config": {"policy": {"x": true}}}`, false)
	want, err := NewValidator(resRetriever).Validate(doc, refTo(res.ComposerSchemaURI))
	if err != nil || len(want) == 0 {
		t.Fatalf("got %v, %v", want, err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 20 {
				got, err := NewValidator(resRetriever).Validate(doc, refTo(res.ComposerSchemaURI))
				if err != nil || formatErrors(got) != formatErrors(want) {
					t.Errorf("got %s, %v", formatErrors(got), err)
				}
			}
		})
	}
	wg.Wait()
}

func TestValidatorDoesNotModifyArguments(t *testing.T) {
	schemaText := `{"definitions": {"a": {"type": "integer"}}, "properties": {"x": {"$ref": "#/definitions/a"}}}`
	schema, _ := php.JSONDecode(schemaText, false)
	doc, _ := php.JSONDecode(`{"x": "s"}`, false)
	errs, err := NewValidator(nil).Validate(doc, schema)
	if err != nil || len(errs) != 1 || errs[0].Message != "String value found, but an integer is required" {
		t.Fatalf("got %+v, %v", errs, err)
	}
	if got, _ := php.JSONEncode(schema, php.JSONUnescapedSlashes); got != `{"definitions":{"a":{"type":"integer"}},"properties":{"x":{"$ref":"#/definitions/a"}}}` {
		t.Errorf("schema modified: %s", got)
	}
}

func TestValidatorRetrieverErrors(t *testing.T) {
	failing := errors.New("JSON schema not found at file:///missing.json")
	_, err := NewValidator(func(string) (string, error) { return "", failing }).Validate(php.NewObject(), refTo("file:///missing.json"))
	if !errors.Is(err, failing) {
		t.Errorf("got %v", err)
	}

	_, err = NewValidator(func(string) (string, error) { return "{", nil }).Validate(php.NewObject(), refTo("file:///broken.json"))
	if err == nil || err.Error() != "JSON syntax is malformed" {
		t.Errorf("got %v", err)
	}

	_, err = NewValidator(resRetriever).Validate(php.NewObject(), refTo(res.ComposerSchemaURI+"#/nope"))
	if err == nil || err.Error() != "File: "+res.ComposerSchemaURI+" is found, but could not resolve fragment: #/nope" {
		t.Errorf("got %v", err)
	}
}

func TestResolveURI(t *testing.T) {
	cases := []struct{ uri, base, want string }{
		{"#/definitions/a", "internal://provided-schema/", "internal://provided-schema/#/definitions/a"},
		{"./composer-schema.json", res.LockSchemaURI, res.ComposerSchemaURI},
		{"../x.json#/a", "file:///a/b/c.json", "file:///a/x.json#/a"},
		{"/abs.json", "http://h/a/b", "http://h/abs.json"},
		{"https://other/x", "file:///a", "https://other/x"},
		{"", "file:///a", "file:///a"},
		{"b.json#/x", "http://h", "http://h/b.json#/x"},
	}
	for _, c := range cases {
		if got, _ := resolveURI(c.uri, c.base, true); got != c.want {
			t.Errorf("resolveURI(%q, %q) = %q, want %q", c.uri, c.base, got, c.want)
		}
	}
}

func TestValidatorFloatLimitsAreNotCastToInt(t *testing.T) {
	cases := []struct {
		name, input, schema, message, constraint, param string
		value                                           float64
	}{
		{"Float divisibleBy is reported without being cast to int", `{"value": 35}`, `{"type": "object", "properties": {"value": {"type": "integer", "divisibleBy": 1.5}}}`, "Is not divisible by 1.5", "divisibleBy", "divisibleBy", 1.5},
		{"Float minimum is reported without being cast to int", `{"value": 1.2}`, `{"properties": {"value": {"type": "number", "minimum": 1.5}}}`, "Must have a minimum value greater than or equal to 1.5", "minimum", "minimum", 1.5},
		{"Float exclusive minimum is reported without being cast to int", `{"value": 1.5}`, `{"properties": {"value": {"type": "number", "minimum": 1.5, "exclusiveMinimum": true}}}`, "Must have a minimum value greater than 1.5", "exclusiveMinimum", "minimum", 1.5},
		{"Float maximum is reported without being cast to int", `{"value": 2.7}`, `{"properties": {"value": {"type": "number", "maximum": 2.5}}}`, "Must have a maximum value less than or equal to 2.5", "maximum", "maximum", 2.5},
		{"Float exclusive maximum is reported without being cast to int", `{"value": 2.5}`, `{"properties": {"value": {"type": "number", "maximum": 2.5, "exclusiveMaximum": true}}}`, "Must have a maximum value less than 2.5", "exclusiveMaximum", "maximum", 2.5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc, err := php.JSONDecode(c.input, false)
			if err != nil {
				t.Fatal(err)
			}
			schema, err := php.JSONDecode(c.schema, false)
			if err != nil {
				t.Fatal(err)
			}
			errs, err := NewValidator(nil).Validate(doc, schema)
			if err != nil {
				t.Fatal(err)
			}
			if len(errs) != 1 {
				t.Fatalf("got %+v", errs)
			}
			e := errs[0]
			if e.Property != "value" || e.Pointer != "/value" || e.Message != c.message || e.Context != ErrorDocumentValidation || e.Constraint.Name != c.constraint {
				t.Errorf("got %+v", e)
			}
			if got, ok := e.Constraint.Params.Get(c.param); !ok || got != any(c.value) || e.Constraint.Params.Len() != 1 {
				t.Errorf("params %+v", e.Constraint.Params)
			}
		})
	}
}

func TestValidatorNestedIDsChangeTheBaseURI(t *testing.T) {
	schema, _ := php.JSONDecode(`{"definitions":{"a":{"$id":"http://example.com/nested/","definitions":{"b":{"$id":"item.json","type":"integer"}}}},"properties":{"x":{"$ref":"http://example.com/nested/item.json"}}}`, false)
	doc, _ := php.JSONDecode(`{"x":"s"}`, false)
	errs, err := NewValidator(nil).Validate(doc, schema)
	if err != nil || len(errs) != 1 || errs[0].Message != "String value found, but an integer is required" {
		t.Fatalf("got %+v, %v", errs, err)
	}
}

func TestValidatorLengthCountsIllFormedSubsequencesOnce(t *testing.T) {
	schema, _ := php.JSONDecode(`{"properties":{"x":{"type":"string","maxLength":2}}}`, false)
	cases := []struct {
		value string
		errs  int
	}{
		{"\xe2\x82\xe2\x82", 0},
		{"\xe0\x80\x80", 1},
	}
	for _, c := range cases {
		doc := php.NewObject()
		doc.Set("x", c.value)
		errs, err := NewValidator(nil).Validate(doc, schema)
		if err != nil || len(errs) != c.errs {
			t.Errorf("%q: got %+v, %v", c.value, errs, err)
		}
	}
}

func TestIsRfc3339_FractionalSecondsBeyondMicroseconds(t *testing.T) {
	if !isRfc3339("2020-01-01T10:00:00.1234567Z") {
		t.Error("seven fractional digits are truncated to microseconds, not rejected")
	}
}

func TestIsValidRelativeReference_PercentNeedsTwoHexDigits(t *testing.T) {
	if isValidRelativeReference("a%zz") {
		t.Error("a%zz is not a valid relative reference")
	}
	if !isValidRelativeReference("a%2Fb") {
		t.Error("a%2Fb is a valid relative reference")
	}
}
