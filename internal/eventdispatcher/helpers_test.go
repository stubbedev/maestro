package eventdispatcher

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
)

// fakeComposer is the Composer instance of createComposerInstance().
type fakeComposer struct {
	root     pkg.RootPackageInterface
	config   *config.Config
	autoload *fakeAutoloader
}

func (c *fakeComposer) Package() pkg.RootPackageInterface  { return c.root }
func (c *fakeComposer) Config() *config.Config             { return c.config }
func (c *fakeComposer) ScriptAutoloader() ScriptAutoloader { return c.autoload }

// fakePartialComposer is a PartialComposer (no autoload generator).
type fakePartialComposer struct {
	root   pkg.RootPackageInterface
	config *config.Config
}

func (c *fakePartialComposer) Package() pkg.RootPackageInterface { return c.root }
func (c *fakePartialComposer) Config() *config.Config            { return c.config }

// fakeAutoloader records makeAutoloader's calls on the generator.
type fakeAutoloader struct {
	packages []pkg.PackageInterface
	devModes []bool
	created  int
}

func (a *fakeAutoloader) CanonicalLocalPackages() ([]pkg.PackageInterface, error) {
	return a.packages, nil
}

func (a *fakeAutoloader) SetDevMode(devMode bool) { a.devModes = append(a.devModes, devMode) }

func (a *fakeAutoloader) CreateLoader([]pkg.PackageInterface) (*LoaderContents, error) {
	a.created++

	return &LoaderContents{VendorDir: "vendor"}, nil
}

// createComposerInstance mirrors the PHP test helper: a Composer with a
// default Config, an empty root package and an empty local repository.
func createComposerInstance() *fakeComposer {
	return &fakeComposer{
		root:     pkg.NewRootPackage("__root__", "1.0.0.0", "1.0.0"),
		config:   config.New(true, ""),
		autoload: &fakeAutoloader{},
	}
}

// fakePHP is the PHP the dispatcher considers itself running on.
type fakePHP struct{}

func (fakePHP) Binary() (string, bool) { return "/usr/bin/php", true }

func (fakePHP) IniGet(name string) (string, error) {
	return map[string]string{"allow_url_fopen": "1", "disable_functions": "", "memory_limit": "1536M"}[name], nil
}

// phpFunc is a PHP method of the fake runtime: it returns whether it
// returned false.
type phpFunc func(ev Event) (bool, error)

// fakeRuntime implements ScriptRuntime with Go stand-ins for PHP classes.
type fakeRuntime struct {
	// methods maps "Class::method" to its implementation; a class is
	// autoloadable when it has a method here or in commands.
	methods map[string]phpFunc
	// callables maps a PHP callable handle to its implementation.
	callables map[Handle]phpFunc
	// commands maps a command class to its run(); notCommands lists
	// classes that exist but do not extend Command.
	commands    map[string]func(input string) (int, error)
	notCommands map[string]bool

	installed []*LoaderContents
	brackets  []string
}

func newFakeRuntime() *fakeRuntime {
	return &fakeRuntime{methods: map[string]phpFunc{}, callables: map[Handle]phpFunc{}, commands: map[string]func(string) (int, error){}, notCommands: map[string]bool{}}
}

func (r *fakeRuntime) classExists(class string) bool {
	if _, ok := r.commands[class]; ok || r.notCommands[class] {
		return true
	}
	for k := range r.methods {
		if strings.HasPrefix(k, class+"::") {
			return true
		}
	}

	return false
}

func (r *fakeRuntime) CallListener(l PHPCallable, ev Event, before func()) (ScriptStatus, bool, error) {
	fn, ok := r.callables[l.H]
	if !ok {
		return StatusNotCallable, false, nil
	}
	before()
	retFalse, err := fn(ev)

	return StatusOK, retFalse, err
}

func (r *fakeRuntime) CallPHPScript(className, methodName string, ev Event, before func()) (ScriptStatus, bool, error) {
	if !r.classExists(className) {
		return StatusNotAutoloadable, false, nil
	}
	fn, ok := r.methods[className+"::"+methodName]
	if !ok {
		return StatusNotCallable, false, nil
	}
	before()
	retFalse, err := fn(ev)

	return StatusOK, retFalse, err
}

func (r *fakeRuntime) RunCommandClass(className string, _ Event, input string, before func() bool) (ScriptStatus, int, error) {
	if !r.classExists(className) {
		return StatusNotAutoloadable, 0, nil
	}
	run, ok := r.commands[className]
	if !ok {
		return StatusNotCommand, 0, nil
	}
	if !before() {
		return StatusSkipped, 0, nil
	}
	code, err := run(input)

	return StatusOK, code, err
}

func (r *fakeRuntime) InstallAutoloader(loader *LoaderContents) error {
	r.installed = append(r.installed, loader)

	return nil
}

func (r *fakeRuntime) DispatchBegin(depth int) error {
	r.brackets = append(r.brackets, "begin "+strconv.Itoa(depth))

	return nil
}

func (r *fakeRuntime) DispatchEnd(depth int) error {
	r.brackets = append(r.brackets, "end "+strconv.Itoa(depth))

	return nil
}

// newDispatcher is new EventDispatcher($composer, $io, $process) with the
// fake PHP; it restores the environment variables dispatching may change.
func newDispatcher(t *testing.T, composer PartialComposer, ioi io.IO, process Process) *EventDispatcher {
	t.Helper()
	keepEnv(t, "PATH", "PHP_BINARY")

	d := New(composer, ioi, process)
	d.SetPHP(fakePHP{})

	return d
}

// keepEnv restores the variables when the test ends.
func keepEnv(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		if v, ok := os.LookupEnv(name); ok {
			t.Setenv(name, v)
		} else {
			t.Setenv(name, "")
			os.Unsetenv(name)
		}
	}
}

// listenersReturning is the getListeners mock returning listeners.
func listenersReturning(listeners ...Listener) func(Event) []Listener {
	return func(Event) []Listener { return listeners }
}

func scripts(s ...string) []Listener {
	l := make([]Listener, len(s))
	for i, v := range s {
		l[i] = Script(v)
	}

	return l
}

// bufferIO is new BufferIO(”, $verbosity).
func bufferIO(t *testing.T, verbosity int) *io.BufferIO {
	t.Helper()
	b, err := io.NewBufferIO("", verbosity, nil)
	if err != nil {
		t.Fatal(err)
	}

	return b
}

// ioMock is getIOMock($verbosity): a BufferIO whose output the test
// compares with the expected lines.
func ioMock(t *testing.T) *io.BufferIO { return bufferIO(t, console.VerbosityNormal) }

// expectOutput checks the IOMock expectations (strict): exactly these
// lines were written.
func expectOutput(t *testing.T, b *io.BufferIO, lines ...string) {
	t.Helper()
	want := strings.Join(lines, "\n")
	if len(lines) > 0 {
		want += "\n"
	}
	if got := php.NormalizeEOL(b.Output()); got != want {
		t.Errorf("output:\n%q\nwant:\n%q", got, want)
	}
}

// recordingIO is a mocked IOInterface recording writeError and writeRaw.
type recordingIO struct {
	*io.NullIO
	errors []string
	raws   []string
}

func newRecordingIO() *recordingIO { return &recordingIO{NullIO: io.NewNullIO()} }

func (r *recordingIO) WriteError(message string, _ bool, _ io.Verbosity) {
	r.errors = append(r.errors, message)
}

func (r *recordingIO) WriteRaw(message string, newline bool, _ io.Verbosity) {
	if newline {
		message += "<newline>"
	}
	r.raws = append(r.raws, message)
}
