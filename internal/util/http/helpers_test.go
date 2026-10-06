package http

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
)

// fakeConfig is the PHPUnit Config mock of Composer's tests: Get answers
// from values (nil otherwise) and counts its calls.
type fakeConfig struct {
	values    map[string]any
	gets      map[string]int
	source    *fakeSource
	auth      *fakeSource
	localAuth *fakeSource

	authCalls, sourceCalls int
	prohibit               func(url string) error
}

func newFakeConfig(values map[string]any) *fakeConfig {
	if values == nil {
		values = map[string]any{}
	}

	return &fakeConfig{values: values, gets: map[string]int{}}
}

func (c *fakeConfig) Get(key string) any {
	c.gets[key]++

	return c.values[key]
}

func (c *fakeConfig) ProhibitURLByConfig(url string, _ io.IO, _ *php.Array) error {
	if c.prohibit != nil {
		return c.prohibit(url)
	}

	return nil
}

func (c *fakeConfig) ConfigSource() ConfigSource {
	c.sourceCalls++
	if c.source == nil {
		c.source = &fakeSource{name: "composer.json"}
	}

	return c.source
}

func (c *fakeConfig) AuthConfigSource() ConfigSource {
	c.authCalls++
	if c.auth == nil {
		c.auth = &fakeSource{name: "auth.json"}
	}

	return c.auth
}

func (c *fakeConfig) LocalAuthConfigSource() ConfigSource {
	if c.localAuth == nil {
		return nil
	}

	return c.localAuth
}

// fakeSource records the settings written to it.
type fakeSource struct {
	name     string
	nameRead int
	added    []string
	removed  []string
}

func (s *fakeSource) Name() string {
	s.nameRead++

	return s.name
}

func (s *fakeSource) AddConfigSetting(name string, value any) error {
	encoded, _ := php.JSONEncode(value, php.JSONUnescapedSlashes)
	s.added = append(s.added, name+"="+encoded)

	return nil
}

func (s *fakeSource) RemoveConfigSetting(name string) error {
	s.removed = append(s.removed, name)

	return nil
}

// list is a *php.Array list of strings.
func list(values ...string) *php.Array { return php.StringList(values) }

// ioMock ports tests/Composer/Test/Mock/IOMock.php: a BufferIO whose
// questions are answered from expectations, checked against the output by
// assertComplete.
type ioMock struct {
	*io.BufferIO
	expectations []map[string]any
	strict       bool
	authLog      [][3]any
}

func newIOMock(t *testing.T) *ioMock {
	t.Helper()

	b, err := io.NewBufferIO("", console.VerbosityDebug, nil)
	if err != nil {
		t.Fatal(err)
	}

	return &ioMock{BufferIO: b}
}

func (m *ioMock) expects(expectations []map[string]any, strict bool) {
	m.expectations = expectations

	var inputs []string

	for _, expect := range expectations {
		if _, ok := expect["ask"]; ok {
			inputs = append(inputs, expect["reply"].(string))
		}
	}

	if len(inputs) > 0 {
		m.SetUserInputs(inputs)
	}

	m.strict = strict
}

func (m *ioMock) Ask(question string, def any) (any, error) {
	return m.BufferIO.Ask(strings.TrimRight(question, "\r\n")+"\n", def)
}

func (m *ioMock) AskConfirmation(question string, def bool) (bool, error) {
	return m.BufferIO.AskConfirmation(strings.TrimRight(question, "\r\n")+"\n", def)
}

func (m *ioMock) AskAndValidate(question string, validator console.Validator, attempts int, def any) (any, error) {
	return m.BufferIO.AskAndValidate(strings.TrimRight(question, "\r\n")+"\n", validator, attempts, def)
}

// AskAndHideAnswer does not hide the answer in tests, like IOMock.
func (m *ioMock) AskAndHideAnswer(question string) (any, error) {
	return m.BufferIO.Ask(strings.TrimRight(question, "\r\n")+"\n", nil)
}

func (m *ioMock) SetAuthentication(repositoryName, username string, password *string) {
	var pass any
	if password != nil {
		pass = *password
	}

	m.authLog = append(m.authLog, [3]any{repositoryName, username, pass})
	m.BufferIO.SetAuthentication(repositoryName, username, password)
}

