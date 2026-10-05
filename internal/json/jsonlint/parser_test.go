package jsonlint

// Ports tests/JsonParserTest.php and tests/Utf8ValidatorTest.php of
// seld/jsonlint 1.12.1. The two tests of the PHP < 7.1 "_empty_" key
// (testDetectsKeyOverridesWithEmpty, testDuplicateKeysWithEmpty) are skipped
// there on PHP 8 and have no port.

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// esc turns <u> into a backslash-u, so JSON unicode escapes survive
// editing tools (docs/PORTING.md, "Tooling hazard").
func esc(s string) string { return strings.ReplaceAll(s, "<u>", `\`+"u") }

// validJSON is JsonParserTest::$json.
var validJSON = []string{
	"42", "42.3", "0.3", "-42", "-42.3", "-0.3",
	"2e1", "2E1", "-2e1", "-2E1", "2E+2", "2E-2", "-2E+2", "-2E-2",
	"true", "false", "null", `""`, "[]", "{}", `"string"`,
	`["a", "sdfsd"]`,
	`{"foo":"bar", "bar":"baz", "":"buz"}`,
	`{"":"foo", "_empty_":"bar"}`,
	esc(`"<u>00c9v<u>00e9nement"`),
	`"http:\/\/foo.com"`,
	`"zo\\mg"`,
	esc(`{"test":"<u>00c9v<u>00e9nement"}`),
	esc(`["<u>00c9v<u>00e9nement"]`),
	`"foo/bar"`,
	`{"test":"http:\/\/foo\\zomg"}`,
	`["http:\/\/foo\\zomg"]`,
	`{"":"foo"}`,
	`{"a":"b", "b":"c"}`,
	"0",
	`""`,
	esc(`"<u>0022"`),
	esc(`"Argument <u>0022input<u>0022 has an invalid value: ..."`),
	`"👻"`,
	esc(`"<u>1f47d"`),
}

// assertDecodes checks that parse(input, flags) equals json_decode(want).
func assertDecodes(t *testing.T, input string, flags int, want string) {
	t.Helper()
	expected, err := php.JSONDecode(want, flags&ParseToAssoc != 0)
	if err != nil {
		t.Fatalf("json_decode(%q): %v", want, err)
	}
	got, err := Parse(input, flags)
	if err != nil {
		t.Fatalf("Parse(%q): %v", input, err)
	}
	if g, w := php.VarExport(got), php.VarExport(expected); g != w {
		t.Errorf("Parse(%q) = %s, want %s", input, g, w)
	}
}

// parseError parses input and returns the error message, failing when
// parsing succeeds.
func parseError(t *testing.T, input string, flags int, failure string) error {
	t.Helper()
	_, err := Parse(input, flags)
	if err == nil {
		t.Fatal(failure)
	}

	return err
}

func assertContains(t *testing.T, err error, needle string) {
	t.Helper()
	if _, ok := errors.AsType[*ParsingError](err); !ok {
		t.Fatalf("got %T %v, want a ParsingException", err, err)
	}
	if !strings.Contains(err.Error(), needle) {
		t.Errorf("message %q does not contain %q", err.Error(), needle)
	}
}

func TestJsonParser_ParsesValidStrings(t *testing.T) {
	for _, input := range validJSON {
		assertDecodes(t, input, 0, input)
	}
}

func TestJsonParser_ErrorOnTrailingComma(t *testing.T) {
	err := parseError(t, "{\n    \"foo\":\"bar\",\n}", 0, "Invalid trailing comma should be detected")
	assertContains(t, err, "It appears you have an extra trailing comma")
}

func TestJsonParser_ErrorOnInvalidQuotes(t *testing.T) {
	err := parseError(t, "{\n    \"foo\": 'bar',\n}", 0, "Invalid quotes for string should be detected")
	assertContains(t, err, "Invalid string, it appears you used single quotes instead of double quotes")
}

func TestJsonParser_ErrorOnUnescapedBackslash(t *testing.T) {
	err := parseError(t, "{\n    \"foo\": \"bar\\z\",\n}", 0, "Invalid unescaped string should be detected")
	assertContains(t, err, `Invalid string, it appears you have an unescaped backslash at: \z`)
}

func TestJsonParser_ErrorOnLongUnescapedBackslash(t *testing.T) {
	err := parseError(t, `{
    "#3026386-26 - Drush fatal error after upgrading to 8.6.6, 8.5.9, or 7.62: PHP Fatal error: Uncaught TYPO3\PharStreamWrapper\Exception": "https://www.drupal.org/files/issues/2019-01-16/d8-3026386-26.patch"
}`, 0, "Invalid unescaped string should be detected")
	want := `Parse error on line 1:
{    "#3026386-26 - Drush
----^
Invalid string, it appears you have an unescaped backslash at: \Phar`
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestJsonParser_ErrorOnLongUnescapedBackslash2(t *testing.T) {
	err := parseError(t, `{
    "#3026386-26 - Drush fatal error after upgrading to 8.6.6, 8.5.9, or 7.62: PHP Fatal error": "https://www.drupal.org/files/issues/201\9-01-16/d8-3026386-26.patch
}`, 0, "Invalid unescaped string should be detected")
	want := `Parse error on line 2:
...: PHP Fatal error": "https://www.drupal.
----------------------^
Invalid string, it appears you have an unescaped backslash at: \9-01`
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestJsonParser_ErrorOnUnterminatedString(t *testing.T) {
	err := parseError(t, `{"bar": "foo}`, 0, "Invalid unterminated string should be detected")
	assertContains(t, err, "Invalid string, it appears you forgot to terminate a string, or attempted to write a multiline string which is invalid")
}

func TestJsonParser_ErrorOnMultilineString(t *testing.T) {
	err := parseError(t, "{\"bar\": \"foo\nbar\"}", 0, "Invalid multi-line string should be detected")
	assertContains(t, err, "Invalid string, it appears you forgot to terminate a string, or attempted to write a multiline string which is invalid")
}

func TestJsonParser_ErrorAtBeginning(t *testing.T) {
	err := parseError(t, "\n\n", 0, "Empty string should be invalid")
	assertContains(t, err, "Parse error on line 1:\n\n^")
}

func TestJsonParser_AvoidLeakingInfoForInvalidFiles(t *testing.T) {
	err := parseError(t, "ABCD", 0, "Empty string should be invalid")
	assertContains(t, err, "Parse error on line 1:\nA...\n^")
}

func TestJsonParser_ParsesMultiInARow(t *testing.T) {
	for _, input := range validJSON {
		assertDecodes(t, input, 0, input)
	}
}

func TestJsonParser_DetectsKeyOverrides(t *testing.T) {
	err := parseError(t, `{"a":"b", "a":"c"}`, DetectKeyConflicts, "Duplicate keys should not be allowed")
	var dke *DuplicateKeyError
	if !errors.As(err, &dke) {
		t.Fatalf("got %T, want a DuplicateKeyException", err)
	}
	assertContains(t, err, "Duplicate key: a")
	if dke.Key() != "a" {
		t.Errorf("key %q", dke.Key())
	}
	if got := php.VarExport(dke.Details.Array()); got != php.VarExport(php.ArrayOf("line", 1, "key", "a")) {
		t.Errorf("details %s", got)
	}
}

func TestJsonParser_DuplicateKeys(t *testing.T) {
	str := `{"a":"b", "a":"c", "a":"d"}`

	result, err := Parse(str, AllowDuplicateKeys)
	if err != nil {
		t.Fatal(err)
	}
	o := result.(*php.Object)
	for _, name := range []string{"a", "a.1", "a.2"} {
		if !o.Has(name) {
			t.Errorf("object has no attribute %q", name)
		}
	}

	result, err = Parse(str, AllowDuplicateKeys|ParseToAssoc)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := php.VarExport(result), php.VarExport(php.ArrayOf("a", "b", "a.1", "c", "a.2", "d")); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestJsonParser_DuplicateKeysToArray(t *testing.T) {
	str := `{"a":"b", "a":"c", "a":"d"}`

	result, err := Parse(str, AllowDuplicateKeysToArray)
	if err != nil {
		t.Fatal(err)
	}
	a, ok := result.(*php.Object).Get("a")
	if !ok {
		t.Fatal("object has no attribute a")
	}
	dups, ok := a.(*php.Object).Get("__duplicates__")
	if !ok {
		t.Fatal("a has no attribute __duplicates__")
	}
	if got, want := php.VarExport(dups), php.VarExport(php.ListOf("b", "c", "d")); got != want {
		t.Errorf("got %s, want %s", got, want)
	}

	result, err = Parse(str, AllowDuplicateKeysToArray|ParseToAssoc)
	if err != nil {
		t.Fatal(err)
	}
	want := php.ArrayOf("a", php.ArrayOf("__duplicates__", php.ListOf("b", "c", "d")))
	if got := php.VarExport(result); got != php.VarExport(want) {
		t.Errorf("got %s, want %s", got, php.VarExport(want))
	}
}

func TestJsonParser_ParseToArray(t *testing.T) {
	json := `{"one":"a", "two":{"three": "four"}, "": "empty"}`
	assertDecodes(t, json, ParseToAssoc, json)
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func TestJsonParser_FileValidUTF8(t *testing.T) {
	s, err := Parse(readFixture(t, "validutf8.json"), ValidateUTF8Encoding)
	if err != nil {
		t.Fatalf("validutf8.json file should pass validation: %v", err)
	}
	if s != "abcdé" {
		t.Errorf("got %q", s)
	}
}

func TestJsonParser_FileNonValidUTF8(t *testing.T) {
	err := parseError(t, readFixture(t, "nonvalidutf8.json"), ValidateUTF8Encoding, "nonvalidutf8.json file should not pass validation.")
	if _, ok := errors.AsType[*InvalidEncodingError](err); !ok {
		t.Fatalf("got %T, want an InvalidEncodingException", err)
	}
	assertContains(t, err, "Non-UTF8 character found")
}

func TestJsonParser_FileWithBOM(t *testing.T) {
	err := parseError(t, readFixture(t, "bom.json"), 0, "BOM should be detected")
	assertContains(t, err, "BOM detected")
}

func TestJsonParser_LongString(t *testing.T) {
	json := `{"k":"` + strings.Repeat(`a\n`, 10000) + `"}`
	assertDecodes(t, json, 0, json)
}

func TestJsonParser_ParseNoneTerminatingString(t *testing.T) {
	v, err := Parse(`{"`, 0)
	if err == nil {
		if v != "" {
			t.Errorf("got %v", v)
		}

		return
	}
	assertContains(t, err, "Invalid string, it appears you forgot to terminate a string")
}

func TestJsonParser_ParsesJsonStringWithComments(t *testing.T) {
	cases := [][2]string{
		{`["a", "sdfsd"]//test`, `["a", "sdfsd"]`},
		{`[/*"a",*/ "sdfsd"]//`, `["sdfsd"]`},
		{`["a", "sdf//sd"]/**/`, `["a", "sdf//sd"]`},
		{`/**/{/*"":*/"g":"foo"}`, `{"g":"foo"}`},
		{`{"a":"b"}//, "b":"c"}`, `{"a":"b"}`},
		{readFixture(t, "with-comments.json"), readFixture(t, "without-comments.json")},
	}
	for _, c := range cases {
		if Lint(c[0], 0) == nil {
			t.Errorf("lint(%q) found no error", c[0])
		}
		if err := Lint(c[0], AllowComments); err != nil {
			t.Errorf("lint(%q, ALLOW_COMMENTS): %v", c[0], err)
		}
		assertDecodes(t, c[0], AllowComments, c[1])
	}
}

func assertInvalidUTF8(t *testing.T, input, reason string) {
	t.Helper()
	err := ValidateUTF8(input)
	if _, ok := errors.AsType[*InvalidEncodingError](err); !ok {
		t.Fatalf("%q should not pass validation (got %v)", input, err)
	}
	if !strings.Contains(err.Error(), "Non-UTF8 character found") || !strings.Contains(err.Error(), reason) {
		t.Errorf("%q: message %q does not contain %q", input, err.Error(), reason)
	}
}

func TestUtf8Validator_ValidUtf8(t *testing.T) {
	for _, s := range []string{"", "abcdé", "euro sign: € and more", "musical clef: 𝄞", "line 1\nline 2\nline 3"} {
		if err := ValidateUTF8(s); err != nil {
			t.Errorf("Valid UTF-8 should pass validation: %v", err)
		}
	}
}

func TestUtf8Validator_AllErrorTypes(t *testing.T) {
	assertInvalidUTF8(t, "\"abcd\xe9\"", " which is not a continuation octet.")
	assertInvalidUTF8(t, "\"abcd\xe9", ", end of string was found instead of a continuation octet.")
	for _, forbidden := range []byte{192, 193, 245, 255} {
		assertInvalidUTF8(t, "\"abcd\xe9"+string([]byte{forbidden})+"\"", " which is one of the four forbidden values (C0, C1, F5, FF).")
		assertInvalidUTF8(t, "\"abcd"+string([]byte{forbidden})+"\"", " which is one of the four forbidden values (C0, C1, F5, FF).")
	}
	assertInvalidUTF8(t, "\"abcd\x81\"", " which is a continuation octet.")
	for i := 160; i <= 191; i++ {
		assertInvalidUTF8(t, "\"abcd\xed"+string([]byte{byte(i)})+"\x81\"", " which is into the forbidden range of surrogate pairs.")
		assertInvalidUTF8(t, "\"abcd\xed"+string([]byte{byte(i)})+"\"", " which is into the forbidden range of surrogate pairs.")
	}
	for i := 246; i < 255; i++ { // 245 and 255 already forbidden
		assertInvalidUTF8(t, "\"abcd"+string([]byte{byte(i)})+"\x81\x81\x81\"", " which is invalid.")
		assertInvalidUTF8(t, "\"abcd"+string([]byte{byte(i)})+"\"", " which is invalid.")
		assertInvalidUTF8(t, "\"abcd\xc3"+string([]byte{byte(i)})+"\"", " which is not a continuation octet.")
	}
}

func TestUtf8Validator_ExceptionExposesPositionDetails(t *testing.T) {
	err := ValidateUTF8("ab\xff")
	iee, ok := errors.AsType[*InvalidEncodingError](err)
	if !ok {
		t.Fatalf("Invalid UTF-8 should not pass validation (got %v)", err)
	}
	if iee.Details.Encoding.CurrentOctet != 255 || iee.Details.Encoding.Line != 1 || iee.Key() != "255" {
		t.Errorf("details %+v, key %q", iee.Details.Encoding, iee.Key())
	}
}
