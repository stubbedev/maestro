package php

import (
	"errors"
	"reflect"
	"testing"
)

// Ports tests/PregTests/*Test.php of composer/pcre 2.3.2 (MIT, see
// testdata/LICENSE-composer), the version Composer 2.10.3 locks. Tests of
// PHP type juggling (subjects that are ints, arrays or null) have no Go
// counterpart: the Go API takes strings.

// expectPcreError checks the PcreException message of a failed call, as
// BaseTestCase::expectPcreException does.
func expectPcreError(t *testing.T, err error, function, pattern, msg string) {
	t.Helper()
	var pe *PcreError
	if !errors.As(err, &pe) {
		t.Fatalf("got %v, want a PcreError", err)
	}
	if want := function + "(): failed executing \"" + pattern + "\": " + msg; pe.Error() != want {
		t.Errorf("got %q, want %q", pe.Error(), want)
	}
}

// expectPcreWarning checks the warning a pattern that does not compile
// triggers, as BaseTestCase::expectPcreWarning does.
func expectPcreWarning(t *testing.T, err error, function string) {
	t.Helper()
	var pe *PcreError
	if !errors.As(err, &pe) {
		t.Fatalf("got %v, want a PcreError", err)
	}
	if want := function + "(): No ending matching delimiter '}' found"; pe.Warning != want {
		t.Errorf("got warning %q, want %q", pe.Warning, want)
	}
}

func assertArray(t *testing.T, got *Array, want *Array) {
	t.Helper()
	if !StrictEquals(got, want) {
		t.Errorf("got %s, want %s", VarExport(got), VarExport(want))
	}
}

func TestPregMatch_Success(t *testing.T) {
	m, err := PregMatch(`{(?P<m>[io])}`, "abcdefghijklmnopqrstuvwxyz")
	if err != nil || m == nil {
		t.Fatal(m, err)
	}
	assertArray(t, m.Array(PregUnmatchedAsNull), ArrayOf(0, "i", "m", "i", 1, "i"))
}

func TestPregMatch_SuccessStrictGroups(t *testing.T) {
	m, err := PregMatchStrictGroups(`{(?<m>\d)(?<matched>a)}`, "3a")
	if err != nil || m == nil {
		t.Fatal(m, err)
	}
	assertArray(t, m.Array(PregUnmatchedAsNull), ArrayOf(0, "3a", "m", "3", 1, "3", "matched", "a", 2, "a"))
}

func TestPregMatch_FailStrictGroups(t *testing.T) {
	_, err := PregMatchStrictGroups(`{(?<m>\d)(?<unmatched>b)?}`, "123")
	if _, ok := errors.AsType[*UnexpectedNullMatchError](err); !ok {
		t.Fatalf("got %v", err)
	}
	if want := `Pattern "{(?<m>\d)(?<unmatched>b)?}" had an unexpected unmatched group "unmatched", make sure the pattern always matches or use match() instead.`; err.Error() != want {
		t.Errorf("got %q", err.Error())
	}
}

func TestPregMatch_SuccessNoRef(t *testing.T) {
	if ok, err := PregIsMatch(`{(?P<m>[io])}`, "abcdefghijklmnopqrstuvwxyz"); !ok || err != nil {
		t.Fatal(ok, err)
	}
}

func TestPregMatch_Failure(t *testing.T) {
	m, err := PregMatch(`{abc}`, "def")
	if m != nil || err != nil {
		t.Fatal(m, err)
	}
	re := MustCompile(`{abc}`)
	n, a, err := re.MatchArray("def", PregUnmatchedAsNull, 0)
	if n != 0 || err != nil {
		t.Fatal(n, err)
	}
	assertArray(t, a, NewArray())
}

func TestPregMatch_BadPatternThrowsIfWarningsAreNotThrowing(t *testing.T) {
	_, err := PregMatch(`{(?P<m>[io])`, "abcdefghijklmnopqrstuvwxyz")
	expectPcreError(t, err, "preg_match", `{(?P<m>[io])`, "Internal error")
}

func TestPregMatch_BadPatternTriggersWarningByDefault(t *testing.T) {
	_, err := PregMatch(`{(?P<m>[io])`, "abcdefghijklmnopqrstuvwxyz")
	expectPcreWarning(t, err, "preg_match")
}

