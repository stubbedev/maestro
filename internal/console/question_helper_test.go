// Ports Tests/Helper/AbstractQuestionHelperTestCase.php,
// QuestionHelperTest.php and SymfonyQuestionHelperTest.php
// (symfony/console).
//
// The autocomplete tests need stty in PHP and are skipped without a
// terminal; here stty is replaced by a no-op so they always run.

package console

import (
	"bytes"
	"errors"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

func getInputStream(input string) *strings.Reader { return strings.NewReader(input) }

func createOutputInterface() *StreamOutput {
	return NewStreamOutput(&bytes.Buffer{}, VerbosityNormal, new(false), nil)
}

func outputContents(out *StreamOutput) string {
	b, _ := out.Stream().(*bytes.Buffer)

	return b.String()
}

// createStreamableInputInterfaceMock returns an input reading from stream
// (stdin when nil).
func createStreamableInputInterfaceMock(stream *strings.Reader, interactive bool) Input {
	in, _ := NewArrayInput(nil, nil)
	if stream != nil {
		in.SetStream(stream)
	}
	in.SetInteractive(interactive)

	return in
}

// fakeStty makes stty available and a no-op.
func fakeStty(t *testing.T) {
	t.Helper()
	avail, run := sttyAvailable, runStty
	sttyAvailable = func() bool { return true }
	runStty = func(...string) string { return "" }
	t.Cleanup(func() { sttyAvailable, runStty = avail, run })
}

func newHelperWithFormatter() *QuestionHelper {
	h := NewQuestionHelper()
	h.SetHelperSet(NewHelperSet(&FormatterHelper{}))

	return h
}

func mustChoice(t *testing.T, question string, choices *php.Array, def any) *ChoiceQuestion {
	t.Helper()
	q, err := NewChoiceQuestion(question, choices, def)
	if err != nil {
		t.Fatal(err)
	}

	return q
}

type asker interface {
	Ask(in Input, out Output, q Questioner) (any, error)
}

func askOK(t *testing.T, h asker, in Input, out Output, q Questioner) any {
	t.Helper()
	got, err := h.Ask(in, out, q)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return got
}

func assertAnswer(t *testing.T, got, want any) {
	t.Helper()
	if list, ok := want.([]any); ok {
		if !reflect.DeepEqual(phpList(got), list) {
			t.Fatalf("got %#v, want %#v", got, want)
		}

		return
	}
	if got != want {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

var heroes = func() *php.Array { return php.ListOf("Superman", "Batman", "Spiderman") }

func testAskChoice(t *testing.T, helper asker, symfony bool) {
	t.Helper()
	inputStream := getInputStream("\n1\n  1  \nFabien\n1\nFabien\n1\n0,2\n 0 , 2  \n\n\n")
	in := func() Input { return createStreamableInputInterfaceMock(inputStream, true) }

	q := mustChoice(t, "What is your favorite superhero?", heroes(), "2")
	_ = q.SetMaxAttempts(1)
	// first answer is an empty answer, we're supposed to receive the default value
	out := createOutputInterface()
	assertAnswer(t, askOK(t, helper, in(), out, q), "Spiderman")
	if symfony && !strings.Contains(outputContents(out), "What is your favorite superhero? [Spiderman]") {
		t.Fatalf("output %q", outputContents(out))
	}

	q = mustChoice(t, "What is your favorite superhero?", heroes(), nil)
	_ = q.SetMaxAttempts(1)
	assertAnswer(t, askOK(t, helper, in(), createOutputInterface(), q), "Batman")
	assertAnswer(t, askOK(t, helper, in(), createOutputInterface(), q), "Batman")

	q = mustChoice(t, "What is your favorite superhero?", heroes(), nil)
	q.SetErrorMessage(`Input "%s" is not a superhero!`)
	_ = q.SetMaxAttempts(2)
	out = createOutputInterface()
	assertAnswer(t, askOK(t, helper, in(), out, q), "Batman")
	if !strings.Contains(outputContents(out), `Input "Fabien" is not a superhero!`) {
		t.Fatalf("output %q", outputContents(out))
	}

	q = mustChoice(t, "What is your favorite superhero?", heroes(), "1")
	_ = q.SetMaxAttempts(1)
	_, err := helper.Ask(in(), createOutputInterface(), q)
	wantConsoleError(t, err, KindInvalidArgument, `Value "Fabien" is invalid`)

	q = mustChoice(t, "What is your favorite superhero?", heroes(), nil)
	_ = q.SetMaxAttempts(1)
	q.SetMultiselect(true)

	assertAnswer(t, askOK(t, helper, in(), createOutputInterface(), q), []any{"Batman"})
	assertAnswer(t, askOK(t, helper, in(), createOutputInterface(), q), []any{"Superman", "Spiderman"})
	assertAnswer(t, askOK(t, helper, in(), createOutputInterface(), q), []any{"Superman", "Spiderman"})

	q = mustChoice(t, "What is your favorite superhero?", heroes(), "0,1")
	_ = q.SetMaxAttempts(1)
	q.SetMultiselect(true)
	out = createOutputInterface()
	assertAnswer(t, askOK(t, helper, in(), out, q), []any{"Superman", "Batman"})
	if symfony && !strings.Contains(outputContents(out), "What is your favorite superhero? [Superman, Batman]") {
		t.Fatalf("output %q", outputContents(out))
	}

	q = mustChoice(t, "What is your favorite superhero?", heroes(), " 0 , 1 ")
	_ = q.SetMaxAttempts(1)
	q.SetMultiselect(true)
	out = createOutputInterface()
	assertAnswer(t, askOK(t, helper, in(), out, q), []any{"Superman", "Batman"})
	if symfony && !strings.Contains(outputContents(out), "What is your favorite superhero? [Superman, Batman]") {
		t.Fatalf("output %q", outputContents(out))
	}

	if !symfony {
		q = mustChoice(t, "What is your favorite superhero?", heroes(), 0)
		// We are supposed to get the default value since we are not in interactive mode
		assertAnswer(t, askOK(t, helper, createStreamableInputInterfaceMock(inputStream, true), createOutputInterface(), q), "Superman")
	}
}

func TestQuestionHelper_AskChoice(t *testing.T) {
	testAskChoice(t, newHelperWithFormatter(), false)
}

func TestQuestionHelper_AskChoiceNonInteractive(t *testing.T) {
	helper := newHelperWithFormatter()
	inputStream := getInputStream("\n1\n  1  \nFabien\n1\nFabien\n1\n0,2\n 0 , 2  \n\n\n")
	in := func() Input { return createStreamableInputInterfaceMock(inputStream, false) }

	q := mustChoice(t, "What is your favorite superhero?", heroes(), "0")
	assertAnswer(t, askOK(t, helper, in(), createOutputInterface(), q), "Superman")

	q = mustChoice(t, "What is your favorite superhero?", heroes(), "Batman")
	assertAnswer(t, askOK(t, helper, in(), createOutputInterface(), q), "Batman")

	q = mustChoice(t, "What is your favorite superhero?", heroes(), nil)
	assertAnswer(t, askOK(t, helper, in(), createOutputInterface(), q), nil)

	q = mustChoice(t, "What is your favorite superhero?", heroes(), "0")
	q.SetValidator(nil)
	assertAnswer(t, askOK(t, helper, in(), createOutputInterface(), q), "Superman")

	q = mustChoice(t, "What is your favorite superhero?", heroes(), nil)
	if _, err := helper.Ask(in(), createOutputInterface(), q); err != nil {
		wantConsoleError(t, err, KindInvalidArgument, `Value "" is invalid`)
	}

	q = mustChoice(t, "Who are your favorite superheros?", heroes(), "0, 1")
	q.SetMultiselect(true)
	assertAnswer(t, askOK(t, helper, in(), createOutputInterface(), q), []any{"Superman", "Batman"})

	q = mustChoice(t, "Who are your favorite superheros?", heroes(), "0, 1")
	q.SetMultiselect(true)
	q.SetValidator(nil)
	assertAnswer(t, askOK(t, helper, in(), createOutputInterface(), q), []any{"Superman", "Batman"})

	q = mustChoice(t, "Who are your favorite superheros?", heroes(), "0, Batman")
	q.SetMultiselect(true)
	assertAnswer(t, askOK(t, helper, in(), createOutputInterface(), q), []any{"Superman", "Batman"})

	q = mustChoice(t, "Who are your favorite superheros?", heroes(), nil)
	q.SetMultiselect(true)
	assertAnswer(t, askOK(t, helper, in(), createOutputInterface(), q), nil)

	q = mustChoice(t, "Who are your favorite superheros?", php.ArrayOf("a", "Batman", "b", "Superman"), "a")
	assertAnswer(t, askOK(t, helper, createStreamableInputInterfaceMock(getInputStream(""), false), createOutputInterface(), q), "a")

	q = mustChoice(t, "Who are your favorite superheros?", heroes(), "")
	q.SetMultiselect(true)
	if _, err := helper.Ask(in(), createOutputInterface(), q); err != nil {
		wantConsoleError(t, err, KindInvalidArgument, `Value "" is invalid`)
	}
}

func TestQuestionHelper_Ask(t *testing.T) {
	dialog := NewQuestionHelper()
	inputStream := getInputStream("\n8AM\n")

	q := NewQuestion("What time is it?", "2PM")
	assertAnswer(t, askOK(t, dialog, createStreamableInputInterfaceMock(inputStream, true), createOutputInterface(), q), "2PM")

	q = NewQuestion("What time is it?", "2PM")
	out := createOutputInterface()
	assertAnswer(t, askOK(t, dialog, createStreamableInputInterfaceMock(inputStream, true), out, q), "8AM")
	if got := outputContents(out); got != "What time is it?" {
		t.Fatalf("output %q", got)
	}
}

func TestQuestionHelper_AskNonTrimmed(t *testing.T) {
	dialog := NewQuestionHelper()
	q := NewQuestion("What time is it?", "2PM")
	q.SetTrimmable(false)
	out := createOutputInterface()
	assertAnswer(t, askOK(t, dialog, createStreamableInputInterfaceMock(getInputStream(" 8AM "), true), out, q), " 8AM ")
	if got := outputContents(out); got != "What time is it?" {
		t.Fatalf("output %q", got)
	}
}

func askAll(t *testing.T, dialog asker, inputStream *strings.Reader, q Questioner, want ...any) {
	t.Helper()
	for i, w := range want {
		got, err := dialog.Ask(createStreamableInputInterfaceMock(inputStream, true), createOutputInterface(), q)
		if err != nil {
			t.Fatalf("answer %d: %v", i, err)
		}
		if list, ok := w.([]any); ok {
			if !reflect.DeepEqual(phpList(got), list) {
				t.Fatalf("answer %d: got %#v, want %#v", i, got, w)
			}
		} else if got != w {
			t.Fatalf("answer %d: got %#v, want %#v", i, got, w)
		}
	}
}

func TestQuestionHelper_AskWithAutocomplete(t *testing.T) {
	fakeStty(t)
	// Acm<NEWLINE>
	// Ac<BACKSPACE><BACKSPACE>s<TAB>Test<NEWLINE>
	// <NEWLINE>
	// <UP ARROW><UP ARROW><UP ARROW><NEWLINE>
	// <UP ARROW><UP ARROW><UP ARROW><UP ARROW><UP ARROW><UP ARROW><UP ARROW><TAB>Test<NEWLINE>
	// <DOWN ARROW><NEWLINE>
	// S<BACKSPACE><BACKSPACE><DOWN ARROW><DOWN ARROW><NEWLINE>
	// F00<BACKSPACE><BACKSPACE>oo<TAB><NEWLINE>
	// F⭐<TAB><BACKSPACE><BACKSPACE>⭐<TAB><NEWLINE>
	inputStream := getInputStream("Acm\nAc\177\177s\tTest\n\n\033[A\033[A\033[A\n\033[A\033[A\033[A\033[A\033[A\033[A\033[A\tTest\n\033[B\nS\177\177\033[B\033[B\nF00\177\177oo\t\nF⭐\t\177\177⭐\t\n")

	q := NewQuestion("Please select a bundle", "FrameworkBundle")
	_ = q.SetAutocompleterValues(php.ListOf("AcmeDemoBundle", "AsseticBundle", "SecurityBundle", "FooBundle", "F⭐Y"))

	askAll(t, newHelperWithFormatter(), inputStream, q, "AcmeDemoBundle", "AsseticBundleTest", "FrameworkBundle", "SecurityBundle", "FooBundleTest", "AcmeDemoBundle", "AsseticBundle", "FooBundle", "F⭐Y")
}

func TestQuestionHelper_AskWithAutocompleteTrimmable(t *testing.T) {
	fakeStty(t)
	inputStream := getInputStream("Acm\nAc\177\177s\tTest\n\n\033[A\033[A\n\033[A\033[A\033[A\033[A\033[A\tTest\n\033[B\nS\177\177\033[B\033[B\nF00\177\177oo\t\n")

	q := NewQuestion("Please select a bundle", "FrameworkBundle")
	_ = q.SetAutocompleterValues(php.ListOf("AcmeDemoBundle ", "AsseticBundle", " SecurityBundle ", "FooBundle"))
	q.SetTrimmable(false)

	askAll(t, newHelperWithFormatter(), inputStream, q, "AcmeDemoBundle ", "AsseticBundleTest", "FrameworkBundle", " SecurityBundle ", "FooBundleTest", "AcmeDemoBundle ", "AsseticBundle", "FooBundle")
}

func TestQuestionHelper_AskWithAutocompleteCallback(t *testing.T) {
	fakeStty(t)
	// Po<TAB>Cr<TAB>P<DOWN ARROW><DOWN ARROW><NEWLINE>
	inputStream := getInputStream("Pa\177\177o\tCr\tP\033[A\033[A\n")

	q := NewQuestion("What's for dinner?", nil)

	// A simple test callback - return an array containing the words the
	// user has already completed, suffixed with all known words.
	callback := func(input string) []string {
		knownWords := []string{"Carrot", "Creme", "Curry", "Parsnip", "Pie", "Potato", "Tart"}
		inputWords := strings.Split(input, " ")
		inputWords = inputWords[:len(inputWords)-1]
		suggestionBase := ""
		if len(inputWords) > 0 {
			suggestionBase = strings.Join(inputWords, " ") + " "
		}
		out := make([]string, len(knownWords))
		for i, w := range knownWords {
			out[i] = suggestionBase + w + " "
		}

		return out
	}
	_ = q.SetAutocompleterCallback(callback)

	askAll(t, newHelperWithFormatter(), inputStream, q, "Potato Creme Pie")
}

func TestQuestionHelper_AskWithAutocompleteWithNonSequentialKeys(t *testing.T) {
	fakeStty(t)
	// <UP ARROW><UP ARROW><NEWLINE><DOWN ARROW><DOWN ARROW><NEWLINE>
	inputStream := getInputStream("\033[A\033[A\n\033[B\033[B\n")

	q := mustChoice(t, "Please select a bundle", php.ArrayOf(1, "AcmeDemoBundle", 4, "AsseticBundle"), nil)
	_ = q.SetMaxAttempts(1)

	askAll(t, newHelperWithFormatter(), inputStream, q, "AcmeDemoBundle", "AsseticBundle")
}

func TestQuestionHelper_AskWithAutocompleteWithExactMatch(t *testing.T) {
	fakeStty(t)
	q := mustChoice(t, "Please select a city", php.ArrayOf("a", "berlin", "b", "copenhagen", "c", "amsterdam"), nil)
	_ = q.SetMaxAttempts(1)

	askAll(t, newHelperWithFormatter(), getInputStream("b\n"), q, "b")
}

func TestQuestionHelper_AskWithAutocompleteWithMultiByteCharacter(t *testing.T) {
	fakeStty(t)
	for _, character := range []string{"$", "¢", "€", "𐍈"} {
		q := mustChoice(t, "Please select a character", php.ArrayOf("$", "1 byte character", "¢", "2 bytes character", "€", "3 bytes character", "𐍈", "4 bytes character"), nil)
		_ = q.SetMaxAttempts(1)

		askAll(t, newHelperWithFormatter(), getInputStream(character+"\n"), q, character)
	}
}

func TestQuestionHelper_AutocompleteWithTrailingBackslash(t *testing.T) {
	fakeStty(t)
	q := NewQuestion("", nil)
	expectedCompletion := `ExampleNamespace\`
	_ = q.SetAutocompleterValues(php.ListOf(expectedCompletion))

	out := createOutputInterface()
	if _, err := newHelperWithFormatter().Ask(createStreamableInputInterfaceMock(getInputStream("E"), true), out, q); err != nil {
		t.Fatal(err)
	}

	// Shell control (esc) sequences are not so important: we only care that
	// <hl> tag is interpreted correctly and replaced
	actual := strings.NewReplacer("\0337", "", "\0338", "", "\033[K", "").Replace(outputContents(out))
	// Remove colors (e.g. "\033[30m", "\033[31;41m")
	actual = regexp.MustCompile(`\033\[\d+(;\d+)?m`).ReplaceAllString(actual, "")

	if actual != expectedCompletion {
		t.Fatalf("got %q", actual)
	}
}

func TestQuestionHelper_AskHiddenResponse(t *testing.T) {
	q := NewQuestion("What time is it?", nil)
	_ = q.SetHidden(true)
	assertAnswer(t, askOK(t, NewQuestionHelper(), createStreamableInputInterfaceMock(getInputStream("8AM\n"), true), createOutputInterface(), q), "8AM")
}

func TestQuestionHelper_AskHiddenResponseNotTrimmed(t *testing.T) {
	q := NewQuestion("What time is it?", nil)
	_ = q.SetHidden(true)
	q.SetTrimmable(false)
	assertAnswer(t, askOK(t, NewQuestionHelper(), createStreamableInputInterfaceMock(getInputStream(" 8AM\n"), true), createOutputInterface(), q), " 8AM\n")
}

func TestQuestionHelper_AskMultilineResponseWithEOF(t *testing.T) {
	essay := `Lorem ipsum dolor sit amet, consectetur adipiscing elit. Pellentesque pretium lectus quis suscipit porttitor. Sed pretium bibendum vestibulum.

Etiam accumsan, justo vitae imperdiet aliquet, neque est sagittis mauris, sed interdum massa leo id leo.

Aliquam rhoncus, libero ac blandit convallis, est sapien hendrerit nulla, vitae aliquet tellus orci a odio. Aliquam gravida ante sit amet massa lacinia, ut condimentum purus venenatis.

Vivamus et erat dictum, euismod neque in, laoreet odio. Aenean vitae tellus at leo vestibulum auctor id eget urna.`

	q := NewQuestion("Write an essay", nil)
	q.SetMultiline(true)
	assertAnswer(t, askOK(t, NewQuestionHelper(), createStreamableInputInterfaceMock(getInputStream(essay), true), createOutputInterface(), q), essay)
}

func TestQuestionHelper_AskMultilineResponseWithSingleNewline(t *testing.T) {
	q := NewQuestion("Write an essay", nil)
	q.SetMultiline(true)
	assertAnswer(t, askOK(t, NewQuestionHelper(), createStreamableInputInterfaceMock(getInputStream("\n"), true), createOutputInterface(), q), nil)
}

func TestQuestionHelper_AskMultilineResponseWithDataAfterNewline(t *testing.T) {
	q := NewQuestion("Write an essay", nil)
	q.SetMultiline(true)
	assertAnswer(t, askOK(t, NewQuestionHelper(), createStreamableInputInterfaceMock(getInputStream("\nthis is text"), true), createOutputInterface(), q), nil)
}

func TestQuestionHelper_AskMultilineResponseWithMultipleNewlinesAtEnd(t *testing.T) {
	q := NewQuestion("Write an essay", nil)
	q.SetMultiline(true)
	assertAnswer(t, askOK(t, NewQuestionHelper(), createStreamableInputInterfaceMock(getInputStream("This is a body\n\n"), true), createOutputInterface(), q), "This is a body")
}

func TestQuestionHelper_AskMultilineResponseWithWithCursorInMiddleOfSeekableInputStream(t *testing.T) {
	response := getInputStream("This\nis\nsome\ninput")
	_, _ = response.Seek(8, 0)

	q := NewQuestion("Write an essay", nil)
	q.SetMultiline(true)
	assertAnswer(t, askOK(t, NewQuestionHelper(), createStreamableInputInterfaceMock(response, true), createOutputInterface(), q), "some\ninput")
	if pos := response.Size() - int64(response.Len()); pos != 8 {
		t.Fatalf("offset %d", pos)
	}
}

func TestQuestionHelper_AskConfirmation(t *testing.T) {
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
		q := NewConfirmationQuestion("Do you like French fries?", c.def, nil)
		got := askOK(t, NewQuestionHelper(), createStreamableInputInterfaceMock(getInputStream(c.answer+"\n"), true), createOutputInterface(), q)
		if got != c.expected {
			t.Errorf("%q: got %#v", c.answer, got)
		}
	}
}

func TestQuestionHelper_AskConfirmationWithCustomTrueAnswer(t *testing.T) {
	inputStream := getInputStream("j\ny\n")
	for range 2 {
		q := NewConfirmationQuestion("Do you like French fries?", false, regexp.MustCompile(`(?i)^(j|y)`))
		assertAnswer(t, askOK(t, NewQuestionHelper(), createStreamableInputInterfaceMock(inputStream, true), createOutputInterface(), q), true)
	}
}

func TestQuestionHelper_AskAndValidate(t *testing.T) {
	dialog := newHelperWithFormatter()

	errMsg := "This is not a color!"
	validator := func(color any) (any, error) {
		if color != "white" && color != "black" {
			return nil, errors.New(errMsg)
		}

		return color, nil
	}

	q := NewQuestion("What color was the white horse of Henry IV?", "white")
	q.SetValidator(validator)
	_ = q.SetMaxAttempts(2)

	inputStream := getInputStream("\nblack\n")
	assertAnswer(t, askOK(t, dialog, createStreamableInputInterfaceMock(inputStream, true), createOutputInterface(), q), "white")
	assertAnswer(t, askOK(t, dialog, createStreamableInputInterfaceMock(inputStream, true), createOutputInterface(), q), "black")

	_, err := dialog.Ask(createStreamableInputInterfaceMock(getInputStream("green\nyellow\norange\n"), true), createOutputInterface(), q)
	if err == nil || err.Error() != errMsg {
		t.Fatalf("got %v", err)
	}
}

func TestQuestionHelper_SelectChoiceFromSimpleChoices(t *testing.T) {
	cases := []struct {
		answer, expected string
	}{
		{"0", "My environment 1"},
		{"1", "My environment 2"},
		{"2", "My environment 3"},
		{"My environment 1", "My environment 1"},
		{"My environment 2", "My environment 2"},
		{"My environment 3", "My environment 3"},
	}
	for _, c := range cases {
		q := mustChoice(t, "Please select the environment to load", php.ListOf("My environment 1", "My environment 2", "My environment 3"), nil)
		_ = q.SetMaxAttempts(1)
		assertAnswer(t, askOK(t, newHelperWithFormatter(), createStreamableInputInterfaceMock(getInputStream(c.answer+"\n"), true), createOutputInterface(), q), c.expected)
	}
}

func TestQuestionHelper_SpecialCharacterChoiceFromMultipleChoiceList(t *testing.T) {
	cases := []struct {
		answer   string
		expected []any
	}{
		{".", []any{"."}},
		{"., src", []any{".", "src"}},
	}
	for _, c := range cases {
		q := mustChoice(t, "Please select the directory", php.ListOf(".", "src"), nil)
		_ = q.SetMaxAttempts(1)
		q.SetMultiselect(true)
		assertAnswer(t, askOK(t, newHelperWithFormatter(), createStreamableInputInterfaceMock(getInputStream(c.answer+"\n"), true), createOutputInterface(), q), c.expected)
	}
}

func TestQuestionHelper_SelectChoiceFromChoiceList(t *testing.T) {
	cases := [][2]string{
		{"env_1", "env_1"},
		{"env_2", "env_2"},
		{"env_3", "env_3"},
		{"My environment 1", "env_1"},
	}
	for _, c := range cases {
		q := mustChoice(t, "Please select the environment to load", php.ArrayOf("env_1", "My environment 1", "env_2", "My environment", "env_3", "My environment"), nil)
		_ = q.SetMaxAttempts(1)
		assertAnswer(t, askOK(t, newHelperWithFormatter(), createStreamableInputInterfaceMock(getInputStream(c[0]+"\n"), true), createOutputInterface(), q), c[1])
	}
}

func TestQuestionHelper_AmbiguousChoiceFromChoicelist(t *testing.T) {
	q := mustChoice(t, "Please select the environment to load", php.ArrayOf("env_1", "My first environment", "env_2", "My environment", "env_3", "My environment"), nil)
	_ = q.SetMaxAttempts(1)

	_, err := newHelperWithFormatter().Ask(createStreamableInputInterfaceMock(getInputStream("My environment\n"), true), createOutputInterface(), q)
	wantConsoleError(t, err, KindInvalidArgument, `The provided answer is ambiguous. Value should be one of "env_2" or "env_3".`)
}

func TestQuestionHelper_NoInteraction(t *testing.T) {
	q := NewQuestion("Do you have a job?", "not yet")
	assertAnswer(t, askOK(t, NewQuestionHelper(), createStreamableInputInterfaceMock(nil, false), createOutputInterface(), q), "not yet")
}

// recordingOutput records the messages written with a line break.
type recordingOutput struct {
	*BufferedOutput
	lines []string
}

func (o *recordingOutput) Write(message string, newline bool, options int) {
	if newline {
		o.lines = append(o.lines, message)
	}
	o.BufferedOutput.Write(message, newline, options)
}

func (o *recordingOutput) Writeln(message string) { o.Write(message, true, OutputNormal) }

func TestQuestionHelper_ChoiceOutputFormattingQuestionForUtf8Keys(t *testing.T) {
	question := "Lorem ipsum?"
	outputShown := []string{
		question,
		"  [<info>foo   </info>] foo",
		"  [<info>żółw  </info>] bar",
		"  [<info>łabądź</info>] baz",
	}
	out := &recordingOutput{BufferedOutput: NewBufferedOutput(VerbosityNormal, false, nil)}

	q := mustChoice(t, question, php.ArrayOf("foo", "foo", "żółw", "bar", "łabądź", "baz"), "foo")
	askOK(t, newHelperWithFormatter(), createStreamableInputInterfaceMock(getInputStream("\n"), true), out, q)

	if !reflect.DeepEqual(out.lines, outputShown) {
		t.Fatalf("got %#v", out.lines)
	}
}

func TestQuestionHelper_AskThrowsExceptionOnMissingInput(t *testing.T) {
	_, err := NewQuestionHelper().Ask(createStreamableInputInterfaceMock(getInputStream(""), true), createOutputInterface(), NewQuestion("What's your name?", nil))
	wantConsoleError(t, err, KindMissingInput, "Aborted.")
}

func TestQuestionHelper_AskThrowsExceptionOnMissingInputForChoiceQuestion(t *testing.T) {
	_, err := NewQuestionHelper().Ask(createStreamableInputInterfaceMock(getInputStream(""), true), createOutputInterface(), mustChoice(t, "Choice", php.ListOf("a", "b"), nil))
	wantConsoleError(t, err, KindMissingInput, "Aborted.")
}

func TestQuestionHelper_AskThrowsExceptionOnMissingInputWithValidator(t *testing.T) {
	q := NewQuestion("What's your name?", nil)
	q.SetValidator(func(value any) (any, error) {
		if !php.ToBool(value) {
			return nil, errors.New("A value is required.")
		}

		return nil, nil
	})

	_, err := NewQuestionHelper().Ask(createStreamableInputInterfaceMock(getInputStream(""), true), createOutputInterface(), q)
	wantConsoleError(t, err, KindMissingInput, "Aborted.")
}

func TestQuestionHelper_QuestionValidatorRepeatsThePrompt(t *testing.T) {
	tries := 0
	application := NewApplication("UNKNOWN", "UNKNOWN")
	application.SetAutoExit(false)
	cmd, err := application.Register("question")
	if err != nil {
		t.Fatal(err)
	}
	cmd.Base().SetCode(func(in Input, out Output) (int, error) {
		q := NewQuestion("This is a promptable question", nil)
		q.SetValidator(func(value any) (any, error) {
			tries++
			if !php.ToBool(value) {
				return nil, errors.New("")
			}

			return value, nil
		})

		if _, err := NewQuestionHelper().Ask(in, out, q); err != nil {
			return 0, err
		}

		return 0, nil
	})

	tester := newApplicationTester(application)
	tester.SetInputs("", "not-empty")

	statusCode, err := tester.Run([]Param{P("command", "question")}, testerOptions{interactive: new(true)})
	if err != nil {
		t.Fatal(err)
	}
	if tries != 2 || statusCode != 0 {
		t.Fatalf("tries %d, status %d", tries, statusCode)
	}
}

func TestQuestionHelper_EmptyChoices(t *testing.T) {
	_, err := NewChoiceQuestion("Question", php.NewArray(), "irrelevant")
	wantConsoleError(t, err, KindSPLLogic, "Choice question must have at least 1 choice available.")
}

func TestQuestionHelper_TraversableAutocomplete(t *testing.T) {
	fakeStty(t)
	inputStream := getInputStream("Acm\nAc\177\177s\tTest\n\n\033[A\033[A\n\033[A\033[A\033[A\033[A\033[A\tTest\n\033[B\nS\177\177\033[B\033[B\nF00\177\177oo\t\n")

	q := NewQuestion("Please select a bundle", "FrameworkBundle")
	_ = q.SetAutocompleterSeq(slices.Values([]string{"AcmeDemoBundle", "AsseticBundle", "SecurityBundle", "FooBundle"}))

	askAll(t, newHelperWithFormatter(), inputStream, q, "AcmeDemoBundle", "AsseticBundleTest", "FrameworkBundle", "SecurityBundle", "FooBundleTest", "AcmeDemoBundle", "AsseticBundle", "FooBundle")
}

func TestQuestionHelper_DisableStty(t *testing.T) {
	fakeStty(t)
	DisableStty()
	t.Cleanup(func() { sttyEnabled.Store(true) })

	q := mustChoice(t, "Please select a bundle", php.ArrayOf(1, "AcmeDemoBundle", 4, "AsseticBundle"), nil)
	_ = q.SetMaxAttempts(1)

	// <UP ARROW><UP ARROW><NEWLINE><DOWN ARROW><DOWN ARROW><NEWLINE>
	// Gives `AcmeDemoBundle` with stty
	_, err := newHelperWithFormatter().Ask(createStreamableInputInterfaceMock(getInputStream("\033[A\033[A\n\033[B\033[B\n"), true), createOutputInterface(), q)
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindInvalidArgument || !strings.Contains(e.Message, "invalid") {
		t.Fatalf("got %v", err)
	}
}

func TestQuestionHelper_TraversableMultiselectAutocomplete(t *testing.T) {
	fakeStty(t)
	// <NEWLINE>
	// F<TAB><NEWLINE>
	// A<3x UP ARROW><TAB>,F<TAB><NEWLINE>
	// F00<BACKSPACE><BACKSPACE>o<TAB>,A<DOWN ARROW>,<SPACE>SecurityBundle<NEWLINE>
	// Acme<TAB>,<SPACE>As<TAB><29x BACKSPACE>S<TAB><NEWLINE>
	// Ac<TAB>,As<TAB><3x BACKSPACE>d<TAB><NEWLINE>
	inputStream := getInputStream("\nF\t\nA\033[A\033[A\033[A\t,F\t\nF00\177\177o\t,A\033[B\t, SecurityBundle\nAcme\t, As\t" + strings.Repeat("\177", 29) + "S\t\nAc\t,As\t\177\177\177d\t\n")

	q := mustChoice(t, "Please select a bundle (defaults to AcmeDemoBundle and AsseticBundle)", php.ListOf("AcmeDemoBundle", "AsseticBundle", "SecurityBundle", "FooBundle"), "0,1")

	// This tests that autocomplete works for all multiselect choices entered by the user
	q.SetMultiselect(true)

	askAll(t, newHelperWithFormatter(), inputStream, q,
		[]any{"AcmeDemoBundle", "AsseticBundle"},
		[]any{"FooBundle"},
		[]any{"AsseticBundle", "FooBundle"},
		[]any{"FooBundle", "AsseticBundle", "SecurityBundle"},
		[]any{"SecurityBundle"},
		[]any{"AcmeDemoBundle", "AsseticBundle"},
	)
}

func TestQuestionHelper_AutocompleteMoveCursorBackwards(t *testing.T) {
	fakeStty(t)
	// F<TAB><BACKSPACE><BACKSPACE><BACKSPACE>
	q := NewQuestion("Question?", "F⭐Y")
	_ = q.SetAutocompleterValues(php.ListOf("F⭐Y"))

	out := createOutputInterface()
	askOK(t, newHelperWithFormatter(), createStreamableInputInterfaceMock(getInputStream("F\t\177\177\177"), true), out, q)

	if got := outputContents(out); !strings.HasSuffix(got, "\033[1D\033[K\033[2D\033[K\033[1D\033[K") {
		t.Fatalf("output %q", got)
	}
}

func TestSymfonyQuestionHelper_AskChoice(t *testing.T) {
	h := NewSymfonyQuestionHelper()
	h.SetHelperSet(NewHelperSet(&FormatterHelper{}))
	testAskChoice(t, h, true)
}

func TestSymfonyQuestionHelper_AskChoiceWithChoiceValueAsDefault(t *testing.T) {
	h := NewSymfonyQuestionHelper()
	h.SetHelperSet(NewHelperSet(&FormatterHelper{}))
	q := mustChoice(t, "What is your favorite superhero?", heroes(), "Batman")
	_ = q.SetMaxAttempts(1)

	out := createOutputInterface()
	assertAnswer(t, askOK(t, h, createStreamableInputInterfaceMock(getInputStream("Batman\n"), true), out, q), "Batman")
	if !strings.Contains(outputContents(out), "What is your favorite superhero? [Batman]") {
		t.Fatalf("output %q", outputContents(out))
	}
}

func TestSymfonyQuestionHelper_AskReturnsNullIfValidatorAllowsIt(t *testing.T) {
	q := NewQuestion("What is your favorite superhero?", nil)
	q.SetValidator(func(value any) (any, error) { return value, nil })
	assertAnswer(t, askOK(t, NewSymfonyQuestionHelper(), createStreamableInputInterfaceMock(getInputStream("\n"), true), createOutputInterface(), q), nil)
}

func symfonyAskOutput(t *testing.T, input string, q Questioner) string {
	t.Helper()
	out := createOutputInterface()
	_, _ = NewSymfonyQuestionHelper().Ask(createStreamableInputInterfaceMock(getInputStream(input), true), out, q)

	return outputContents(out)
}

func TestSymfonyQuestionHelper_AskEscapeDefaultValue(t *testing.T) {
	if got := symfonyAskOutput(t, `\`, NewQuestion("Can I have a backslash?", `\`)); !strings.Contains(got, `Can I have a backslash? [\]`) {
		t.Fatalf("output %q", got)
	}
}

func TestSymfonyQuestionHelper_AskEscapeAndFormatLabel(t *testing.T) {
	got := symfonyAskOutput(t, `Foo\Bar`, NewQuestion(`Do you want to use Foo\Bar <comment>or</comment> Foo\Baz\?`, `Foo\Baz`))
	if !strings.Contains(got, `Do you want to use Foo\Bar or Foo\Baz\? [Foo\Baz]:`) {
		t.Fatalf("output %q", got)
	}
}

func TestSymfonyQuestionHelper_LabelTrailingBackslash(t *testing.T) {
	if got := symfonyAskOutput(t, "sure", NewQuestion(`Question with a trailing \`, nil)); !strings.Contains(got, `Question with a trailing \`) {
		t.Fatalf("output %q", got)
	}
}

func TestSymfonyQuestionHelper_AskThrowsExceptionOnMissingInput(t *testing.T) {
	_, err := NewSymfonyQuestionHelper().Ask(createStreamableInputInterfaceMock(getInputStream(""), true), createOutputInterface(), NewQuestion("What's your name?", nil))
	wantConsoleError(t, err, KindMissingInput, "Aborted.")
}

func TestSymfonyQuestionHelper_ChoiceQuestionPadding(t *testing.T) {
	q := mustChoice(t, "qqq", php.ArrayOf("foo", "foo", "żółw", "bar", "łabądź", "baz"), nil)
	want := " qqq:\n  [foo   ] foo\n  [żółw  ] bar\n  [łabądź] baz\n >"
	if got := symfonyAskOutput(t, "foo\n", q); !strings.Contains(got, want) {
		t.Fatalf("output %q", got)
	}
}

func TestSymfonyQuestionHelper_ChoiceQuestionCustomPrompt(t *testing.T) {
	q := mustChoice(t, "qqq", php.ListOf("foo"), nil)
	q.SetPrompt(" >ccc> ")
	want := " qqq:\n  [0] foo\n >ccc>"
	if got := symfonyAskOutput(t, "foo\n", q); !strings.Contains(got, want) {
		t.Fatalf("output %q", got)
	}
}

func TestSymfonyQuestionHelper_AskMultilineQuestionIncludesHelpText(t *testing.T) {
	q := NewQuestion("Write an essay", nil)
	q.SetMultiline(true)
	if got := symfonyAskOutput(t, `\`, q); !strings.Contains(got, "Write an essay (press Ctrl+D to continue)") {
		t.Fatalf("output %q", got)
	}
}
