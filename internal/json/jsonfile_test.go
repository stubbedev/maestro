package json

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/json/jsonlint"
	"github.com/stubbedev/maestro/internal/php"
)

// Ports tests/Composer/Test/Json/JsonFileTest.php,
// tests/Composer/Test/Json/JsonFormatterTest.php and
// tests/Composer/Test/Json/JsonValidationExceptionTest.php.

func jfExpectParseError(t *testing.T, text, json string) {
	t.Helper()
	result, err := ParseJSON(json, "")
	var pe *jsonlint.ParsingError
	if !errors.As(err, &pe) {
		t.Fatalf("Parsing should have failed but didn't.\nExpected:\n%q\nFor:\n%q\nGot:\n%s (%v)", text, json, php.VarExport(result), err)
	}
	if !strings.Contains(pe.Message, text) {
		t.Errorf("message %q does not contain %q", pe.Message, text)
	}
}

func TestJsonFile_ParseErrorDetectExtraComma(t *testing.T) {
	jfExpectParseError(t, "Parse error on line 2", `{
        "foo": "bar",
}`)
}

func TestJsonFile_ParseErrorDetectExtraCommaInArray(t *testing.T) {
	jfExpectParseError(t, "Parse error on line 3", `{
        "foo": [
            "bar",
        ]
}`)
}

func TestJsonFile_ParseErrorDetectUnescapedBackslash(t *testing.T) {
	jfExpectParseError(t, "Parse error on line 1", `{
        "fo\o": "bar"
}`)
}

func TestJsonFile_ParseErrorSkipsEscapedBackslash(t *testing.T) {
	jfExpectParseError(t, "Parse error on line 2", `{
        "fo\\o": "bar"
        "a": "b"
}`)
}

func TestJsonFile_ParseErrorDetectSingleQuotes(t *testing.T) {
	jfExpectParseError(t, "Parse error on line 1", `{
        'foo': "bar"
}`)
}

func TestJsonFile_ParseErrorDetectMissingQuotes(t *testing.T) {
	jfExpectParseError(t, "Parse error on line 1", `{
        foo: "bar"
}`)
}

func TestJsonFile_ParseErrorDetectArrayAsHash(t *testing.T) {
	jfExpectParseError(t, "Parse error on line 2", `{
        "foo": ["bar": "baz"]
}`)
}

func TestJsonFile_ParseErrorDetectMissingComma(t *testing.T) {
	jfExpectParseError(t, "Parse error on line 2", `{
        "foo": "bar"
        "bar": "foo"
}`)
}

func jfNewFile(t *testing.T, path string) *File {
	t.Helper()
	f, err := NewFile(path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	return f
}

func jfTempFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "composer.json")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}

	return path
}

func jfValidationError(t *testing.T, err error) *ValidationError {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected a ValidationError, got %v", err)
	}

	return ve
}

func TestJsonFile_SchemaValidation(t *testing.T) {
	json := jfNewFile(t, "testdata/Fixtures/composer.json")
	if err := json.ValidateSchema(StrictSchema, ""); err != nil {
		t.Error(err)
	}
	if err := json.ValidateSchema(LaxSchema, ""); err != nil {
		t.Error(err)
	}
}

func TestJsonFile_SchemaValidationError(t *testing.T) {
	file := jfTempFile(t, `{ "name": null }`)
	json := jfNewFile(t, file)
	expectedMessage := `"` + file + `" does not match the expected JSON schema`
	expectedError := "name : NULL value found, but a string is required"
	for _, schema := range []int{StrictSchema, LaxSchema} {
		e := jfValidationError(t, json.ValidateSchema(schema, ""))
		if e.Message != expectedMessage {
			t.Errorf("message %q", e.Message)
		}
		if !slices.Contains(e.Errors, expectedError) {
			t.Errorf("errors %q", e.Errors)
		}
	}
}

func TestJsonFile_SchemaValidationLaxAdditionalProperties(t *testing.T) {
	file := jfTempFile(t, `{ "name": "vendor/package", "description": "generic description", "foo": "bar" }`)
	json := jfNewFile(t, file)
	e := jfValidationError(t, json.ValidateSchema(StrictSchema, ""))
	if e.Message != `"`+file+`" does not match the expected JSON schema` {
		t.Errorf("message %q", e.Message)
	}
	if want := []string{"The property foo is not defined and the definition does not allow additional properties"}; !slices.Equal(e.Errors, want) {
		t.Errorf("errors %q, want %q", e.Errors, want)
	}
	if err := json.ValidateSchema(LaxSchema, ""); err != nil {
		t.Error(err)
	}
}

