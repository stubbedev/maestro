package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// Factory::validateJsonSchema and JsonConfigSource::manipulateJson join
// their messages with PHP_EOL, "\r\n" on Windows.
func TestConfig_MessagesUseWindowsEOL(t *testing.T) {
	php.SetEOLForTest(t, "\r\n")
	dir := t.TempDir()

	auth := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(auth, []byte(`{"github-oauth": "x", "bearer": {"x.org": 1}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := json.NewFile(auth, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = ValidateJSONSchema(nil, file, json.AuthSchema, "")
	want := `"` + auth + `" does not match the expected JSON schema, this may result in errors and should be resolved:` +
		"\r\n - github-oauth : String value found, but an object is required\r\n - bearer.x.org : Integer value found, but a string is required"
	if uve, ok := errors.AsType[*util.UnexpectedValueError](err); !ok || uve.Message != want {
		t.Errorf("validateJsonSchema: %v\nwant %q", err, want)
	}

	path := filepath.Join(dir, "composer.json")
	if err := os.WriteFile(path, []byte(`{"autoload-dev": {"psr-0": []}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	file, err = json.NewFile(path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = NewJSONConfigSource(file, false).AddProperty("extra.foo", false)
	want = "Failed to update composer.json with a valid format, reverting to the original content. Please report an issue to us with details (command you run and a copy of your composer.json). \r\n" +
		"autoload-dev.psr-0 : Array value found, but an object is required"
	if re, ok := errors.AsType[*util.RuntimeError](err); !ok || re.Message != want {
		t.Errorf("addProperty: %v\nwant %q", err, want)
	}
}
