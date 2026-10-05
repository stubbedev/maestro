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
	}
	for _, c := range cases {
		if got, _ := resolveURI(c.uri, c.base, true); got != c.want {
			t.Errorf("resolveURI(%q, %q) = %q, want %q", c.uri, c.base, got, c.want)
		}
	}
}
