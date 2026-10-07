package json

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// useSchemaMemo has ValidateJSONSchema use a memo at a fresh path for the
// test.
func useSchemaMemo(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "schema", "validated")
	UseSchemaMemo(path)
	t.Cleanup(func() { schemaMemos.Store(nil) })
	if schemaMemos.Load() == nil {
		t.Skip("the test binary cannot tell what it is")
	}

	return path
}

// A document that validated is remembered, one with findings is not and
// reports them every time, and a remembered document is not validated
// again.
func TestValidateJSONSchema_Memo(t *testing.T) {
	path := useSchemaMemo(t)
	valid := php.ArrayOf("name", "acme/app", "description", "x")
	invalid := php.ArrayOf("name", "acme/app", "minimum-stability", "bogus")

	if err := ValidateJSONSchema("composer.json", valid, LaxSchema, ""); err != nil {
		t.Fatal(err)
	}
	lines := func() []string {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}

		return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	}
	if got := lines(); len(got) != 2 || got[0]+"\n" != schemaMemoHeader() {
		t.Fatalf("memo file %q", got)
	}

	for range 2 {
		if _, ok := errors.AsType[*ValidationError](ValidateJSONSchema("composer.json", invalid, LaxSchema, "")); !ok {
			t.Fatal("findings were not reported")
		}
	}
	if got := lines(); len(got) != 2 {
		t.Errorf("a document with findings was remembered: %q", got)
	}

	// what is remembered is not validated
	encoded, _ := php.JSONEncode(invalid, 0)
	schemaMemos.Load().remember(schemaKey(LaxSchema, encoded))
	if err := ValidateJSONSchema("composer.json", invalid, LaxSchema, ""); err != nil {
		t.Errorf("a remembered document was validated: %v", err)
	}
	// in another schema, it is
	if err := ValidateJSONSchema("composer.json", invalid, StrictSchema, ""); err == nil {
		t.Error("a document remembered for another schema was not validated")
	}
}

// A memo file of another binary or format holds nothing.
func TestValidateJSONSchema_MemoOfAnotherBinary(t *testing.T) {
	path := useSchemaMemo(t)
	invalid := php.ArrayOf("name", "acme/app", "minimum-stability", "bogus")
	encoded, _ := php.JSONEncode(invalid, 0)
	sum := schemaKey(LaxSchema, encoded)
	m := &schemaMemo{path: path}
	m.remember(sum)
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), schemaMemoHeader(), "maestro schema memo 1 1 1\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ValidateJSONSchema("composer.json", invalid, LaxSchema, ""); err == nil {
		t.Error("another binary's memo was trusted")
	}
}
