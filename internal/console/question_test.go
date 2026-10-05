// Ports Tests/Question/QuestionTest.php, ChoiceQuestionTest.php and
// ConfirmationQuestionTest.php (symfony/console) and Composer's
// tests/Composer/Test/Question/StrictConfirmationQuestionTest.php.

package console

import (
	"errors"
	"reflect"
	"regexp"
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

func wantConsoleError(t *testing.T, err error, kind Kind, message string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("want %s %q, got %v", kindClass[kind], message, err)
	}
	if e.Kind != kind || e.Message != message {
		t.Fatalf("want %s %q, got %s %q", kindClass[kind], message, kindClass[e.Kind], e.Message)
	}
}

func TestQuestion_GetQuestion(t *testing.T) {
	if got := NewQuestion("Test question", nil).Question(); got != "Test question" {
		t.Fatalf("got %q", got)
	}
}

func TestQuestion_GetDefault(t *testing.T) {
	if got := NewQuestion("Test question", "Default value").Default(); got != "Default value" {
		t.Fatalf("got %v", got)
	}
}

func TestQuestion_GetDefaultDefault(t *testing.T) {
	if got := NewQuestion("Test question", nil).Default(); got != nil {
		t.Fatalf("got %v", got)
	}
}

func TestQuestion_IsSetHidden(t *testing.T) {
	for _, hidden := range []bool{true, false} {
		q := NewQuestion("Test question", nil)
		if err := q.SetHidden(hidden); err != nil {
			t.Fatal(err)
		}
		if q.IsHidden() != hidden {
			t.Fatalf("hidden %v", hidden)
		}
	}
}

func TestQuestion_IsHiddenDefault(t *testing.T) {
	if NewQuestion("Test question", nil).IsHidden() {
		t.Fatal("hidden by default")
	}
}

func TestQuestion_SetHiddenWithAutocompleterCallback(t *testing.T) {
	q := NewQuestion("Test question", nil)
	_ = q.SetAutocompleterCallback(func(string) []string { return []string{} })
	wantConsoleError(t, q.SetHidden(true), KindLogic, "A hidden question cannot use the autocompleter.")
}

func TestQuestion_SetHiddenWithNoAutocompleterCallback(t *testing.T) {
	q := NewQuestion("Test question", nil)
	_ = q.SetAutocompleterCallback(func(string) []string { return []string{} })
	_ = q.SetAutocompleterCallback(nil)
	if err := q.SetHidden(true); err != nil {
		t.Fatal(err)
	}
}

func TestQuestion_IsSetHiddenFallback(t *testing.T) {
	for _, fallback := range []bool{true, false} {
		q := NewQuestion("Test question", nil)
		q.SetHiddenFallback(fallback)
		if q.IsHiddenFallback() != fallback {
			t.Fatalf("fallback %v", fallback)
		}
	}
}

func TestQuestion_IsHiddenFallbackDefault(t *testing.T) {
	if !NewQuestion("Test question", nil).IsHiddenFallback() {
		t.Fatal("no hidden fallback by default")
	}
}

func TestQuestion_GetSetAutocompleterValues(t *testing.T) {
	cases := map[string]struct {
		set  func(q *Question) error
		want []string
	}{
		"array": {
			func(q *Question) error { return q.SetAutocompleterValues(php.ListOf("a", "b", "c", "d")) },
			[]string{"a", "b", "c", "d"},
		},
		"associative array": {
			func(q *Question) error { return q.SetAutocompleterValues(php.ArrayOf("a", "c", "b", "d")) },
			[]string{"a", "b", "c", "d"},
		},
		"iterator": {
			func(q *Question) error { return q.SetAutocompleterSeq(slices.Values([]string{"a", "b", "c", "d"})) },
			[]string{"a", "b", "c", "d"},
		},
		"null": {
			func(q *Question) error { return q.SetAutocompleterValues(nil) },
			nil,
		},
	}
	for name, c := range cases {
		q := NewQuestion("Test question", nil)
		if err := c.set(q); err != nil {
			t.Fatal(err)
		}
		if got := q.AutocompleterValues(); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %#v, want %#v", name, got, c.want)
		}
	}
}