func TestJsonFile_SchemaValidationLaxRequired(t *testing.T) {
	file := jfTempFile(t, "")
	json := jfNewFile(t, file)
	expectedMessage := `"` + file + `" does not match the expected JSON schema`

	cases := []struct {
		contents string
		contains []string // assertContains
		equals   []string // assertEquals, when set
	}{
		{`{ }`, []string{"name : The property name is required", "description : The property description is required"}, nil},
		{`{ "name": "vendor/package" }`, nil, []string{"description : The property description is required"}},
		{`{ "description": "generic description" }`, nil, []string{"name : The property name is required"}},
		{`{ "type": "library" }`, []string{"name : The property name is required", "description : The property description is required"}, nil},
		{`{ "type": "project" }`, []string{"name : The property name is required", "description : The property description is required"}, nil},
	}
	for _, c := range cases {
		if err := os.WriteFile(file, []byte(c.contents), 0o644); err != nil {
			t.Fatal(err)
		}
		e := jfValidationError(t, json.ValidateSchema(StrictSchema, ""))
		if e.Message != expectedMessage {
			t.Errorf("%s: message %q", c.contents, e.Message)
		}
		for _, want := range c.contains {
			if !slices.Contains(e.Errors, want) {
				t.Errorf("%s: errors %q lack %q", c.contents, e.Errors, want)
			}
		}
		if c.equals != nil && !slices.Equal(e.Errors, c.equals) {
			t.Errorf("%s: errors %q, want %q", c.contents, e.Errors, c.equals)
		}
		if err := json.ValidateSchema(LaxSchema, ""); err != nil {
			t.Errorf("%s: lax: %v", c.contents, err)
		}
	}

	if err := os.WriteFile(file, []byte(`{ "name": "vendor/package", "description": "generic description" }`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := json.ValidateSchema(StrictSchema, ""); err != nil {
		t.Error(err)
	}
	if err := json.ValidateSchema(LaxSchema, ""); err != nil {
		t.Error(err)
	}
}

func TestJsonFile_CustomSchemaValidationLax(t *testing.T) {
	file := jfTempFile(t, `{ "custom": "property", "another custom": "property" }`)
	schema := filepath.Join(t.TempDir(), "schema.json")
	if err := os.WriteFile(schema, []byte(`{ "properties": { "custom": { "type": "string" }}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := jfNewFile(t, file).ValidateSchema(LaxSchema, schema); err != nil {
		t.Error(err)
	}
}

func TestJsonFile_CustomSchemaValidationStrict(t *testing.T) {
	file := jfTempFile(t, `{ "custom": "property" }`)
	schema := filepath.Join(t.TempDir(), "schema.json")
	if err := os.WriteFile(schema, []byte(`{ "properties": { "custom": { "type": "string" }}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := jfNewFile(t, file).ValidateSchema(StrictSchema, schema); err != nil {
		t.Error(err)
	}
}

func TestJsonFile_AuthSchemaValidationWithCustomDataSource(t *testing.T) {
	json, _ := php.JSONDecode(`{"github-oauth": "foo"}`, false)
	e := jfValidationError(t, ValidateJSONSchema("COMPOSER_AUTH", json, AuthSchema, ""))
	if e.Message != `"COMPOSER_AUTH" does not match the expected JSON schema` {
		t.Errorf("message %q", e.Message)
	}
	if want := []string{"github-oauth : String value found, but an object is required"}; !slices.Equal(e.Errors, want) {
		t.Errorf("errors %q, want %q", e.Errors, want)
	}
}

func TestJsonFile_ParseErrorDetectMissingCommaMultiline(t *testing.T) {
	jfExpectParseError(t, "Parse error on line 2", `{
        "foo": "barbar"

        "bar": "foo"
}`)
}

func TestJsonFile_ParseErrorDetectMissingColon(t *testing.T) {
	jfExpectParseError(t, "Parse error on line 3", `{
        "foo": "bar",
        "bar" "foo"
}`)
}

// jfAssertJSONFormat ports assertJsonFormat; options -1 is null (the
// default options).
func jfAssertJSONFormat(t *testing.T, json string, data any, options php.JSONFlag) {
	t.Helper()
	json = strings.ReplaceAll(json, "\r", "")
	if options < 0 {
		options = DefaultEncodeFlags
	}
	got, err := Encode(data, options, IndentDefault)
	if err != nil {
		t.Fatal(err)
	}
	if got != json {
		t.Errorf("got %q, want %q", got, json)
	}
}

func TestJsonFile_SimpleJsonString(t *testing.T) {
	jfAssertJSONFormat(t, `{
    "name": "composer/composer"
}`, php.ArrayOf("name", "composer/composer"), -1)
}

func TestJsonFile_TrailingBackslash(t *testing.T) {
	jfAssertJSONFormat(t, `{
    "Metadata\\": "src/"
}`, php.ArrayOf(`Metadata\`, "src/"), -1)
}

func TestJsonFile_FormatEmptyArray(t *testing.T) {
	jfAssertJSONFormat(t, `{
    "test": [],
    "test2": {}
}`, php.ArrayOf("test", php.NewArray(), "test2", php.NewObject()), -1)
}

func TestJsonFile_Escape(t *testing.T) {
	jfAssertJSONFormat(t, `{
    "Metadata\\\"": "src/"
}`, php.ArrayOf(`Metadata\"`, "src/"), -1)
}

func TestJsonFile_Unicode(t *testing.T) {
	jfAssertJSONFormat(t, `{
    "Žluťoučký \" kůň": "úpěl ďábelské ódy za €"
}`, php.ArrayOf(`Žluťoučký " kůň`, "úpěl ďábelské ódy za €"), -1)
}

func TestJsonFile_OnlyUnicode(t *testing.T) {
	jfAssertJSONFormat(t, `"\\\/ƌ"`, `\/ƌ`, php.JSONUnescapedUnicode)
}

func TestJsonFile_EscapedSlashes(t *testing.T) {
	jfAssertJSONFormat(t, `"\\\/foo"`, `\/foo`, 0)
}

func TestJsonFile_EscapedBackslashes(t *testing.T) {
	jfAssertJSONFormat(t, `"a\\b"`, `a\b`, 0)
}

func TestJsonFile_EscapedUnicode(t *testing.T) {
	// The expected JSON is "\u018c": a backslash, "u" and the code point.
	jfAssertJSONFormat(t, "\"\\"+"u018c\"", "ƌ", 0)
}

func TestJsonFile_DoubleEscapedUnicode(t *testing.T) {
	data := php.ListOf("Zdjęcia", "hjkjhl\\"+"u0119kkjk")
	encodedData, err := EncodeDefault(data)
	if err != nil {
		t.Fatal(err)
	}
	doubleEncodedData, err := EncodeDefault(php.ArrayOf("t", encodedData))
	if err != nil {
		t.Fatal(err)
	}

	decoded, _ := php.JSONDecode(doubleEncodedData, true)
	inner, _ := decoded.(*php.Array).Get("t")
	doubleData, _ := php.JSONDecode(inner.(string), true)
	if !php.StrictEquals(data, doubleData) {
		t.Errorf("got %s", php.VarExport(doubleData))
	}
}

func jfCopyTabs(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("testdata/Fixtures/tabs.json")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "tabs2.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestJsonFile_PreserveIndentationAfterRead(t *testing.T) {
	path := jfCopyTabs(t)
	jsonFile := jfNewFile(t, path)
	if _, err := jsonFile.Read(); err != nil {
		t.Fatal(err)
	}
	if err := jsonFile.Write(php.ArrayOf("foo", "baz"), DefaultEncodeFlags); err != nil {
		t.Fatal(err)
	}

	if got, _ := os.ReadFile(path); string(got) != "{\n\t\"foo\": \"baz\"\n}\n" {
		t.Errorf("got %q", got)
	}
}

func TestJsonFile_OverwritesIndentationByDefault(t *testing.T) {
	path := jfCopyTabs(t)
	jsonFile := jfNewFile(t, path)
	if err := jsonFile.Write(php.ArrayOf("foo", "baz"), DefaultEncodeFlags); err != nil {
		t.Fatal(err)
	}

	if got, _ := os.ReadFile(path); string(got) != "{\n    \"foo\": \"baz\"\n}\n" {
		t.Errorf("got %q", got)
	}
}

func jfMergeConflictData() *php.Array {
	return php.ArrayOf(
		"_readme", php.ListOf(
			"This file locks the dependencies of your project to a known state",
			"Read more about it at https://getcomposer.org/doc/01-basic-usage.md#installing-dependencies",
			"This file is @generated automatically",
		),
		"content-hash", "VCS merge conflict detected. Please run `composer update --lock`.",
		"packages", php.NewArray(),
		"packages-dev", php.NewArray(),
		"aliases", php.NewArray(),
		"minimum-stability", "stable",
		"stability-flags", php.NewArray(),
		"prefer-stable", false,
		"prefer-lowest", false,
		"platform", php.NewArray(),
		"platform-dev", php.NewArray(),
		"plugin-api-version", "2.3.0",
	)
}

func jfReadFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/Fixtures/" + name)
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func TestJsonFile_ComposerLockFileMergeConflictSimple(t *testing.T) {
	got, err := ParseJSON(jfReadFixture(t, "composer-lock-merge-conflict-simple.txt"), "/path/to/composer.lock")
	if err != nil {
		t.Fatal(err)
	}
	if !php.LooseEquals(jfMergeConflictData(), got) {
		t.Errorf("got %s", php.VarExport(got))
	}
}

func TestJsonFile_ComposerLockFileMergeConflictSimpleCRLF(t *testing.T) {
	// As in Composer, this reads the LF fixture.
	got, err := ParseJSON(jfReadFixture(t, "composer-lock-merge-conflict-simple.txt"), "/path/to/composer.lock")
	if err != nil {
		t.Fatal(err)
	}
	if !php.LooseEquals(jfMergeConflictData(), got) {
		t.Errorf("got %s", php.VarExport(got))
	}
}

func TestJsonFile_ComposerLockFileMergeConflictComplex(t *testing.T) {
	// complex files have multiple conflict markers and can thus not be simply resolved
	_, err := ParseJSON(jfReadFixture(t, "composer-lock-merge-conflict-complex.txt"), "/path/to/composer.lock")
	if _, ok := errors.AsType[*jsonlint.ParsingError](err); !ok {
		t.Errorf("expected a ParsingError, got %v", err)
	}
}

func TestJsonFile_ComposerLockFileMergeConflictComplexCRLF(t *testing.T) {
	_, err := ParseJSON(jfReadFixture(t, "composer-lock-merge-conflict-complex-crlf.txt"), "/path/to/composer.lock")
	if _, ok := errors.AsType[*jsonlint.ParsingError](err); !ok {
		t.Errorf("expected a ParsingError, got %v", err)
	}
}

func TestJsonFile_ComposerLockFileMergeConflictExtended(t *testing.T) {
	got, err := ParseJSON(jfReadFixture(t, "composer-lock-merge-conflict-extended.txt"), "/path/to/composer.lock")
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := got.(*php.Array).Get("content-hash")
	if hash != "VCS merge conflict detected. Please run `composer update --lock`." {
		t.Errorf("content-hash %v", hash)
	}
}

func TestJsonFormatter_UnicodeWithPrependedSlash(t *testing.T) {
	data := `"\\\` + "u0119\""
	expected := `"\\ę"`
	got, err := Format(data, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestJsonFormatter_Utf16SurrogatePair(t *testing.T) {
	escaped := `"\` + `ud83d\` + `ude00"`
	got, err := Format(escaped, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if got != escaped {
		t.Errorf("got %q, want %q", got, escaped)
	}
}

func TestJsonValidationException_GetErrors(t *testing.T) {
	cases := []struct {
		message         string
		errors          []string
		expectedMessage string
		expectedErrors  []string
	}{
		{"test message", []string{}, "test message", []string{}},
		{"", []string{"foo"}, "", []string{"foo"}},
	}
	for _, c := range cases {
		object := &ValidationError{Message: c.message, Errors: c.errors}
		if object.Error() != c.expectedMessage {
			t.Errorf("message %q", object.Error())
		}
		if !slices.Equal(object.Errors, c.expectedErrors) {
			t.Errorf("errors %q", object.Errors)
		}
	}
}

func TestJsonValidationException_GetErrorsWhenNoErrorsProvided(t *testing.T) {
	object := &ValidationError{Message: "test message"}
	if len(object.Errors) != 0 {
		t.Errorf("errors %q", object.Errors)
	}
}
