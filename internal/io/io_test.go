// Ports tests/Composer/Test/IO/{BaseIOTest,BufferIOTest,ConsoleIOTest,NullIOTest}.php
// (Composer); PHPUnit mocks are small fakes.

package io

import (
	"bytes"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
)

// fakeInput is the InputInterface mock: only the methods ConsoleIO calls
// are implemented.
type fakeInput struct {
	console.Input
	interactive []bool
	calls       int
}

func (f *fakeInput) IsInteractive() bool {
	v := f.interactive[f.calls]
	f.calls++

	return v
}

type writeCall struct {
	message string
	newline bool
	options int
}

// fakeOutput is the OutputInterface / ConsoleOutputInterface mock.
type fakeOutput struct {
	console.Output
	verbosity int
	decorated bool
	errOut    console.Output
	writes    []writeCall
	verbCalls int
}

func (f *fakeOutput) Verbosity() int {
	f.verbCalls++

	return f.verbosity
}
func (f *fakeOutput) IsDecorated() bool { return f.decorated }
func (f *fakeOutput) Write(message string, newline bool, options int) {
	f.writes = append(f.writes, writeCall{message, newline, options})
}

type fakeConsoleOutput struct {
	*fakeOutput
	errCalls int
}

func (f *fakeConsoleOutput) ErrorOutput() console.Output {
	f.errCalls++
	if f.errOut != nil {
		return f.errOut
	}

	return f
}
func (f *fakeConsoleOutput) SetErrorOutput(out console.Output) { f.errOut = out }

// fakeHelperSet is the HelperSet mock.
type fakeHelperSet struct {
	helper console.Helper
	gets   []string
}

func (f *fakeHelperSet) Get(name string) (console.Helper, error) {
	f.gets = append(f.gets, name)

	return f.helper, nil
}

// fakeQuestionHelper is the QuestionHelper mock.
type fakeQuestionHelper struct {
	console.HelperBase
	asked  []console.Questioner
	answer any
}

func (*fakeQuestionHelper) Name() string { return "question" }
func (f *fakeQuestionHelper) Ask(in console.Input, out console.Output, q console.Questioner) (any, error) {
	if in == nil || out == nil {
		return nil, errors.New("nil input or output")
	}
	f.asked = append(f.asked, q)

	return f.answer, nil
}

func TestConsoleIO_IsInteractive(t *testing.T) {
	in := &fakeInput{interactive: []bool{true, false}}
	io := NewConsoleIO(in, &fakeOutput{}, &fakeHelperSet{})

	if !io.IsInteractive() {
		t.Error("want interactive")
	}
	if io.IsInteractive() {
		t.Error("want non-interactive")
	}
	if in.calls != 2 {
		t.Errorf("isInteractive called %d times", in.calls)
	}
}

func TestConsoleIO_Write(t *testing.T) {
	out := &fakeOutput{verbosity: console.VerbosityNormal}
	io := NewConsoleIO(&fakeInput{}, out, &fakeHelperSet{})
	io.Write("some information about something", false, Normal)

	if out.verbCalls != 1 {
		t.Errorf("getVerbosity called %d times", out.verbCalls)
	}
	want := []writeCall{{"some information about something", false, console.VerbosityNormal}}
	if !reflect.DeepEqual(out.writes, want) {
		t.Errorf("writes = %#v", out.writes)
	}
}

func TestConsoleIO_WriteError(t *testing.T) {
	out := &fakeConsoleOutput{fakeOutput: &fakeOutput{verbosity: console.VerbosityNormal}}
	io := NewConsoleIO(&fakeInput{}, out, &fakeHelperSet{})
	io.WriteError("some information about something", false, Normal)

	if out.verbCalls != 1 || out.errCalls != 1 {
		t.Errorf("getVerbosity %d, getErrorOutput %d calls", out.verbCalls, out.errCalls)
	}
	want := []writeCall{{"some information about something", false, console.VerbosityNormal}}
	if !reflect.DeepEqual(out.writes, want) {
		t.Errorf("writes = %#v", out.writes)
	}
}