func TestQuestion_SetAutocompleterValuesWithTraversable(t *testing.T) {
	calls := map[string]int{}
	seq := func(v string) func(yield func(string) bool) {
		return func(yield func(string) bool) {
			calls[v]++
			yield(v)
		}
	}
	q1 := NewQuestion("Test question 1", nil)
	_ = q1.SetAutocompleterSeq(seq("Potato"))
	q2 := NewQuestion("Test question 2", nil)
	_ = q2.SetAutocompleterSeq(seq("Carrot"))

	// Call multiple times to verify that Traversable result is cached, and
	// that there is no crosstalk between cached copies.
	for range 2 {
		if got := q1.AutocompleterValues(); !reflect.DeepEqual(got, []string{"Potato"}) {
			t.Fatalf("q1 got %v", got)
		}
		if got := q2.AutocompleterValues(); !reflect.DeepEqual(got, []string{"Carrot"}) {
			t.Fatalf("q2 got %v", got)
		}
	}
	if calls["Potato"] != 1 || calls["Carrot"] != 1 {
		t.Fatalf("iterated %v", calls)
	}
}

func TestQuestion_GetAutocompleterValuesDefault(t *testing.T) {
	if got := NewQuestion("Test question", nil).AutocompleterValues(); got != nil {
		t.Fatalf("got %v", got)
	}
}

func TestQuestion_GetSetAutocompleterCallback(t *testing.T) {
	q := NewQuestion("Test question", nil)
	_ = q.SetAutocompleterCallback(func(string) []string { return []string{"x"} })
	if cb := q.AutocompleterCallback(); cb == nil || cb("")[0] != "x" {
		t.Fatal("callback not kept")
	}
}

func TestQuestion_GetAutocompleterCallbackDefault(t *testing.T) {
	if NewQuestion("Test question", nil).AutocompleterCallback() != nil {
		t.Fatal("callback set by default")
	}
}

func TestQuestion_SetAutocompleterCallbackWhenHidden(t *testing.T) {
	q := NewQuestion("Test question", nil)
	_ = q.SetHidden(true)
	wantConsoleError(t, q.SetAutocompleterCallback(func(string) []string { return []string{} }), KindLogic, "A hidden question cannot use the autocompleter.")
}

func TestQuestion_SetAutocompleterCallbackWhenNotHidden(t *testing.T) {
	q := NewQuestion("Test question", nil)
	_ = q.SetHidden(true)
	_ = q.SetHidden(false)
	if err := q.SetAutocompleterCallback(func(string) []string { return []string{} }); err != nil {
		t.Fatal(err)
	}
}

func TestQuestion_GetSetValidator(t *testing.T) {
	q := NewQuestion("Test question", nil)
	q.SetValidator(func(input any) (any, error) { return input, nil })
	if q.Validator() == nil {
		t.Fatal("validator not kept")
	}
	q.SetValidator(nil)
	if q.Validator() != nil {
		t.Fatal("validator not cleared")
	}
}

func TestQuestion_GetValidatorDefault(t *testing.T) {
	if NewQuestion("Test question", nil).Validator() != nil {
		t.Fatal("validator set by default")
	}
}

func TestQuestion_GetSetMaxAttempts(t *testing.T) {
	for _, attempts := range []int{1, 5} {
		q := NewQuestion("Test question", nil)
		if err := q.SetMaxAttempts(attempts); err != nil {
			t.Fatal(err)
		}
		if q.MaxAttempts() != attempts {
			t.Fatalf("got %d", q.MaxAttempts())
		}
	}
	q := NewQuestion("Test question", nil)
	_ = q.SetMaxAttempts(3)
	q.ResetMaxAttempts()
	if q.MaxAttempts() != 0 {
		t.Fatalf("null attempts: got %d", q.MaxAttempts())
	}
}

func TestQuestion_SetMaxAttemptsInvalid(t *testing.T) {
	for _, attempts := range []int{0, -1} {
		wantConsoleError(t, NewQuestion("Test question", nil).SetMaxAttempts(attempts), KindInvalidArgument, "Maximum number of attempts must be a positive value.")
	}
}

