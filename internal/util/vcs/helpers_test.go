package vcs

import (
	"testing"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util/http"
	"github.com/stubbedev/maestro/internal/util/processmock"
)

// fakeConfig is the PHPUnit Config mock of Composer's tests: Get answers
// from values (nil otherwise).
type fakeConfig struct {
	values      map[string]any
	auth, local *fakeSource
	source      *fakeSource
}

func newFakeConfig(values map[string]any) *fakeConfig {
	return &fakeConfig{values: values, auth: &fakeSource{name: "auth.json"}, source: &fakeSource{name: "config.json"}}
}

func (c *fakeConfig) Get(key string) any { return c.values[key] }

func (c *fakeConfig) ProhibitURLByConfig(string, io.IO, *php.Array) error { return nil }

func (c *fakeConfig) ConfigSource() http.ConfigSource     { return c.source }
func (c *fakeConfig) AuthConfigSource() http.ConfigSource { return c.auth }

func (c *fakeConfig) LocalAuthConfigSource() http.ConfigSource {
	if c.local == nil {
		return nil
	}

	return c.local
}

type fakeSource struct {
	name    string
	added   []string
	removed []string
}

func (s *fakeSource) Name() string { return s.name }

func (s *fakeSource) AddConfigSetting(name string, _ any) error {
	s.added = append(s.added, name)

	return nil
}

func (s *fakeSource) RemoveConfigSetting(name string) error {
	s.removed = append(s.removed, name)

	return nil
}

// fakeIO is the PHPUnit IOInterface mock: authentication lives in auths
// unless hasAuth/getAuth are scripted; questions are answered by ask.
type fakeIO struct {
	*io.NullIO

	interactive bool
	auths       map[string]io.Authentication
	hasAuth     func(origin string) bool
	ask         func(question string) any
	confirm     bool
	writes      []string
	hasAuthArgs []string
}

func newFakeIO() *fakeIO {
	return &fakeIO{NullIO: io.NewNullIO(), auths: map[string]io.Authentication{}}
}

func (f *fakeIO) IsInteractive() bool { return f.interactive }

func (f *fakeIO) HasAuthentication(origin string) bool {
	f.hasAuthArgs = append(f.hasAuthArgs, origin)
	if f.hasAuth != nil {
		return f.hasAuth(origin)
	}

	_, ok := f.auths[origin]

	return ok
}

func (f *fakeIO) Authentication(origin string) io.Authentication { return f.auths[origin] }

func (f *fakeIO) SetAuthentication(origin, username string, password *string) {
	f.auths[origin] = io.Authentication{Username: &username, Password: password}
}

func (f *fakeIO) WriteError(message string, _ bool, _ io.Verbosity) {
	f.writes = append(f.writes, message)
}

func (f *fakeIO) Ask(question string, def any) (any, error) {
	if f.ask != nil {
		return f.ask(question), nil
	}

	return def, nil
}

func (f *fakeIO) AskAndHideAnswer(question string) (any, error) {
	if f.ask != nil {
		return f.ask(question), nil
	}

	return nil, nil
}

func (f *fakeIO) AskConfirmation(string, bool) (bool, error) { return f.confirm, nil }

func authOf(username, password string) io.Authentication {
	return io.Authentication{Username: &username, Password: &password}
}

func assertComplete(t *testing.T, m *processmock.Mock) {
	t.Helper()

	if err := m.AssertComplete(); err != nil {
		t.Fatal(err)
	}
}

func list(values ...string) *php.Array { return php.StringList(values) }
