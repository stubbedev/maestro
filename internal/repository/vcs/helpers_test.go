package vcs

import (
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
	"github.com/stubbedev/maestro/internal/util/http/httpmock"
	"github.com/stubbedev/maestro/internal/util/processmock"
	uvcs "github.com/stubbedev/maestro/internal/util/vcs"
)

// newConfig is Composer's tests' new Config() merged with ['config' =>
// ['home' => $home] + extra].
func newConfig(t *testing.T, home string, extra ...any) *config.Config {
	t.Helper()

	cfg := config.New(false, "")
	settings := php.ArrayOf(append([]any{"home", home}, extra...)...)

	if err := cfg.Merge(php.ArrayOf("config", settings), config.SourceUnknown); err != nil {
		t.Fatal(err)
	}

	return cfg
}

// testIO is the IOInterface mock of Composer's tests: it records the
// messages written to stderr and answers hidden questions from replies.
type testIO struct {
	*io.NullIO

	interactive, verbose, veryVerbose bool

	mu      sync.Mutex
	errors  []string
	asked   []string
	replies []string
}

func newTestIO() *testIO { return &testIO{NullIO: io.NewNullIO()} }

func (t *testIO) IsInteractive() bool { return t.interactive }
func (t *testIO) IsVerbose() bool     { return t.verbose || t.veryVerbose }
func (t *testIO) IsVeryVerbose() bool { return t.veryVerbose }

func (t *testIO) WriteError(message string, _ bool, verbosity io.Verbosity) {
	switch {
	case verbosity <= io.Normal:
	case verbosity == io.Verbose && t.IsVerbose():
	case verbosity == io.VeryVerbose && t.veryVerbose:
	default:
		return
	}

	t.mu.Lock()
	t.errors = append(t.errors, message)
	t.mu.Unlock()
}

func (t *testIO) OverwriteError(message string, _ bool, _ int, _ io.Verbosity) {
	t.mu.Lock()
	t.errors = append(t.errors, "overwrite:"+message)
	t.mu.Unlock()
}

func (t *testIO) AskAndHideAnswer(question string) (any, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.asked = append(t.asked, question)
	if len(t.replies) == 0 {
		return nil, nil
	}

	reply := t.replies[0]
	t.replies = t.replies[1:]

	return reply, nil
}

func (t *testIO) messages() []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return slices.Clone(t.errors)
}

// nopSource is a ConfigSourceInterface mock.
type nopSource struct{ name string }

func (nopSource) AddRepository(string, any, bool) error           { return nil }
func (nopSource) InsertRepository(string, any, string, int) error { return nil }
func (nopSource) SetRepositoryURL(string, string) error           { return nil }
func (nopSource) RemoveRepository(string) error                   { return nil }
func (nopSource) AddConfigSetting(string, any) error              { return nil }
func (nopSource) RemoveConfigSetting(string) error                { return nil }
func (nopSource) AddProperty(string, any) error                   { return nil }
func (nopSource) RemoveProperty(string) error                     { return nil }
func (nopSource) AddLink(string, string, string) error            { return nil }
func (nopSource) RemoveLink(string, string) error                 { return nil }
func (s nopSource) Name() string                                  { return s.name }

var _ config.ConfigSource = nopSource{}

// deps are the driver collaborators of a test.
func deps(ioi io.IO, cfg *config.Config, h http.Getter, p uvcs.Process) Deps {
	return Deps{IO: ioi, Config: cfg.ForHTTP(), HTTPDownloader: h, Process: p}
}

// pinGitVersion pins the process-wide git version, as the static cache
// of Git::getVersion is warm in Composer's test runs. Tests calling it
// must not run in parallel.
func pinGitVersion(t *testing.T) {
	t.Helper()
	uvcs.SetVersion("2.45.0", true)
	t.Cleanup(func() { uvcs.SetVersion("", false) })
}

func assertProcessComplete(t *testing.T, p *processmock.Mock) {
	t.Helper()

	if err := p.AssertComplete(); err != nil {
		t.Fatal(err)
	}
}

func assertHTTPComplete(t *testing.T, d *httpmock.Downloader) {
	t.Helper()

	if err := d.AssertComplete(); err != nil {
		t.Fatal(err)
	}
}

func noErr(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
}

// expectError fails unless err is (or wraps) a T with message msg ("" for
// any).
func expectError[T error](t *testing.T, err error, msg string) {
	t.Helper()

	e, ok := errors.AsType[T](err)
	if !ok {
		t.Fatalf("got error %v (%T), want %T", err, err, e)
	}

	if msg != "" && err.Error() != msg {
		t.Fatalf("got message %q, want %q", err.Error(), msg)
	}
}

// expectRuntimeError fails unless err is a \RuntimeException.
func expectRuntimeError(t *testing.T, err error, msg string) {
	t.Helper()

	if !phperr.InstanceOf(err, "RuntimeException") {
		t.Fatalf("got error %v (%T), want a RuntimeException", err, err)
	}

	if msg != "" && err.Error() != msg {
		t.Fatalf("got message %q, want %q", err.Error(), msg)
	}
}

// assertArray compares a against the JSON encoding of want.
func assertArray(t *testing.T, a *php.Array, want string) {
	t.Helper()

	got, err := php.JSONEncode(a, php.JSONUnescapedSlashes)
	noErr(t, err)

	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// strMap builds a branch/tag map.
func strMap(kv ...string) *php.Array {
	a := php.NewArray()
	for i := 0; i+1 < len(kv); i += 2 {
		a.Set(kv[i], kv[i+1])
	}

	return a
}

// invalidArgument is \InvalidArgumentException.
type invalidArgument = util.InvalidArgumentError