func TestConsoleIO_WriteWithMultipleLineStringWhenDebugging(t *testing.T) {
	out := &fakeOutput{verbosity: console.VerbosityNormal}
	io := NewConsoleIO(&fakeInput{}, out, &fakeHelperSet{})
	io.EnableDebugging(time.Now())

	example := strings.Split(`First line\nSecond lines`, `\n`)
	io.WriteMessages(example, false, Normal)

	if out.verbCalls != 1 || len(out.writes) != 2 {
		t.Fatalf("getVerbosity %d calls, writes %#v", out.verbCalls, out.writes)
	}
	for i, re := range []*regexp.Regexp{regexp.MustCompile(`(.*)/(.*) First line`), regexp.MustCompile(`(.*)/(.*) Second line`)} {
		if !re.MatchString(out.writes[i].message) || out.writes[i].newline {
			t.Errorf("write %d = %#v", i, out.writes[i])
		}
	}
	if !regexp.MustCompile(`^\[\d+\.\dMiB/\d+\.\d\ds\] First line$`).MatchString(out.writes[0].message) {
		t.Errorf("debug prefix = %q", out.writes[0].message)
	}
}

func TestConsoleIO_Overwrite(t *testing.T) {
	out := &fakeOutput{verbosity: console.VerbosityNormal, decorated: true}
	io := NewConsoleIO(&fakeInput{}, out, &fakeHelperSet{})
	io.Write("something (<question>strlen = 23</question>)", true, Normal)
	io.Overwrite("shorter (<comment>12</comment>)", false, -1, Normal)
	io.Overwrite("something longer than initial (<info>34</info>)", true, -1, Normal)

	want := []struct {
		message string
		newline bool
	}{
		{"something (<question>strlen = 23</question>)", true},
		{strings.Repeat("\x08", 23), false},
		{"shorter (<comment>12</comment>)", false},
		{strings.Repeat(" ", 11), false},
		{strings.Repeat("\x08", 11), false},
		{strings.Repeat("\x08", 12), false},
		{"something longer than initial (<info>34</info>)", false},
	}
	if len(out.writes) < 7 {
		t.Fatalf("writes = %#v", out.writes)
	}
	for i, w := range want {
		if out.writes[i].message != w.message || out.writes[i].newline != w.newline {
			t.Errorf("write %d = %#v, want %#v", i, out.writes[i], w)
		}
	}
}

func written(out *fakeOutput) string {
	var b strings.Builder
	for _, w := range out.writes {
		b.WriteString(w.message)
	}

	return b.String()
}

func TestConsoleIO_OverwriteErrorWithoutDecorationWritesNoBackspaces(t *testing.T) {
	out := &fakeConsoleOutput{fakeOutput: &fakeOutput{verbosity: console.VerbosityNormal}}
	io := NewConsoleIO(&fakeInput{}, out, &fakeHelperSet{})
	io.WriteError("Loading composer repositories with package information", true, Normal)
	io.OverwriteError("", false, -1, Normal)

	w := written(out.fakeOutput)
	if strings.Contains(w, "\x08") || !strings.Contains(w, "Loading composer repositories with package information") {
		t.Errorf("written = %q", w)
	}
}

func TestConsoleIO_OverwriteErrorChecksTheErrorOutputDecoration(t *testing.T) {
	// stdout is a decorated terminal but the error output is redirected (not decorated),
	// so overwriteError must write a plain line and no backspaces to the error output
	errOut := &fakeOutput{}
	out := &fakeConsoleOutput{fakeOutput: &fakeOutput{verbosity: console.VerbosityNormal, decorated: true, errOut: errOut}}
	io := NewConsoleIO(&fakeInput{}, out, &fakeHelperSet{})
	io.WriteError("Loading composer repositories with package information", true, Normal)
	io.OverwriteError("Reading composer.json of acme/foo (1.0.0)", false, -1, Normal)

	w := written(errOut)
	if strings.Contains(w, "\x08") || !strings.Contains(w, "Reading composer.json of acme/foo (1.0.0)") {
		t.Errorf("written = %q", w)
	}
}

func newAskIO(answer any) (*ConsoleIO, *fakeQuestionHelper, *fakeHelperSet) {
	helper := &fakeQuestionHelper{answer: answer}
	set := &fakeHelperSet{helper: helper}

	return NewConsoleIO(&fakeInput{}, &fakeOutput{}, set), helper, set
}

func checkAsked(t *testing.T, helper *fakeQuestionHelper, set *fakeHelperSet) console.Questioner {
	t.Helper()
	if len(helper.asked) != 1 || !reflect.DeepEqual(set.gets, []string{"question"}) {
		t.Fatalf("asked %d questions, helper gets %v", len(helper.asked), set.gets)
	}

	return helper.asked[0]
}

