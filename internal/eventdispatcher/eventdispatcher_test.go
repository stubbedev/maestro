// Ports tests/Composer/Test/EventDispatcher/EventDispatcherTest.php. The
// PHP callables of the test class (EventDispatcherTest::call, someMethod,
// ...) run in fakeRuntime; PHPUnit's getListeners mocks set listenersFor.

package eventdispatcher

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/script"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
	"github.com/stubbedev/maestro/internal/util/processmock"
)

const testClass = `Composer\Test\EventDispatcher\EventDispatcherTest`

// testClassRuntime is the runtime holding EventDispatcherTest's static
// methods.
func testClassRuntime(t *testing.T, binDir string) *fakeRuntime {
	t.Helper()
	rt := newFakeRuntime()
	rt.methods[testClass+"::call"] = func(Event) (bool, error) {
		return false, &Error{Class: "RuntimeException"}
	}
	rt.methods[testClass+"::someMethod"] = func(Event) (bool, error) { return false, nil }
	rt.methods[testClass+"::someMethod2"] = func(Event) (bool, error) { return false, nil }
	rt.methods[testClass+"::getTestEnv"] = func(Event) (bool, error) {
		if v, _ := os.LookupEnv("ABC"); v != "123" {
			return false, errors.New("getenv() did not return the expected value. expected 123 got " + v)
		}

		return false, nil
	}
	rt.methods[testClass+"::createsVendorBinFolderChecksEnvDoesNotContainsBin"] = func(Event) (bool, error) {
		if err := os.MkdirAll(binDir, 0o700); err != nil {
			return false, err
		}
		if strings.Contains(os.Getenv("PATH"), binDir) {
			t.Errorf("PATH %q contains %q", os.Getenv("PATH"), binDir)
		}

		return false, nil
	}
	rt.methods[testClass+"::createsVendorBinFolderChecksEnvContainsBin"] = func(Event) (bool, error) {
		if !strings.Contains(os.Getenv("PATH"), binDir) {
			t.Errorf("PATH %q does not contain %q", os.Getenv("PATH"), binDir)
		}

		return false, nil
	}

	return rt
}

func TestEventDispatcher_ListenerExceptionsAreCaught(t *testing.T) {
	out := ioMock(t)
	d := newDispatcher(t, createComposerInstance(), out, nil)
	d.SetScriptRuntime(testClassRuntime(t, ""))
	d.listenersFor = listenersReturning(Script(testClass + "::call"))

	_, err := d.DispatchScript(script.PostInstallCmd, false, nil, nil)

	var e *Error
	if !errors.As(err, &e) || e.Class != "RuntimeException" {
		t.Fatalf("err = %v, want the RuntimeException", err)
	}
	expectOutput(t, out,
		"> "+testClass+"::call",
		"Script "+testClass+"::call handling the post-install-cmd event terminated with an exception",
	)
}