func TestPregMatch_ThrowsIfEngineErrors(t *testing.T) {
	_, err := PregMatch(`/(?:\D+|<\d+>)*[!?]/`, "foobar foobar foobar")
	expectPcreError(t, err, "preg_match", `/(?:\D+|<\d+>)*[!?]/`, "Backtrack limit exhausted")
}

func TestPregMatchAll_Success(t *testing.T) {
	re := MustCompile(`{[aei]}`)
	n, a, err := re.MatchAllArray("abcdefghijklmnopqrstuvwxyz", PregUnmatchedAsNull, 0)
	if n != 3 || err != nil {
		t.Fatal(n, err)
	}
	assertArray(t, a, ListOf(ListOf("a", "e", "i")))
	ms, err := PregMatchAll(`{[aei]}`, "abcdefghijklmnopqrstuvwxyz")
	if len(ms) != 3 || err != nil || ms[2].Get(0) != "i" {
		t.Fatal(ms, err)
	}
}

func TestPregMatchAll_Failure(t *testing.T) {
	n, a, err := MustCompile(`{abc}`).MatchAllArray("def", PregUnmatchedAsNull, 0)
	if n != 0 || err != nil {
		t.Fatal(n, err)
	}
	assertArray(t, a, ListOf(NewArray()))
}

func TestPregMatchAll_SuccessStrictGroups(t *testing.T) {
	ms, err := PregMatchAllStrictGroups(`{(?<m>\d)(?<matched>a)}`, "3a")
	if len(ms) != 1 || err != nil {
		t.Fatal(ms, err)
	}
	_, a, _ := MustCompile(`{(?<m>\d)(?<matched>a)}`).MatchAllArray("3a", PregUnmatchedAsNull, 0)
	assertArray(t, a, ArrayOf(0, ListOf("3a"), "m", ListOf("3"), 1, ListOf("3"), "matched", ListOf("a"), 2, ListOf("a")))
}

func TestPregMatchAll_FailStrictGroups(t *testing.T) {
	_, err := PregMatchAllStrictGroups(`{(?<m>\d)(?<unmatched>b)?}`, "123")
	if want := `Pattern "{(?<m>\d)(?<unmatched>b)?}" had an unexpected unmatched group "unmatched", make sure the pattern always matches or use matchAll() instead.`; err == nil || err.Error() != want {
		t.Errorf("got %v", err)
	}
}

func TestPregMatchAll_BadPatternThrowsIfWarningsAreNotThrowing(t *testing.T) {
	_, err := PregMatchAll(`{[aei]`, "abcdefghijklmnopqrstuvwxyz")
	expectPcreError(t, err, "preg_match_all", `{[aei]`, "Internal error")
	expectPcreWarning(t, err, "preg_match_all")
}

func TestPregIsMatchWithOffsets_Success(t *testing.T) {
	m, err := PregMatch(`{(?P<m>[io])}`, "abcdefghijklmnopqrstuvwxyz")
	if err != nil || m == nil {
		t.Fatal(m, err)
	}
	assertArray(t, m.Array(PregUnmatchedAsNull|PregOffsetCapture), ArrayOf(0, ListOf("i", 8), "m", ListOf("i", 8), 1, ListOf("i", 8)))
}