func TestConsoleIO_Ask(t *testing.T) {
	io, helper, set := newAskIO(nil)
	if _, err := io.Ask("Why?", "default"); err != nil {
		t.Fatal(err)
	}
	q, ok := checkAsked(t, helper, set).(*console.Question)
	if !ok || q.Question() != "Why?" || q.Default() != "default" {
		t.Errorf("question = %#v", q)
	}
}

func TestConsoleIO_AskConfirmation(t *testing.T) {
	io, helper, set := newAskIO(true)
	got, err := io.AskConfirmation("Why?", false)
	if err != nil || !got {
		t.Fatal(got, err)
	}
	if _, ok := checkAsked(t, helper, set).(*console.StrictConfirmationQuestion); !ok {
		t.Error("want a StrictConfirmationQuestion")
	}
}

func TestConsoleIO_AskAndValidate(t *testing.T) {
	io, helper, set := newAskIO(nil)
	validator := func(any) (any, error) { return true, nil }
	if _, err := io.AskAndValidate("Why?", validator, 10, "default"); err != nil {
		t.Fatal(err)
	}
	q, ok := checkAsked(t, helper, set).(*console.Question)
	if !ok || q.MaxAttempts() != 10 || q.Validator() == nil || q.Default() != "default" {
		t.Errorf("question = %#v", q)
	}
}

func TestConsoleIO_AskAndHideAnswer(t *testing.T) {
	io, helper, set := newAskIO("secret")
	got, err := io.AskAndHideAnswer("Password?")
	if err != nil || got != "secret" {
		t.Fatal(got, err)
	}
	if q, ok := checkAsked(t, helper, set).(*console.Question); !ok || !q.IsHidden() {
		t.Error("want a hidden Question")
	}
}

func TestConsoleIO_Select(t *testing.T) {
	io, helper, set := newAskIO(php.ListOf("item2"))
	result, err := io.Select("Select item", php.ListOf("item1", "item2"), "item1", 0, "Error message", true)
	if err != nil {
		t.Fatal(err)
	}
	q, ok := checkAsked(t, helper, set).(*console.ChoiceQuestion)
	if !ok || !q.IsMultiselect() || q.MaxAttempts() != 0 {
		t.Errorf("question = %#v", q)
	}
	if got, err := php.JSONEncode(result, 0); err != nil || got != `["1"]` {
		t.Errorf("result = %s", got)
	}
}

func TestConsoleIO_SelectSingleAndAssoc(t *testing.T) {
	io, _, _ := newAskIO("item2")
	if got, _ := io.Select("q", php.ListOf("item1", "item2"), nil, 0, DefaultSelectErrorMessage, false); got != "1" {
		t.Errorf("list single = %#v", got)
	}
	io, _, _ = newAskIO("missing")
	if got, _ := io.Select("q", php.ListOf("item1"), nil, 0, DefaultSelectErrorMessage, false); got != "" {
		t.Errorf("list missing = %#v", got)
	}
	io, _, _ = newAskIO("b")
	if got, _ := io.Select("q", php.ArrayOf("a", "x", "b", "y"), nil, 0, DefaultSelectErrorMessage, false); got != "b" {
		t.Errorf("assoc = %#v", got)
	}
}

func TestConsoleIO_SetAndGetAuthentication(t *testing.T) {
	io := NewConsoleIO(&fakeInput{}, &fakeOutput{}, &fakeHelperSet{})
	io.SetAuthentication("repoName", "l3l0", new("passwd"))

	auth := io.Authentication("repoName")
	if auth.Username == nil || *auth.Username != "l3l0" || auth.Password == nil || *auth.Password != "passwd" {
		t.Errorf("auth = %#v", auth)
	}
}

func TestConsoleIO_GetAuthenticationWhenDidNotSet(t *testing.T) {
	io := NewConsoleIO(&fakeInput{}, &fakeOutput{}, &fakeHelperSet{})
	if auth := io.Authentication("repoName"); auth.Username != nil || auth.Password != nil {
		t.Errorf("auth = %#v", auth)
	}
}