func TestQuestion_GetMaxAttemptsDefault(t *testing.T) {
	if NewQuestion("Test question", nil).MaxAttempts() != 0 {
		t.Fatal("attempts limited by default")
	}
}

func TestQuestion_GetSetNormalizer(t *testing.T) {
	q := NewQuestion("Test question", nil)
	q.SetNormalizer(func(input any) any { return input })
	if q.Normalizer() == nil {
		t.Fatal("normalizer not kept")
	}
}

func TestQuestion_GetNormalizerDefault(t *testing.T) {
	if NewQuestion("Test question", nil).Normalizer() != nil {
		t.Fatal("normalizer set by default")
	}
}

func TestQuestion_SetMultiline(t *testing.T) {
	for _, multiline := range []bool{true, false} {
		q := NewQuestion("Test question", nil)
		if q.SetMultiline(multiline) != q || q.IsMultiline() != multiline {
			t.Fatalf("multiline %v", multiline)
		}
	}
}

func TestQuestion_IsMultilineDefault(t *testing.T) {
	if NewQuestion("Test question", nil).IsMultiline() {
		t.Fatal("multiline by default")
	}
}

// phpList returns the values of a *php.Array answer.
func phpList(v any) []any {
	if a, ok := v.(*php.Array); ok {
		return a.Values()
	}

	return nil
}

func TestChoiceQuestion_SelectUseCases(t *testing.T) {
	cases := []struct {
		multiselect bool
		answers     []any
		expected    any
		message     string
		def         any
	}{
		{false, []any{"First response", "First response ", " First response", " First response "}, "First response", "When passed single answer on singleSelect, the defaultValidator must return this answer as a string", nil},
		{true, []any{"First response", "First response ", " First response", " First response "}, []any{"First response"}, "When passed single answer on MultiSelect, the defaultValidator must return this answer as an array", nil},
		{true, []any{"First response,Second response", " First response , Second response "}, []any{"First response", "Second response"}, "When passed multiple answers on MultiSelect, the defaultValidator must return these answers as an array", nil},
		{false, []any{nil}, nil, "When used null as default single answer on singleSelect, the defaultValidator must return this answer as null", nil},
		{false, []any{"First response"}, "First response", "When used a string as default single answer on singleSelect, the defaultValidator must return this answer as a string", "First response"},
		{false, []any{0}, "First response", "When passed single answer using choice's key, the defaultValidator must return the choice value", nil},
		{true, []any{"0, 2"}, []any{"First response", "Third response"}, "When passed multiple answers using choices' key, the defaultValidator must return the choice values in an array", nil},
	}
	for _, c := range cases {
		q, err := NewChoiceQuestion("A question", php.ListOf("First response", "Second response", "Third response", "Fourth response", nil), c.def)
		if err != nil {
			t.Fatal(err)
		}
		q.SetMultiselect(c.multiselect)
		for _, answer := range c.answers {
			got, err := q.Validator()(answer)
			if err != nil {
				t.Fatalf("%s: %v", c.message, err)
			}
			if list, ok := c.expected.([]any); ok {
				if !reflect.DeepEqual(phpList(got), list) {
					t.Errorf("%s: got %#v", c.message, got)
				}
			} else if got != c.expected {
				t.Errorf("%s: got %#v", c.message, got)
			}
		}
	}
}

func TestChoiceQuestion_NonTrimmable(t *testing.T) {
	q, _ := NewChoiceQuestion("A question", php.ListOf("First response ", " Second response", "  Third response  "), nil)
	q.SetTrimmable(false)

	if got, err := q.Validator()("  Third response  "); err != nil || got != "  Third response  " {
		t.Fatalf("got %#v, %v", got, err)
	}

	q.SetMultiselect(true)

	got, err := q.Validator()("First response , Second response")
	if err != nil || !reflect.DeepEqual(phpList(got), []any{"First response ", " Second response"}) {
		t.Fatalf("got %#v, %v", got, err)
	}
}