func TestPregIsMatchAllWithOffsets_Success(t *testing.T) {
	_, a, err := MustCompile(`{[aei]}`).MatchAllArray("abcdefghijklmnopqrstuvwxyz", PregUnmatchedAsNull|PregOffsetCapture, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertArray(t, a, ListOf(ListOf(ListOf("a", 0), ListOf("e", 4), ListOf("i", 8))))
}

func TestPregReplace_Success(t *testing.T) {
	res, n, err := PregReplace(`{(?P<m>d)}`, "e", "abcd", -1)
	if res != "abce" || n != 1 || err != nil {
		t.Fatal(res, n, err)
	}
}

func TestPregReplace_Failure(t *testing.T) {
	res, n, err := PregReplace(`{abc}`, "123", "def", -1)
	if res != "def" || n != 0 || err != nil {
		t.Fatal(res, n, err)
	}
}

func TestPregReplace_BadPatternThrowsIfWarningsAreNotThrowing(t *testing.T) {
	_, _, err := PregReplace(`{(?P<m>d)`, "e", "abcd", -1)
	expectPcreError(t, err, "preg_replace", `{(?P<m>d)`, "Internal error")
	expectPcreWarning(t, err, "preg_replace")
}

func TestPregReplaceCallback_Success(t *testing.T) {
	res, n, err := PregReplaceCallback(`{(?P<m>d)}`, "abcd", func(m *Match) string { return "(" + m.Get(0) + ")" }, -1)
	if res != "abc(d)" || n != 1 || err != nil {
		t.Fatal(res, n, err)
	}
}

func TestPregReplaceCallback_SuccessWithOffset(t *testing.T) {
	res, n, err := PregReplaceCallback(`{(?P<m>d)}`, "abcd", func(m *Match) string {
		v, _ := m.Array(PregOffsetCapture).Get(0)
		s, _ := v.(*Array).Get(0)
		return "(" + s.(string) + ")"
	}, -1)
	if res != "abc(d)" || n != 1 || err != nil {
		t.Fatal(res, n, err)
	}
}

func TestPregReplaceCallback_Failure(t *testing.T) {
	res, n, err := PregReplaceCallback(`{abc}`, "def", func(m *Match) string { return "(" + m.Get(0) + ")" }, -1)
	if res != "def" || n != 0 || err != nil {
		t.Fatal(res, n, err)
	}
}

func TestPregReplaceCallback_BadPatternThrowsIfWarningsAreNotThrowing(t *testing.T) {
	_, _, err := PregReplaceCallback(`{(?P<m>d)`, "abcd", func(m *Match) string { return "" }, -1)
	expectPcreError(t, err, "preg_replace_callback", `{(?P<m>d)`, "Internal error")
	expectPcreWarning(t, err, "preg_replace_callback")
}

func TestPregGrep_Success(t *testing.T) {
	a, err := PregGrep(`{[bc]}`, ListOf("a", "b", "c"), 0)
	if err != nil {
		t.Fatal(err)
	}
	assertArray(t, a, ArrayOf(1, "b", 2, "c"))
}

func TestPregGrep_Failure(t *testing.T) {
	a, err := PregGrep(`{[de]}`, ListOf("a", "b", "c"), 0)
	if err != nil {
		t.Fatal(err)
	}
	assertArray(t, a, NewArray())
}

func TestPregGrep_BadPatternThrowsIfWarningsAreNotThrowing(t *testing.T) {
	_, err := PregGrep(`{[de]`, ListOf("a", "b", "c"), 0)
	expectPcreError(t, err, "preg_grep", `{[de]`, "Internal error")
	expectPcreWarning(t, err, "preg_grep")
}

func TestPregSplit_Success(t *testing.T) {
	parts, err := PregSplit(`{[\s,]+}`, "a, b, c", -1, 0)
	if err != nil || !reflect.DeepEqual(parts, []string{"a", "b", "c"}) {
		t.Fatal(parts, err)
	}
}

func TestPregSplit_Failure(t *testing.T) {
	parts, err := PregSplit(`{[\s,]+}`, "abc", -1, 0)
	if err != nil || !reflect.DeepEqual(parts, []string{"abc"}) {
		t.Fatal(parts, err)
	}
}

func TestPregSplit_BadPatternThrowsIfWarningsAreNotThrowing(t *testing.T) {
	_, err := PregSplit(`{[\s,]+`, "a, b, c", -1, 0)
	expectPcreError(t, err, "preg_split", `{[\s,]+`, "Internal error")
	expectPcreWarning(t, err, "preg_split")
}

func TestPregSplitWithOffsets_Success(t *testing.T) {
	pieces, err := MustCompile(`{[\s,]+}`).SplitWithOffsets("a, b, c", -1, 0)
	if err != nil || !reflect.DeepEqual(pieces, []SplitPiece{{"a", 0}, {"b", 3}, {"c", 6}}) {
		t.Fatal(pieces, err)
	}
	pieces, err = MustCompile(`{[\s,]+}`).SplitWithOffsets("abc", -1, 0)
	if err != nil || !reflect.DeepEqual(pieces, []SplitPiece{{"abc", 0}}) {
		t.Fatal(pieces, err)
	}
}

// The tests below cover the Go API beyond composer/pcre's suite.

func TestMatchAccessors(t *testing.T) {
	m, err := PregMatch(`{(?<year>\d{4})-(?<month>\d\d)(?:-(?<day>\d\d))?(x)?}`, "on 2024-05")
	if err != nil || m == nil {
		t.Fatal(m, err)
	}
	if v, ok := m.Named("year"); v != "2024" || !ok {
		t.Errorf("year: %q %v", v, ok)
	}
	if v, ok := m.Named("day"); v != "" || ok {
		t.Errorf("day: %q %v", v, ok)
	}
	if _, ok := m.Named("nope"); ok {
		t.Error("unknown name matched")
	}
	if m.Offset(0) != 3 || m.Offset(3) != -1 || m.Groups() != 5 || m.Count() != 3 {
		t.Errorf("offsets %d %d, groups %d, count %d", m.Offset(0), m.Offset(3), m.Groups(), m.Count())
	}
	assertArray(t, m.Array(0), ArrayOf(0, "2024-05", "year", "2024", 1, "2024", "month", "05", 2, "05"))
	assertArray(t, m.Array(PregUnmatchedAsNull), ArrayOf(0, "2024-05", "year", "2024", 1, "2024", "month", "05", 2, "05", "day", nil, 3, nil, 4, nil))
}

func TestPregQuote(t *testing.T) {
	for _, c := range []struct{ in, delim, want string }{
		{"", "", ""},
		{"abc", "", "abc"},
		{"vendor/package", "", "vendor/package"},
		{"vendor/package", "/", `vendor\/package`},
		{`.\+*?[^]$(){}=!<>|:-#`, "", `\.\\\+\*\?\[\^\]\$\(\)\{\}\=\!\<\>\|\:\-\#`},
		{"a\x00b", "", `a\000b`},
		{"a~b", "~x", `a\~b`},
	} {
		if got := PregQuote(c.in, c.delim); got != c.want {
			t.Errorf("PregQuote(%q, %q) = %q, want %q", c.in, c.delim, got, c.want)
		}
	}
}

func TestCompileErrors(t *testing.T) {
	for pattern, want := range map[string]string{
		"":        "Empty regular expression",
		"  ":      "Empty regular expression",
		"abc":     "Delimiter must not be alphanumeric, backslash, or NUL byte",
		`\abc\`:   "Delimiter must not be alphanumeric, backslash, or NUL byte",
		"/abc":    "No ending delimiter '/' found",
		"{abc":    "No ending matching delimiter '}' found",
		"/abc/q":  "Unknown modifier 'q'",
		"/abc/e":  "Unknown modifier 'e'",
		"/a/\x00": "NUL byte is not a valid modifier",
		// PHP prints a byte above 0x7f as the raw byte, not as a character.
		"/abc/\xe9": "Unknown modifier '\xe9'",
		"\xa7abc":   "No ending delimiter '\xa7' found",
		"/(/":       "Compilation failed: missing closing parenthesis at offset 1",
	} {
		_, err := Compile(pattern)
		var pe *PatternError
		if !errors.As(err, &pe) || pe.Error() != want || pe.Pattern != pattern {
			t.Errorf("Compile(%q): got %v, want %q", pattern, err, want)
		}
	}
}

func TestPcreErrorCodes(t *testing.T) {
	re := MustCompile(`/a/u`)
	_, err := re.Match("\xff")
	var pe *PcreError
	if !errors.As(err, &pe) || pe.Code != PregBadUTF8Error || pe.Function != "preg_match" {
		t.Fatalf("got %v", err)
	}
	if _, err := re.MatchAt("a", 5); !errors.As(err, &pe) || pe.Code != PregInternalError {
		t.Errorf("offset past the end: %v", err)
	}
	if m, err := re.MatchAt("ba", -1); err != nil || m == nil || m.Offset(0) != 1 {
		t.Errorf("negative offset: %v %v", m, err)
	}
}

func TestRegexpConcurrent(t *testing.T) {
	re := MustCompile(`{(?<x>[a-z]+)(\d+)?}i`)
	done := make(chan bool)
	for range 8 {
		go func() {
			for range 200 {
				ms, err := re.MatchAll("abc123 def x9")
				if err != nil || len(ms) != 3 || ms[2].Get(2) != "9" {
					t.Error(ms, err)
				}
			}
			done <- true
		}()
	}
	for range 8 {
		<-done
	}
}

// TestSprintfUnknownSpecifierRawByte checks that sprintf names an unknown
// specifier above 0x7f by its raw byte, as PHP's ValueError does.
func TestSprintfUnknownSpecifierRawByte(t *testing.T) {
	_, err := Sprintf("%\xe9", 1)
	if want := "Unknown format specifier \"\xe9\""; err == nil || err.Error() != want {
		t.Errorf("Sprintf(%%\\xe9): got %v, want %q", err, want)
	}
}