func TestConsoleIO_HasAuthentication(t *testing.T) {
	io := NewConsoleIO(&fakeInput{}, &fakeOutput{}, &fakeHelperSet{})
	io.SetAuthentication("repoName", "l3l0", new("passwd"))

	if !io.HasAuthentication("repoName") || io.HasAuthentication("repoName2") {
		t.Error("hasAuthentication mismatch")
	}
}

func TestConsoleIO_Sanitize(t *testing.T) {
	cases := []struct {
		name          string
		input         string
		allowNewlines bool
		expected      string
	}{
		{`string with \n allowed`, "Hello\nWorld", true, "Hello\nWorld"},
		{`string with \r\n allowed`, "Hello\r\nWorld", true, "Hello\r\nWorld"},
		{`string with standalone \r removed`, "Hello\rWorld", true, "HelloWorld"},
		{"string with escape sequence removed", "Hello\x1B[31mWorld", true, "HelloWorld"},
		{"string with control chars removed", "Hello\x01\x08\x09World", true, "HelloWorld"},
		{"string with mixed control chars and newlines", "Line1\n\x1B[32mLine2\x08\rLine3", true, "Line1\nLine2Line3"},
		{"string with null bytes are allowed", "Hello\x00World", true, "Hello\x00World"},
		{`string with \n removed`, "Hello\nWorld", false, "HelloWorld"},
		{`string with \r\n removed`, "Hello\r\nWorld", false, "HelloWorld"},
		{"string with escape sequence removed (no newlines)", "Hello\x1B[31mWorld", false, "HelloWorld"},
		{"string with all control chars removed", "Hello\x01\x08\x09\x0A\x0DWorld", false, "HelloWorld"},
		{"empty string", "", true, ""},
		{"string with no control chars", "Hello World", true, "Hello World"},
		{"string with unicode", "Hello 世界\nTest", true, "Hello 世界\nTest"},
		{"CSI with multiple parameters", "Text\x1B[1;31;40mColored\x1B[0mNormal", true, "TextColoredNormal"},
		{"CSI SGR reset", "Before\x1B[mAfter", true, "BeforeAfter"},
		{"CSI cursor positioning", "Line\x1B[2J\x1B[H\x1B[10;5HText", true, "LineText"},
		{"OSC with BEL terminator", "Text\x1B]0;Window Title\x07More", true, "TextMore"},
		{"OSC with ST terminator", "Text\x1B]2;Title\x1B\\More", true, "TextMore"},
		{"Simple ESC sequences", "Text\x1B7Saved\x1B8Restored\x1BcReset", true, "TextSavedRestoredReset"},
		{"ESC D (Index)", "Line1\x1BDLine2", true, "Line1Line2"},
		{"ESC E (Next Line)", "Line1\x1BELine2", true, "Line1Line2"},
		{"ESC M (Reverse Index)", "Text\x1BMMore", true, "TextMore"},
		{"ESC N (SS2) and ESC O (SS3)", "Text\x1BNchar\x1BOanother", true, "Textcharanother"},
		{"Multiple escape sequences in sequence", "\x1B[1m\x1B[31m\x1B[44mBold Red on Blue\x1B[0m", true, "Bold Red on Blue"},
		{"CSI with question mark (private mode)", "Text\x1B[?25lHidden\x1B[?25hVisible", true, "TextHiddenVisible"},
		{"CSI erase sequences", "Clear\x1B[2J\x1B[K\x1B[1KScreen", true, "ClearScreen"},
		{"Hyperlink OSC 8", "Click \x1B]8;;https://example.com\x1B\\here\x1B]8;;\x1B\\ for link", true, "Click here for link"},
		{"Mixed content with complex sequences", "\x1B[1;33mWarning:\x1B[0m File\x1B[31m not\x1B[0m found\n\x1B[2KRetrying...", true, "Warning: File not found\nRetrying..."},
		{"malformed UTF-8 single byte", "Hello\xFFWorld", true, "Hello?World"},
		{"malformed UTF-8 multiple bytes", "Test\xC3\x28Data", true, "Test?(Data"},
		{"malformed UTF-8 with ANSI escape", "Line\xFF\x1B[31mColor\xFE", true, "Line?Color?"},
		{"valid UTF-8 unchanged", "Hello 世界 Test", true, "Hello 世界 Test"},
		{"mixed valid and invalid UTF-8", "Hello\xFF世界\xFETest", true, "Hello?世界?Test"},
		// beyond Composer's provider
		{"OSC without terminator before newline", "a\x1B]0;t\nb", true, "a0;t\nb"},
		{"ESC before newline kept", "a\x1B\nb", true, "a\x1B\nb"},
		{"trailing ESC kept", "a\x1B", true, "a\x1B"},
		{"ESC multibyte", "a\x1Bé!", true, "a!"},
	}
	for _, c := range cases {
		if got := Sanitize(c.input, c.allowNewlines); got != c.expected {
			t.Errorf("%s: Sanitize(%q) = %q, want %q", c.name, c.input, got, c.expected)
		}
	}

	arrays := []struct {
		name          string
		input         []string
		allowNewlines bool
		expected      []string
	}{
		{"array with newlines allowed", []string{"Hello\nWorld", "Foo\r\nBar"}, true, []string{"Hello\nWorld", "Foo\r\nBar"}},
		{"array with control chars removed", []string{"Hello\x1B[31mWorld", "Foo\x08Bar\r"}, true, []string{"HelloWorld", "FooBar"}},
		{"array with newlines removed", []string{"Hello\nWorld", "Foo\r\nBar"}, false, []string{"HelloWorld", "FooBar"}},
		{"array with all control chars removed", []string{"Test\x01\x0A", "Data\x1B[m\x0D"}, false, []string{"Test", "Data"}},
		{"empty array", []string{}, true, []string{}},
		{"malformed UTF-8 in array", []string{"Item\xFF", "Data\xC3\x28"}, true, []string{"Item?", "Data?("}},
	}
	for _, c := range arrays {
		if got := SanitizeMessages(c.input, c.allowNewlines); !reflect.DeepEqual(got, c.expected) {
			t.Errorf("%s: SanitizeMessages(%q) = %q, want %q", c.name, c.input, got, c.expected)
		}
	}
}