// The "string object" choices of the PHP test (objects with __toString)
// have no equivalent in the value model and are left out, as is
// testSelectWithNonStringChoices.
func TestChoiceQuestion_SelectAssociativeChoices(t *testing.T) {
	cases := map[string][2]string{
		`select "0" choice by key`:          {"0", "0"},
		`select "0" choice by value`:        {"First choice", "0"},
		"select by key":                     {"foo", "foo"},
		"select by value":                   {"Foo", "foo"},
		"select by key, with numeric key":   {"99", "99"},
		"select by value, with numeric key": {"N°99", "99"},
	}
	for name, c := range cases {
		q, _ := NewChoiceQuestion("A question", php.ArrayOf("0", "First choice", "foo", "Foo", "99", "N°99"), nil)
		got, err := q.Validator()(c[0])
		if err != nil || got != c[1] {
			t.Errorf("%s: got %#v, %v", name, got, err)
		}
	}
}

func TestConfirmationQuestion_DefaultRegexUsecases(t *testing.T) {
	cases := []struct {
		def      bool
		answers  []string
		expected bool
		message  string
	}{
		{true, []string{"y", "Y", "yes", "YES", "yEs", ""}, true, "When default is true, the normalizer must return true for %q"},
		{true, []string{"n", "N", "no", "NO", "nO", "foo", "1", "0"}, false, "When default is true, the normalizer must return false for %q"},
		{false, []string{"y", "Y", "yes", "YES", "yEs"}, true, "When default is false, the normalizer must return true for %q"},
		{false, []string{"n", "N", "no", "NO", "nO", "foo", "1", "0", ""}, false, "When default is false, the normalizer must return false for %q"},
	}
	for _, c := range cases {
		sut := NewConfirmationQuestion("A question", c.def, nil)
		for _, answer := range c.answers {
			if got := sut.Normalizer()(answer); got != c.expected {
				t.Errorf(c.message, answer)
			}
		}
	}
}

func newQuestionInput(stream string) Input {
	in, _ := NewArrayInput(nil, nil)
	in.SetStream(getInputStream(stream))

	return in
}

func TestStrictConfirmationQuestion_AskConfirmationBadAnswer(t *testing.T) {
	for _, answer := range []string{"not correct", "no more", "yes please", "yellow"} {
		q := NewStrictConfirmationQuestion("Do you like French fries?", true, nil, nil)
		_ = q.SetMaxAttempts(1)
		_, err := NewQuestionHelper().Ask(newQuestionInput(answer+"\n"), createOutputInterface(), q)
		wantConsoleError(t, err, KindInvalidArgument, "Please answer yes, y, no, or n.")
	}
}

func TestStrictConfirmationQuestion_AskConfirmation(t *testing.T) {
	cases := []struct {
		answer   string
		expected bool
		def      bool
	}{
		{"", true, true},
		{"", false, false},
		{"y", true, true},
		{"yes", true, true},
		{"n", false, true},
		{"no", false, true},
	}
	for _, c := range cases {
		q := NewStrictConfirmationQuestion("Do you like French fries?", c.def, nil, nil)
		got, err := NewQuestionHelper().Ask(newQuestionInput(c.answer+"\n"), createOutputInterface(), q)
		if err != nil || got != c.expected {
			t.Errorf("%q: got %#v, %v", c.answer, got, err)
		}
	}
}

func TestStrictConfirmationQuestion_AskConfirmationWithCustomTrueAndFalseAnswer(t *testing.T) {
	q := NewStrictConfirmationQuestion("Do you like French fries?", false, regexp.MustCompile(`(?i)^ja\n?$`), regexp.MustCompile(`(?i)^nein\n?$`))

	if got, err := NewQuestionHelper().Ask(newQuestionInput("ja\n"), createOutputInterface(), q); err != nil || got != true {
		t.Fatalf("ja: got %#v, %v", got, err)
	}
	if got, err := NewQuestionHelper().Ask(newQuestionInput("nein\n"), createOutputInterface(), q); err != nil || got != false {
		t.Fatalf("nein: got %#v, %v", got, err)
	}
}