func TestEventDispatcher_DispatcherCanExecuteSingleCommandLineScript(t *testing.T) {
	for _, command := range []string{"phpunit", "echo foo", "echo -n foo"} {
		t.Run(command, func(t *testing.T) {
			process := processmock.New()
			process.Expects([]processmock.Expectation{processmock.Shell(command)}, true, nil)

			d := newDispatcher(t, createComposerInstance(), newRecordingIO(), process)
			d.listenersFor = listenersReturning(Script(command))

			if _, err := d.DispatchScript(script.PostInstallCmd, false, nil, nil); err != nil {
				t.Fatal(err)
			}
			if err := process.AssertComplete(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEventDispatcher_DispatcherPassDevModeToAutoloadGeneratorForScriptEvents(t *testing.T) {
	for _, devMode := range []bool{true, false} {
		t.Run(map[bool]string{true: "dev", false: "no-dev"}[devMode], func(t *testing.T) {
			composer := createComposerInstance()
			composer.root.SetScripts(php.ArrayOf("scriptName", php.ListOf("ClassName::testMethod")))

			d := newDispatcher(t, composer, newRecordingIO(), processmock.New())
			d.SetScriptRuntime(newFakeRuntime())

			event := NewScriptEvent("scriptName", composer, newRecordingIO(), devMode, nil, nil)
			if _, err := d.Dispatch("scriptName", event); err != nil {
				t.Fatal(err)
			}

			if len(composer.autoload.devModes) == 0 || composer.autoload.devModes[0] != devMode {
				t.Errorf("setDevMode calls = %v, want %v", composer.autoload.devModes, devMode)
			}
		})
	}
}

func TestEventDispatcher_DispatcherRemoveListener(t *testing.T) {
	composer := createComposerInstance()
	out := bufferIO(t, console.VerbosityVerbose)
	d := newDispatcher(t, composer, out, processmock.New())

	rt := testClassRuntime(t, "")
	const this Handle = 1
	listener := PHPCallable{H: 10, ObjectH: this, Class: testClass, Method: "someMethod"}
	listener2 := PHPCallable{H: 11, ObjectH: this, Class: testClass, Method: "someMethod2"}
	listener3 := Script(testClass + "::someMethod")
	rt.callables[listener.H] = rt.methods[testClass+"::someMethod"]
	rt.callables[listener2.H] = rt.methods[testClass+"::someMethod2"]
	d.SetScriptRuntime(rt)

	d.AddListener("ev1", listener, 0)
	d.AddListener("ev1", listener, 1)
	d.AddListener("ev1", listener2, 1)
	d.AddListener("ev1", listener3, 0)
	d.AddListener("ev2", listener3, 0)
	d.AddListener("ev2", listener, 0)
	for _, ev := range []string{"ev1", "ev2"} {
		if _, err := d.Dispatch(ev, nil); err != nil {
			t.Fatal(err)
		}
	}

	expected := "> ev1: " + testClass + "->someMethod\n" +
		"> ev1: " + testClass + "->someMethod2\n" +
		"> ev1: " + testClass + "->someMethod\n" +
		"> ev1: " + testClass + "::someMethod\n" +
		"> ev2: " + testClass + "::someMethod\n" +
		"> ev2: " + testClass + "->someMethod\n"
	if got := out.Output(); got != expected {
		t.Fatalf("output:\n%s\nwant:\n%s", got, expected)
	}

	d.RemoveListener(MatchObject(this))
	for _, ev := range []string{"ev1", "ev2"} {
		if _, err := d.Dispatch(ev, nil); err != nil {
			t.Fatal(err)
		}
	}

	expected += "> ev1: " + testClass + "::someMethod\n" +
		"> ev2: " + testClass + "::someMethod\n"
	if got := out.Output(); got != expected {
		t.Fatalf("output:\n%s\nwant:\n%s", got, expected)
	}
}

func TestEventDispatcher_DispatcherCanExecuteCliAndPhpInSameEventScriptStack(t *testing.T) {
	process := processmock.New()
	process.Expects([]processmock.Expectation{processmock.Shell("echo -n foo"), processmock.Shell("echo -n bar")}, true, nil)

	out := bufferIO(t, console.VerbosityVerbose)
	d := newDispatcher(t, createComposerInstance(), out, process)
	d.SetScriptRuntime(testClassRuntime(t, ""))
	d.listenersFor = listenersReturning(scripts("echo -n foo", testClass+"::someMethod", "echo -n bar")...)

	if _, err := d.DispatchScript(script.PostInstallCmd, false, nil, nil); err != nil {
		t.Fatal(err)
	}

	expectOutput(t, out,
		"> post-install-cmd: echo -n foo",
		"> post-install-cmd: "+testClass+"::someMethod",
		"> post-install-cmd: echo -n bar",
	)
	if err := process.AssertComplete(); err != nil {
		t.Fatal(err)
	}
}

func TestEventDispatcher_DispatcherCanPutEnv(t *testing.T) {
	keepEnv(t, "ABC")
	out := bufferIO(t, console.VerbosityVerbose)
	d := newDispatcher(t, createComposerInstance(), out, processmock.New())
	d.SetScriptRuntime(testClassRuntime(t, ""))
	d.listenersFor = listenersReturning(scripts("@putenv ABC=123", testClass+"::getTestEnv")...)

	if _, err := d.DispatchScript(script.PostInstallCmd, false, nil, nil); err != nil {
		t.Fatal(err)
	}

	expectOutput(t, out,
		"> post-install-cmd: @putenv ABC=123",
		"> post-install-cmd: "+testClass+"::getTestEnv",
	)
}

func TestEventDispatcher_DispatcherAppendsDirBinOnPathForEveryListener(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	binDir := filepath.Join(dir, "vendor", "bin")
	t.Setenv("COMPOSER_BIN_DIR", binDir)

	out := bufferIO(t, console.VerbosityVerbose)
	d := newDispatcher(t, createComposerInstance(), out, processmock.New())
	d.SetScriptRuntime(testClassRuntime(t, binDir))
	d.listenersFor = listenersReturning(scripts(
		testClass+"::createsVendorBinFolderChecksEnvDoesNotContainsBin",
		testClass+"::createsVendorBinFolderChecksEnvContainsBin",
	)...)

	if _, err := d.DispatchScript(script.PostInstallCmd, false, nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestEventDispatcher_DispatcherSupportForAdditionalArgs(t *testing.T) {
	process := processmock.New()
	out := bufferIO(t, console.VerbosityVerbose)
	d := newDispatcher(t, createComposerInstance(), out, process)
	d.listenersFor = listenersReturning(scripts(
		"echo -n foo @no_additional_args",
		"@php foo.php @additional_args then the rest",
		"echo -n bar",
	)...)

	phpCmd, err := d.getPhpExecCommand()
	if err != nil {
		t.Fatal(err)
	}

	args := util.Escape("ARG") + " " + util.Escape("ARG2") + " " + util.Escape("--arg")
	process.Expects([]processmock.Expectation{
		processmock.Shell("echo -n foo"),
		processmock.Shell(phpCmd + " foo.php " + args + " then the rest"),
		processmock.Shell("echo -n bar " + args),
	}, true, nil)

	if _, err := d.DispatchScript(script.PostInstallCmd, false, []string{"ARG", "ARG2", "--arg"}, nil); err != nil {
		t.Fatal(err)
	}

	expectOutput(t, out,
		"> post-install-cmd: echo -n foo",
		"> post-install-cmd: @php foo.php "+args+" then the rest",
		"> post-install-cmd: echo -n bar "+args,
	)
	if err := process.AssertComplete(); err != nil {
		t.Fatal(err)
	}
}

// scriptGroups is the getListeners callback of the script group tests.
func scriptGroups(groups map[string][]string) func(Event) []Listener {
	return func(ev Event) []Listener { return scripts(groups[ev.Name()]...) }
}

func TestEventDispatcher_DispatcherCanExecuteComposerScriptGroups(t *testing.T) {
	process := processmock.New()
	process.Expects([]processmock.Expectation{processmock.Shell("echo -n foo"), processmock.Shell("echo -n baz"), processmock.Shell("echo -n bar")}, true, nil)

	composer := createComposerInstance()
	out := bufferIO(t, console.VerbosityVerbose)
	d := newDispatcher(t, composer, out, process)
	d.listenersFor = scriptGroups(map[string][]string{
		"root":     {"@group"},
		"group":    {"echo -n foo", "@subgroup", "echo -n bar"},
		"subgroup": {"echo -n baz"},
	})

	if _, err := d.Dispatch("root", NewScriptEvent("root", composer, out, false, nil, nil)); err != nil {
		t.Fatal(err)
	}

	expectOutput(t, out,
		"> root: @group",
		"> group: echo -n foo",
		"> group: @subgroup",
		"> subgroup: echo -n baz",
		"> group: echo -n bar",
	)
	if err := process.AssertComplete(); err != nil {
		t.Fatal(err)
	}
}

func TestEventDispatcher_RecursionInScriptsNames(t *testing.T) {
	process := processmock.New()
	process.Expects([]processmock.Expectation{processmock.Shell("echo Hello " + util.Escape("World"))}, true, nil)

	composer := createComposerInstance()
	out := bufferIO(t, console.VerbosityVerbose)
	d := newDispatcher(t, composer, out, process)
	d.listenersFor = scriptGroups(map[string][]string{
		"hello":      {"echo Hello"},
		"helloWorld": {"@hello World"},
	})

	if _, err := d.Dispatch("helloWorld", NewScriptEvent("helloWorld", composer, out, false, nil, nil)); err != nil {
		t.Fatal(err)
	}

	expectOutput(t, out,
		"> helloWorld: @hello World",
		"> hello: echo Hello "+util.Escape("World"),
	)
	if err := process.AssertComplete(); err != nil {
		t.Fatal(err)
	}
}

func TestEventDispatcher_DispatcherDetectInfiniteRecursion(t *testing.T) {
	composer := createComposerInstance()
	ioi := newRecordingIO()
	d := newDispatcher(t, composer, ioi, processmock.New())
	d.listenersFor = scriptGroups(map[string][]string{
		"root":    {"@recurse"},
		"recurse": {"@root"},
	})

	_, err := d.Dispatch("root", NewScriptEvent("root", composer, ioi, false, nil, nil))

	var e *Error
	if !errors.As(err, &e) || e.Class != "RuntimeException" || e.Message != "Circular call to script handler 'root' detected" {
		t.Fatalf("err = %#v", err)
	}
}

func TestEventDispatcher_DispatcherOutputsCommand(t *testing.T) {
	ioi := newRecordingIO()
	d := newDispatcher(t, createComposerInstance(), ioi, http.NewProcessExecutor(ioi))
	d.listenersFor = listenersReturning(Script("echo foo"))

	if _, err := d.DispatchScript(script.PostInstallCmd, false, nil, nil); err != nil {
		t.Fatal(err)
	}

	if len(ioi.errors) != 1 || ioi.errors[0] != "> echo foo" {
		t.Errorf("writeError calls = %q", ioi.errors)
	}
	// cmd.exe's echo ends its line with CRLF.
	want := "foo\n"
	if util.IsWindows() {
		want = "foo\r\n"
	}
	if len(ioi.raws) != 1 || ioi.raws[0] != want {
		t.Errorf("writeRaw calls = %q", ioi.raws)
	}
}

func TestEventDispatcher_DispatcherOutputsErrorOnFailedCommand(t *testing.T) {
	out := ioMock(t)
	d := newDispatcher(t, createComposerInstance(), out, util.NewProcessExecutor(nil))
	code := "exit 1"
	d.listenersFor = listenersReturning(Script(code))

	_, err := d.DispatchScript(script.PostInstallCmd, false, nil, nil)

	expectOutput(t, out,
		"> exit 1",
		"Script "+code+" handling the post-install-cmd event returned with error code 1",
	)
	var e *ScriptExecutionError
	if !errors.As(err, &e) || !strings.Contains(e.Message, "Error Output: ") || e.Code != 1 {
		t.Fatalf("err = %#v", err)
	}
}

func TestEventDispatcher_DispatcherInstallerEvents(t *testing.T) {
	d := newDispatcher(t, createComposerInstance(), newRecordingIO(), processmock.New())
	d.listenersFor = listenersReturning()

	if _, err := d.DispatchInstallerEvent(PreOperationsExec, true, true, struct{}{}); err != nil {
		t.Fatal(err)
	}
}

func TestEventDispatcher_DispatcherDoesntReturnSkippedScripts(t *testing.T) {
	t.Setenv("COMPOSER_SKIP_SCRIPTS", "scriptName")
	composer := createComposerInstance()
	composer.root.SetScripts(php.ArrayOf("scriptName", php.ListOf("scriptName")))

	ioi := newRecordingIO()
	d := newDispatcher(t, composer, ioi, processmock.New())

	event := NewScriptEvent("scriptName", composer, ioi, false, nil, nil)
	if d.HasEventListeners(event) {
		t.Error("hasEventListeners() = true, want false")
	}
}

// WillDispatchTo answers as HasEventListeners without the notice about
// skipped scripts, which the dispatch itself prints.
func TestEventDispatcher_WillDispatchToIsQuiet(t *testing.T) {
	t.Setenv("COMPOSER_SKIP_SCRIPTS", "scriptName")
	composer := createComposerInstance()
	composer.root.SetScripts(php.ArrayOf("scriptName", php.ListOf("scriptName"), "other", php.ListOf("other")))

	ioi := newRecordingIO()
	d := newDispatcher(t, composer, ioi, processmock.New())

	if d.WillDispatchTo(NewScriptEvent("scriptName", composer, ioi, false, nil, nil)) {
		t.Error("WillDispatchTo(skipped) = true, want false")
	}

	if !d.WillDispatchTo(NewScriptEvent("other", composer, ioi, false, nil, nil)) {
		t.Error("WillDispatchTo(other) = false, want true")
	}

	if len(ioi.errors) != 0 {
		t.Errorf("output %q, want none", ioi.errors)
	}
}