// TestEnsureValidUTF8 checks the maximal-subpart replacement against
// mb_convert_encoding($s, 'UTF-8', 'UTF-8') results from PHP 8.4.
func TestEnsureValidUTF8(t *testing.T) {
	for in, want := range map[string]string{
		"\xE4\xB8A":        "?A",
		"\xE4\xB8":         "?",
		"\xC3\x28":         "?(",
		"\xF0\x90\x80":     "?",
		"\xED\xA0\x80":     "???",
		"\xC0\x80":         "??",
		"\xF5\x80":         "??",
		"\x80\x80":         "??",
		"\xE0\x80\x80":     "???",
		"\xF4\x90\x80\x80": "????",
	} {
		if got := ensureValidUTF8(in); got != want {
			t.Errorf("ensureValidUTF8(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConsoleIO_Log(t *testing.T) {
	out := &fakeConsoleOutput{fakeOutput: &fakeOutput{verbosity: console.VerbosityDebug}}
	io := NewConsoleIO(&fakeInput{}, out, &fakeHelperSet{})
	io.Error("e", php.ArrayOf("a", "b/ü"))
	io.Warning("w", nil)
	io.Notice("n", php.NewArray())
	io.Info("i", nil)
	io.Debug("d", php.ListOf(1))
	io.Emergency("em", nil)
	io.Log("custom", "c", nil)

	want := []writeCall{
		{`<error>e {"a":"b/ü"}</error>`, true, console.VerbosityNormal},
		{"<warning>w</warning>", true, console.VerbosityNormal},
		{"<info>n</info>", true, console.VerbosityVerbose},
		{"<info>i</info>", true, console.VerbosityVeryVerbose},
		{"d [1]", true, console.VerbosityDebug},
		{"<error>em</error>", true, console.VerbosityNormal},
		{"c", true, console.VerbosityDebug},
	}
	if !reflect.DeepEqual(out.writes, want) {
		t.Errorf("writes = %#v", out.writes)
	}
}

func TestConsoleIO_WriteRawAndVerbosity(t *testing.T) {
	out := &fakeOutput{verbosity: console.VerbosityNormal}
	io := NewConsoleIO(&fakeInput{}, out, &fakeHelperSet{})
	io.WriteRaw("<info>\x1Braw</info>", true, Normal)
	io.Write("verbose", true, Verbose)
	io.Write("quiet\x1B[0m", false, Quiet)

	want := []writeCall{
		{"<info>\x1Braw</info>", true, console.VerbosityNormal | console.OutputRaw},
		{"quiet", false, console.VerbosityQuiet},
	}
	if !reflect.DeepEqual(out.writes, want) {
		t.Errorf("writes = %#v", out.writes)
	}
}

func TestConsoleIO_Timestamps(t *testing.T) {
	out := &fakeOutput{verbosity: console.VerbosityNormal}
	io := NewConsoleIO(&fakeInput{}, out, &fakeHelperSet{})
	io.EnableTimestamps("")
	io.Write("x", true, Normal)
	if !regexp.MustCompile(`^\[\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{3}[+-]\d\d:\d\d\] x$`).MatchString(out.writes[0].message) {
		t.Errorf("timestamped = %q", out.writes[0].message)
	}
}

// fakeConfig is a Composer\Config holding the defaults of new Config(false).
type fakeConfig struct {
	values *php.Array
	merges []string
}

func newFakeConfig() *fakeConfig {
	return &fakeConfig{values: php.ArrayOf(
		"bitbucket-oauth", php.NewArray(),
		"github-oauth", php.NewArray(),
		"gitlab-oauth", php.NewArray(),
		"gitlab-token", php.NewArray(),
		"forgejo-token", php.NewArray(),
		"http-basic", php.NewArray(),
		"bearer", php.NewArray(),
		"custom-headers", php.NewArray(),
		"client-certificate", php.NewArray(),
		"github-domains", php.ListOf("github.com"),
		"gitlab-domains", php.ListOf("gitlab.com"),
		"forgejo-domains", php.ListOf("codeberg.org"),
		"process-timeout", int64(300),
	)}
}

func (c *fakeConfig) Get(key string) any {
	v, _ := c.values.Get(key)

	return v
}

func (c *fakeConfig) Merge(config *php.Array, source string) {
	cfg, _ := config.GetArray("config")
	for k, v := range cfg.All() {
		c.values.SetKey(k, v)
	}
	j, _ := php.JSONEncode(config, php.JSONUnescapedSlashes)
	c.merges = append(c.merges, j+" "+source)
}

func (c *fakeConfig) set(key string, value any) { c.values.Set(key, value) }

func TestBaseIO_LoadConfigurationAcceptsValidGithubToken(t *testing.T) {
	tokens := map[string]string{
		"legacy 40-hex PAT": "8a7f2c1bdc4e9f06a3b7c2e9d4f1a8b6c5d7e0f2",
		"ghp_ flat token":   "ghp_n3K9wQ2eL5bV8mY1pX4cZ7aR0fT6sH3uJ8oI",
		"gho_ flat token":   "gho_M2pQ7vR4xL9eK6bN1cT8aZ0sJ3wY5fH7uG2d",
		"ghu_ flat token":   "ghu_R5tY8wA1xC4eK7bN0pV3mL6sH9uJ2gD5fQ8z",
		"ghs_ flat token":   "ghs_K7bN3pV5mL8eR2tY9wA1xC4sH6uJ0gD3fQ8z",
		"ghr_ flat token":   "ghr_X9aZ2sJ5wY8fH1uG4dR7bN0pV3mL6eK2tQ5c",
		// shivammathur/setup-php style: ghs_<id>_<base64url>.<base64url>.<base64url>
		"ghs_ structured installation token (jwt body)": "ghs_1234567890_eyJhbGciOiJSUzI1NiJ9.eyJpc3MiOiJjb21wb3NlciJ9.aB-cDef_GHIjkl-mnoPQR0123456",
		"github_pat_ fine-grained PAT":                  "github_pat_11ABCDEFG0aB1cD2eF3gH4_n3K9wQ2eL5bV8mY1pX4cZ7aR0fT6sH3uJ8oI2pQ7vR4xL9eK6bN",
	}
	for name, token := range tokens {
		io, err := NewBufferIO("", 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		config := newFakeConfig()
		config.set("github-oauth", php.ArrayOf("github.com", token))

		timeout := 0
		io.LoadConfiguration(config, func(v int) { timeout = v })

		auth := io.Authentication("github.com")
		if auth.Username == nil || *auth.Username != token || auth.Password == nil || *auth.Password != "x-oauth-basic" {
			t.Errorf("%s: auth = %#v", name, auth)
		}
		if timeout != 300 {
			t.Errorf("%s: timeout = %d", name, timeout)
		}
	}
}

func TestBaseIO_LoadConfiguration(t *testing.T) {
	io, err := NewBufferIO("", console.VerbosityDebug, nil)
	if err != nil {
		t.Fatal(err)
	}
	io.SetAuthentication("example.org", "old", nil)

	config := newFakeConfig()
	config.set("bitbucket-oauth", php.ArrayOf("bitbucket.org", php.ArrayOf("consumer-key", "k", "consumer-secret", "s")))
	config.set("github-oauth", php.ArrayOf("gh.example.com", "ghtoken"))
	config.set("gitlab-oauth", php.ArrayOf("gitlab.com", php.ArrayOf("token", "gltoken")))
	config.set("gitlab-token", php.ArrayOf("gl.example.com", "pt", "gl2.example.com", php.ArrayOf("username", "u", "token", "t")))
	config.set("forgejo-token", php.ArrayOf("codeberg.org", php.ArrayOf("username", "fu", "token", "ft")))
	config.set("http-basic", php.ArrayOf("example.org", php.ArrayOf("username", "user", "password", "pass")))
	config.set("bearer", php.ArrayOf("bearer.example.com", "btoken"))
	config.set("custom-headers", php.ArrayOf("headers.example.com", php.ListOf("X-A: b/c"), "null.example.com", nil))
	config.set("client-certificate", php.ArrayOf(
		"cert.example.com", php.ArrayOf("local_cert", "/c.pem", "passphrase", "p"),
		"nocert.example.com", php.ArrayOf("local_pk", "/k.pem"),
	))

	io.LoadConfiguration(config, nil)

	var got []string
	for _, a := range io.Authentications() {
		p := "null"
		if a.Password != nil {
			p = *a.Password
		}
		got = append(got, a.Repository+" "+*a.Username+" "+p)
	}
	want := []string{
		"example.org user pass",
		"bitbucket.org k s",
		"gh.example.com ghtoken x-oauth-basic",
		"gitlab.com gltoken oauth2",
		"gl.example.com pt private-token",
		"gl2.example.com u t",
		"codeberg.org fu ft",
		"bearer.example.com btoken bearer",
		`headers.example.com ["X-A: b\/c"] custom-headers`,
		`cert.example.com client-certificate {"local_cert":"\/c.pem","passphrase":"p"}`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("authentications =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	wantMerges := []string{
		`{"config":{"github-domains":["github.com","gh.example.com"]}} implicit-due-to-auth`,
		`{"config":{"gitlab-domains":["gitlab.com","gl.example.com"]}} implicit-due-to-auth`,
		`{"config":{"gitlab-domains":["gitlab.com","gl.example.com","gl2.example.com"]}} implicit-due-to-auth`,
	}
	if !reflect.DeepEqual(config.merges, wantMerges) {
		t.Errorf("merges = %q", config.merges)
	}

	wantOut := "gh.example.com is not in the configured github-domains, adding it implicitly as authentication is configured for this domain\n" +
		"gl.example.com is not in the configured gitlab-domains, adding it implicitly as authentication is configured for this domain\n" +
		"gl2.example.com is not in the configured gitlab-domains, adding it implicitly as authentication is configured for this domain\n" +
		// BufferIO's default formatter has no "warning" style, so the tag
		// is printed as is, as in Composer.
		"<warning>Warning: You should avoid overwriting already defined auth settings for example.org.</warning>\n" +
		"<warning>Warning: Client certificate configuration is missing key `local_cert` for nocert.example.com.</warning>\n"
	if got := io.Output(); got != wantOut {
		t.Errorf("output =\n%s\nwant\n%s", got, wantOut)
	}

	// Loading the same settings again is silent.
	io2, _ := NewBufferIO("", 0, nil)
	io2.SetAuthentication("example.org", "user", new("pass"))
	config2 := newFakeConfig()
	config2.set("http-basic", php.ArrayOf("example.org", php.ArrayOf("username", "user", "password", "pass")))
	io2.LoadConfiguration(config2, nil)
	if got := io2.Output(); got != "" {
		t.Errorf("unexpected output %q", got)
	}

	io2.ResetAuthentications()
	if len(io2.Authentications()) != 0 || io2.HasAuthentication("example.org") {
		t.Error("ResetAuthentications kept entries")
	}
}

// questionHelperImplemented reports whether console.QuestionHelper is the
// real port rather than the work-in-progress stand-in.
func questionHelperImplemented(t *testing.T) bool {
	t.Helper()
	in, err := console.NewStringInput("")
	if err != nil {
		t.Fatal(err)
	}
	in.SetInteractive(false)
	answer, _ := console.NewQuestionHelper().Ask(in, console.NewNullOutput(), console.NewQuestion("q", "d"))

	return answer == "d"
}

func TestBufferIO_SetUserInputs(t *testing.T) {
	if !questionHelperImplemented(t) {
		t.Skip("console.QuestionHelper is not ported yet")
	}
	io, err := NewBufferIO("", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	io.SetUserInputs([]string{"yes", "no", ""})

	if ok, err := io.AskConfirmation("Please say yes!", false); err != nil || !ok {
		t.Errorf("first answer = %v, %v", ok, err)
	}
	if ok, err := io.AskConfirmation("Now please say no!", true); err != nil || ok {
		t.Errorf("second answer = %v, %v", ok, err)
	}
	if got, err := io.Ask("Empty string last", "default"); err != nil || got != "default" {
		t.Errorf("third answer = %#v, %v", got, err)
	}
}

// TestBufferIO_Output checks getOutput()'s backspace normalisation against
// results of Composer's regex in PHP 8.4.
func TestBufferIO_Output(t *testing.T) {
	for _, c := range [][2]string{
		{"abc\b\b\bdef", "def"},
		{"Loading\b\b\b\b\b\b\bDone   \b\b\b\n", "Done\n\n"},
		{"a\b", ""},
		{"\b\bx", "x"},
		{"x\ny\bz", "x\nz"},
		{"<info>ab</info>\b\bok", "ok"},
		{"long text  \b\b\bshort\n", "long text\nshort\n"},
		{"\n\ba\bb", "\n\ba\nb"},
		{"ab\b\b\bc", "ab\nc"},
		{"no backspaces\n", "no backspaces\n"},
	} {
		if got := normalizeBackspaces(c[0]); got != c[1] {
			t.Errorf("normalizeBackspaces(%q) = %q, want %q", c[0], got, c[1])
		}
	}

	io, err := NewBufferIO("", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	io.Write("<info>Loading</info>", false, Normal)
	io.Write("\x1B[31mred", true, Normal)
	if got := io.Output(); got != "Loadingred\n" {
		t.Errorf("Output() = %q", got)
	}
	if io.IsDecorated() || io.IsInteractive() {
		t.Error("BufferIO must be undecorated and non-interactive")
	}
}

func TestNullIO(t *testing.T) {
	io := NewNullIO()

	if io.IsInteractive() {
		t.Error("IsInteractive")
	}
	if io.HasAuthentication("foo") {
		t.Error("HasAuthentication")
	}
	if v, _ := io.AskAndHideAnswer("foo"); v != nil {
		t.Error("AskAndHideAnswer")
	}
	if len(io.Authentications()) != 0 {
		t.Error("Authentications")
	}
	if auth := io.Authentication("foo"); auth.Username != nil || auth.Password != nil {
		t.Error("Authentication")
	}
	if v, _ := io.Ask("bar", "foo"); v != "foo" {
		t.Error("Ask")
	}
	if v, _ := io.AskConfirmation("bar", false); v {
		t.Error("AskConfirmation")
	}
	if v, _ := io.AskAndValidate("question", func(any) (any, error) { return true, nil }, 0, "foo"); v != "foo" {
		t.Error("AskAndValidate")
	}
	if v, _ := io.Select("question", php.ListOf("item1", "item2"), "1", 2, "foo", true); v != "1" {
		t.Error("Select")
	}
	// Logging goes nowhere.
	io.Error("x", nil)
}

func TestConsoleIO_ProgressBarAndTable(t *testing.T) {
	var stdout, stderr bytes.Buffer
	out := console.NewConsoleOutputStreams(&stdout, &stderr, console.VerbosityNormal, new(false), nil)
	in, _ := console.NewArrayInput(nil, nil)
	cio := NewConsoleIO(in, out, console.NewHelperSet())

	cio.ProgressBar(3).Start()
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "0/3") {
		t.Errorf("progress bar: stdout %q, stderr %q", stdout.String(), stderr.String())
	}
	table := cio.Table().SetHeaders([]any{"a"})
	if err := table.Render(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "| a |") {
		t.Errorf("table: stdout %q", stdout.String())
	}
}