func (m *ioMock) assertComplete(t *testing.T) {
	t.Helper()

	output := php.NormalizeEOL(m.Output())

	if m.expectations == nil {
		return
	}

	if len(m.expectations) == 0 {
		if output != "" && m.strict {
			t.Fatalf("There was strictly no output expected but some output occurred: %s", output)
		}

		return
	}

	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")

outer:
	for _, expect := range m.expectations {
		if auth, ok := expect["auth"]; ok {
			want := fmt.Sprint(auth)

			for len(m.authLog) > 0 {
				got := m.authLog[0]
				m.authLog = m.authLog[1:]

				if fmt.Sprint(got[:]) == want {
					continue outer
				}

				if m.strict {
					t.Fatalf("IO authentication mismatch. Expected:\n%s\nGot:\n%v", want, got)
				}
			}

			t.Fatalf("Expected %q auth to be set but there are no setAuthentication calls left to consume.", want)
		}

		var pattern string

		switch {
		case expect["ask"] != nil:
			pattern = "{^" + php.PregQuote(expect["ask"].(string), "") + "$}"
		case expect["regex"] == true:
			pattern = expect["text"].(string)
		default:
			pattern = "{^" + php.PregQuote(expect["text"].(string), "") + "$}"
		}

		for len(lines) > 0 {
			line := lines[0]
			lines = lines[1:]

			if ok, err := php.PregIsMatch(pattern, line); err == nil && ok {
				continue outer
			}

			if m.strict {
				t.Fatalf("IO output mismatch. Expected:\n%v\nGot:\n%s", expect, line)
			}
		}

		t.Fatalf("Expected %v to be output still but there is no output left to consume. Complete output:\n%s", expect, output)
	}
}

// fakeIO is the PHPUnit IOInterface mock: every method can be scripted
// and is counted.
type fakeIO struct {
	*io.NullIO

	interactive bool
	hasAuth     func(origin string) bool
	getAuth     func(origin string) io.Authentication
	ask         func(question string) any
	validate    func(question string, validator console.Validator, attempts int, def any) (any, error)

	calls  map[string]int
	args   map[string][]string
	writes []string
	auths  [][3]any
}

func newFakeIO() *fakeIO {
	return &fakeIO{NullIO: io.NewNullIO(), calls: map[string]int{}, args: map[string][]string{}}
}

func (f *fakeIO) record(name, arg string) {
	f.calls[name]++
	f.args[name] = append(f.args[name], arg)
}

func (f *fakeIO) IsInteractive() bool { return f.interactive }

func (f *fakeIO) HasAuthentication(origin string) bool {
	f.record("hasAuthentication", origin)
	if f.hasAuth != nil {
		return f.hasAuth(origin)
	}

	return false
}

func (f *fakeIO) Authentication(origin string) io.Authentication {
	f.record("getAuthentication", origin)
	if f.getAuth != nil {
		return f.getAuth(origin)
	}

	return io.Authentication{}
}

func (f *fakeIO) SetAuthentication(origin, username string, password *string) {
	f.record("setAuthentication", origin)

	var pass any
	if password != nil {
		pass = *password
	}

	f.auths = append(f.auths, [3]any{origin, username, pass})
}

func (f *fakeIO) WriteError(message string, newline bool, verbosity io.Verbosity) {
	f.record("writeError", message)
	f.writes = append(f.writes, fmt.Sprintf("%s|%v|%d", message, newline, verbosity))
}

func (f *fakeIO) WriteErrorMessages(messages []string, newline bool, verbosity io.Verbosity) {
	for _, m := range messages {
		f.WriteError(m, newline, verbosity)
	}
}

func (f *fakeIO) OverwriteError(message string, _ bool, _ int, _ io.Verbosity) {
	f.record("overwriteError", message)
}

func (f *fakeIO) Ask(question string, def any) (any, error) {
	f.record("ask", question)
	if f.ask != nil {
		return f.ask(question), nil
	}

	return def, nil
}

func (f *fakeIO) AskAndHideAnswer(question string) (any, error) {
	f.record("askAndHideAnswer", question)
	if f.ask != nil {
		return f.ask(question), nil
	}

	return nil, nil
}

func (f *fakeIO) AskAndValidate(question string, validator console.Validator, attempts int, def any) (any, error) {
	f.record("askAndValidate", question)
	if f.validate != nil {
		return f.validate(question, validator, attempts, def)
	}

	return def, nil
}

// auth builds an io.Authentication from strings, "\x00" standing for null.
func auth(username, password string) io.Authentication {
	var a io.Authentication
	if username != "\x00" {
		a.Username = &username
	}

	if password != "\x00" {
		a.Password = &password
	}

	return a
}

// jsonOf renders a php value for comparisons.
func jsonOf(t *testing.T, v any) string {
	t.Helper()

	s, err := php.JSONEncode(v, php.JSONUnescapedSlashes)
	if err != nil {
		t.Fatal(err)
	}

	return s
}

// mustJSON decodes JSON into php values.
func mustJSON(t *testing.T, s string) any {
	t.Helper()

	v, err := php.JSONDecode(s, true)
	if err != nil {
		t.Fatal(err)
	}

	return v
}

type ioAuth = io.Authentication
