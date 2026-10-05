// Dispatcher behaviour Composer's own tests leave out: priorities and
// propagation, --no-scripts, the runtime bracket and class loader, command
// classes, native PHP scripts, @script argument handling and debug output.

package eventdispatcher

import (
	"errors"
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/processmock"
)

// recorder returns a phpFunc appending name to calls.
func recorder(calls *[]string, name string, stop bool) phpFunc {
	return func(ev Event) (bool, error) {
		*calls = append(*calls, name)
		if stop {
			ev.StopPropagation()
		}

		return false, nil
	}
}

func TestDispatch_PrioritiesScriptsLastAndStopPropagation(t *testing.T) {
	composer := createComposerInstance()
	composer.root.SetScripts(php.ArrayOf("ev", php.ListOf("Script::run")))

	var calls []string
	rt := newFakeRuntime()
	rt.methods["Script::run"] = recorder(&calls, "script", false)
	rt.callables[1] = recorder(&calls, "low", false)
	rt.callables[2] = recorder(&calls, "high", false)
	rt.callables[3] = recorder(&calls, "zero", false)

	d := newDispatcher(t, composer, newRecordingIO(), processmock.New())
	d.SetScriptRuntime(rt)
	d.AddListener("ev", PHPCallable{H: 1, Class: "P", Method: "low"}, -5)
	d.AddListener("ev", PHPCallable{H: 2, Class: "P", Method: "high"}, 10)
	d.AddListener("ev", PHPCallable{H: 3, Class: "P", Method: "zero"}, 0)

	if _, err := d.Dispatch("ev", nil); err != nil {
		t.Fatal(err)
	}
	if want := []string{"high", "zero", "script", "low"}; !slices.Equal(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}

	// a plugin listener stopping propagation keeps the scripts from running
	calls = nil
	rt.callables[2] = recorder(&calls, "high", true)
	if _, err := d.Dispatch("ev", nil); err != nil {
		t.Fatal(err)
	}
	if want := []string{"high"}; !slices.Equal(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestDispatch_NoScriptsKeepsPluginListeners(t *testing.T) {
	composer := createComposerInstance()
	composer.root.SetScripts(php.ArrayOf("ev", php.ListOf("Script::run")))

	var calls []string
	rt := newFakeRuntime()
	rt.methods["Script::run"] = recorder(&calls, "script", false)
	rt.callables[1] = recorder(&calls, "plugin", false)

	d := newDispatcher(t, composer, newRecordingIO(), processmock.New())
	d.SetScriptRuntime(rt)
	d.SetRunScripts(false)
	d.AddListener("ev", PHPCallable{H: 1, Class: "P", Method: "m"}, 0)

	if _, err := d.Dispatch("ev", nil); err != nil {
		t.Fatal(err)
	}
	if want := []string{"plugin"}; !slices.Equal(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	if d.RunScripts() {
		t.Error("RunScripts() = true")
	}
}

func TestDispatch_ReturnCodes(t *testing.T) {
	rt := newFakeRuntime()
	rt.callables[1] = func(Event) (bool, error) { return true, nil }
	rt.methods["C::ok"] = func(Event) (bool, error) { return false, nil }
	rt.commands[`App\FooCommand`] = func(string) (int, error) { return 3, nil }

	d := newDispatcher(t, createComposerInstance(), newRecordingIO(), processmock.New())
	d.SetScriptRuntime(rt)

	d.listenersFor = listenersReturning(PHPCallable{H: 1, Class: "P", Method: "m"}, Script("C::ok"))
	if code, err := d.Dispatch("ev", nil); err != nil || code != 1 {
		t.Fatalf("code, err = %d, %v; want 1 (false returned)", code, err)
	}

	d.listenersFor = listenersReturning(Script(`App\FooCommand`), PHPCallable{H: 1, Class: "P", Method: "m"})
	if code, err := d.Dispatch("custom", nil); err != nil || code != 3 {
		t.Fatalf("code, err = %d, %v; want 3", code, err)
	}
}

func TestDispatch_PHPCallableNotCallable(t *testing.T) {
	d := newDispatcher(t, createComposerInstance(), newRecordingIO(), processmock.New())
	d.SetScriptRuntime(newFakeRuntime())
	d.listenersFor = listenersReturning(PHPCallable{H: 9, Class: `Vendor\Plugin`, Method: "onEvent"})

	_, err := d.Dispatch("ev", nil)
	var e *Error
	if !errors.As(err, &e) || e.Message != `Subscriber Vendor\Plugin::onEvent for event ev is not callable, make sure the function is defined and public` {
		t.Fatalf("err = %v", err)
	}
}

func TestDispatch_PhpScriptWarnings(t *testing.T) {
	rt := newFakeRuntime()
	rt.methods[`Vendor\Exists::other`] = func(Event) (bool, error) { return false, nil }

	out := ioMock(t)
	d := newDispatcher(t, createComposerInstance(), out, processmock.New())
	d.SetScriptRuntime(rt)
	d.listenersFor = listenersReturning(scripts(`Vendor\Missing::run`, `Vendor\Exists::run`)...)

	if _, err := d.DispatchScript("post-install-cmd", false, nil, nil); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, out,
		`<warning>Class Vendor\Missing is not autoloadable, can not call post-install-cmd script</warning>`,
		`<warning>Method Vendor\Exists::run is not callable, can not call post-install-cmd script</warning>`,
	)
}

func TestDispatch_PHPErrorIsNotCaughtAsException(t *testing.T) {
	rt := newFakeRuntime()
	rt.methods["C::m"] = func(Event) (bool, error) {
		return false, &Error{Class: "Error", Message: "Call to undefined function foo()"}
	}

	out := ioMock(t)
	d := newDispatcher(t, createComposerInstance(), out, processmock.New())
	d.SetScriptRuntime(rt)
	d.listenersFor = listenersReturning(Script("C::m"))

	if _, err := d.DispatchScript("post-install-cmd", false, nil, nil); err == nil {
		t.Fatal("no error")
	}
	expectOutput(t, out, "> C::m")
}

func TestDispatch_CommandClass(t *testing.T) {
	var inputs []string
	rt := newFakeRuntime()
	rt.commands[`App\FooCommand`] = func(input string) (int, error) {
		inputs = append(inputs, input)

		return 0, nil
	}
	rt.notCommands[`App\BarCommand`] = true

	out := ioMock(t)
	d := newDispatcher(t, createComposerInstance(), out, processmock.New())
	d.SetScriptRuntime(rt)
	d.listenersFor = listenersReturning(scripts(`App\FooCommand`, `App\BarCommand`, `App\BazCommand`)...)

	if _, err := d.DispatchScript("custom-cmd", false, []string{"a b", "--opt"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := d.DispatchScript("post-install-cmd", false, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := d.DispatchScript("custom-cmd", false, nil, php.ArrayOf("script-alias-input", "x --y")); err != nil {
		t.Fatal(err)
	}

	if want := []string{"'a b' '--opt'", "x --y"}; !slices.Equal(inputs, want) {
		t.Errorf("inputs = %q, want %q", inputs, want)
	}
	expectOutput(t, out,
		`<warning>Class App\BarCommand does not extend Symfony\Component\Console\Command\Command, can not call custom-cmd script</warning>`,
		`<warning>Class App\BazCommand is not autoloadable, can not call custom-cmd script</warning>`,
		`<warning>You cannot bind post-install-cmd to a Command class, use a non-reserved name</warning>`,
		`<warning>Class App\BarCommand does not extend Symfony\Component\Console\Command\Command, can not call post-install-cmd script</warning>`,
		`<warning>Class App\BazCommand is not autoloadable, can not call post-install-cmd script</warning>`,
		`<warning>Class App\BarCommand does not extend Symfony\Component\Console\Command\Command, can not call custom-cmd script</warning>`,
		`<warning>Class App\BazCommand is not autoloadable, can not call custom-cmd script</warning>`,
	)
}

func TestDispatch_NativeDisableProcessTimeoutNeedsNoRuntime(t *testing.T) {
	timeout := util.GetProcessTimeout()
	t.Cleanup(func() { util.SetProcessTimeout(timeout) })
	util.SetProcessTimeout(300)

	composer := createComposerInstance()
	out := ioMock(t)
	d := newDispatcher(t, composer, out, processmock.New())
	d.listenersFor = listenersReturning(Script(`Composer\Config::disableProcessTimeout`))

	if _, err := d.DispatchScript("test", false, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := util.GetProcessTimeout(); got != 0 {
		t.Errorf("process timeout = %d, want 0", got)
	}
	expectOutput(t, out, `> Composer\Config::disableProcessTimeout`)
	// makeAutoloader still built the loader (and set the dev mode) as
	// Composer does, but no PHP was needed to run the script
	if composer.autoload.created != 1 {
		t.Errorf("loaders built = %d, want 1", composer.autoload.created)
	}
}

func TestDispatch_PhpScriptWithoutRuntime(t *testing.T) {
	d := newDispatcher(t, createComposerInstance(), newRecordingIO(), processmock.New())
	d.listenersFor = listenersReturning(Script(`Vendor\Class::method`))

	_, err := d.DispatchScript("test", false, nil, nil)
	var e *Error
	if !errors.As(err, &e) || e.Message != `maestro: script Vendor\Class::method requires PHP but no PHP runtime is available` {
		t.Fatalf("err = %v", err)
	}
}

func TestDispatch_RuntimeBracketAndClassLoader(t *testing.T) {
	composer := createComposerInstance()
	composer.autoload.packages = []pkg.PackageInterface{pkg.NewPackage("a/a", "1.0.0.0", "1.0.0")}

	rt := newFakeRuntime()
	rt.methods["C::m"] = func(Event) (bool, error) { return false, nil }
	rt.methods["C::n"] = func(Event) (bool, error) { return false, nil }

	d := newDispatcher(t, composer, newRecordingIO(), processmock.New())
	d.SetScriptRuntime(rt)
	d.listenersFor = scriptGroups(map[string][]string{
		"outer": {"echo no php", "C::m", "@inner"},
		"inner": {"C::n", "C::m"},
		"shell": {"echo only"},
	})

	if _, err := d.DispatchScript("outer", true, nil, nil); err != nil {
		t.Fatal(err)
	}
	if want := []string{"begin 1", "begin 2", "end 2", "end 1"}; !slices.Equal(rt.brackets, want) {
		t.Errorf("brackets = %v, want %v", rt.brackets, want)
	}
	// one loader: the package set and dev mode did not change between the
	// callables, and C::m is only prepared once
	if len(rt.installed) != 1 || composer.autoload.created != 1 {
		t.Errorf("installed %d, built %d loaders; want 1, 1", len(rt.installed), composer.autoload.created)
	}
	if want := []bool{true, true}; !slices.Equal(composer.autoload.devModes, want) {
		t.Errorf("setDevMode calls = %v, want %v", composer.autoload.devModes, want)
	}

	// a different dev mode is a new loader
	rt.methods["C::o"] = rt.methods["C::m"]
	d.listenersFor = listenersReturning(Script("C::o"))
	if _, err := d.DispatchScript("other", false, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(rt.installed) != 2 {
		t.Errorf("installed %d loaders, want 2", len(rt.installed))
	}

	// a dispatch without PHP listeners leaves the runtime alone
	rt.brackets = nil
	d.listenersFor = scriptGroups(map[string][]string{"shell": {"echo only"}})
	if _, err := d.DispatchScript("shell", false, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(rt.brackets) != 0 {
		t.Errorf("brackets = %v, want none", rt.brackets)
	}
}

func TestDispatch_PartialComposerSkipsAutoloader(t *testing.T) {
	composer := &fakePartialComposer{root: pkg.NewRootPackage("__root__", "1.0.0.0", "1.0.0"), config: createComposerInstance().config}
	rt := newFakeRuntime()
	rt.callables[1] = func(Event) (bool, error) { return false, nil }

	d := newDispatcher(t, composer, newRecordingIO(), processmock.New())
	d.SetScriptRuntime(rt)
	d.AddListener("init", PHPCallable{H: 1, Class: "P", Method: "m"}, 0)

	if _, err := d.Dispatch("init", nil); err != nil {
		t.Fatal(err)
	}
	if len(rt.installed) != 0 {
		t.Errorf("installed %d loaders, want 0", len(rt.installed))
	}

	_, err := d.DispatchScript("post-install-cmd", false, nil, nil)
	var e *Error
	if !errors.As(err, &e) || e.Class != "LogicException" {
		t.Fatalf("err = %v, want the LogicException", err)
	}
}

func TestDispatch_ScriptReferenceArguments(t *testing.T) {
	composer := createComposerInstance()
	var got []*ScriptEvent
	rt := newFakeRuntime()
	rt.methods["C::m"] = func(ev Event) (bool, error) {
		got = append(got, ev.(*ScriptEvent))

		return false, nil
	}

	d := newDispatcher(t, composer, newRecordingIO(), processmock.New())
	d.SetScriptRuntime(rt)
	d.listenersFor = scriptGroups(map[string][]string{
		"plain": {"@target a b"},
		"spot":  {"@target a @additional_args b"},
		"none":  {"@target a @no_additional_args"},
		"alias": {"@target x y"},
		// recursion goes through the event stack, so target has its own name
		"target": {"C::m"},
	})

	origin := NewScriptEvent("plain", composer, newRecordingIO(), false, []string{"u1", "u2"}, nil)
	for _, ev := range []*ScriptEvent{
		origin,
		NewScriptEvent("spot", composer, newRecordingIO(), true, []string{"u1"}, nil),
		NewScriptEvent("none", composer, newRecordingIO(), false, []string{"u1"}, nil),
		NewScriptEvent("alias", composer, newRecordingIO(), false, []string{"u1"}, php.ArrayOf("script-alias-input", "--z")),
	} {
		if _, err := d.Dispatch(ev.Name(), ev); err != nil {
			t.Fatal(err)
		}
	}

	if len(got) != 4 {
		t.Fatalf("got %d events", len(got))
	}
	if !slices.Equal(got[0].Arguments(), []string{"a", "b", "u1", "u2"}) || got[0].OriginatingEvent() != Event(origin) {
		t.Errorf("plain: args %q, origin %v", got[0].Arguments(), got[0].OriginatingEvent())
	}
	// Composer assigns array_splice()'s (empty) return value to $args
	if len(got[1].Arguments()) != 0 || !got[1].IsDevMode() {
		t.Errorf("spot: args %q, dev %v", got[1].Arguments(), got[1].IsDevMode())
	}
	if !slices.Equal(got[2].Arguments(), []string{"a"}) {
		t.Errorf("none: args %q", got[2].Arguments())
	}
	if v, _ := got[3].Flags().GetString("script-alias-input"); v != "'x' 'y' --z" {
		t.Errorf("alias: script-alias-input %q", v)
	}
}

func TestDispatch_ScriptReferenceFromPlainEvent(t *testing.T) {
	d := newDispatcher(t, createComposerInstance(), newRecordingIO(), processmock.New())
	d.listenersFor = scriptGroups(map[string][]string{"ev": {"@other"}, "other": {"echo"}})

	_, err := d.Dispatch("ev", nil)
	var e *Error
	if !errors.As(err, &e) || e.Message != `Call to undefined method Composer\EventDispatcher\Event::getComposer()` || !e.IsPHPError() {
		t.Fatalf("err = %v", err)
	}
}

func TestDispatch_NonExistentScriptReference(t *testing.T) {
	composer := createComposerInstance()
	out := ioMock(t)
	d := newDispatcher(t, composer, out, processmock.New())
	d.listenersFor = scriptGroups(map[string][]string{"ev": {"@missing"}})

	if _, err := d.DispatchScript("ev", false, nil, nil); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, out, "<warning>You made a reference to a non-existent script @missing</warning>")
}

func TestDispatch_ScriptAliasInputHidesCommand(t *testing.T) {
	process := processmock.New()
	out := ioMock(t)
	d := newDispatcher(t, createComposerInstance(), out, process)
	d.listenersFor = listenersReturning(Script("echo hidden"))

	if _, err := d.DispatchScript("alias", false, nil, php.ArrayOf("script-alias-input", "")); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Dispatch("__exec_command", nil); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, out)
	if got := len(process.Log()); got != 2 {
		t.Errorf("ran %d commands, want 2", got)
	}
}

func TestDispatch_DebugEvents(t *testing.T) {
	t.Setenv("COMPOSER_DEBUG_EVENTS", "1")
	out := ioMock(t)
	composer := createComposerInstance()
	d := newDispatcher(t, composer, out, processmock.New())

	if _, err := d.Dispatch("ev", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := d.DispatchPackageEvent(PostPackageInstall, false, nil, nil, stringOp("Installing a/a (1.0.0)")); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Dispatch(PluginCommand, NewCommandEvent(PluginCommand, "install", nil, nil, nil, nil)); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, out,
		"Dispatching ev event",
		"Dispatching post-package-install (Installing a/a (1.0.0)) event",
		"Dispatching command (install) event",
	)
}

type stringOp string

func (s stringOp) String() string { return string(s) }

func TestDispatch_SkipScriptsMessage(t *testing.T) {
	t.Setenv("COMPOSER_SKIP_SCRIPTS", " other , test ,")
	composer := createComposerInstance()
	composer.root.SetScripts(php.ArrayOf("test", php.ListOf("echo")))

	out := bufferIO(t, console.VerbosityVerbose)
	d := newDispatcher(t, composer, out, processmock.New())

	if _, err := d.DispatchScript("test", false, nil, nil); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, out, "Skipped script listeners for test because of COMPOSER_SKIP_SCRIPTS")
}

func TestDispatch_GoFuncAndRemoveListener(t *testing.T) {
	var calls int
	d := newDispatcher(t, createComposerInstance(), newRecordingIO(), processmock.New())
	d.AddListener(PreOperationsExec, GoFunc(func(Event) error {
		calls++

		return nil
	}), 0)
	d.AddListener(PreOperationsExec, Script("echo x"), 0)

	if _, err := d.DispatchInstallerEvent(PreOperationsExec, false, true, nil); err != nil {
		t.Fatal(err)
	}
	d.RemoveListener(MatchScript("echo x"))
	if !d.HasEventListeners(NewEvent(PreOperationsExec, nil, nil)) {
		t.Error("the Go listener was removed")
	}
	if calls != 1 {
		t.Errorf("calls = %d", calls)
	}
}

type subscriber []Subscription

func (s subscriber) SubscribedEvents() []Subscription { return s }

func TestDispatch_AddSubscriberAndEventRevisions(t *testing.T) {
	var order []string
	listener := func(name string) GoFunc {
		return func(Event) error {
			order = append(order, name)

			return nil
		}
	}

	d := newDispatcher(t, createComposerInstance(), newRecordingIO(), processmock.New())
	d.AddSubscriber(subscriber{{Event: "ev", Listener: listener("a"), Priority: 0}, {Event: "ev", Listener: listener("b"), Priority: 5}})

	ev := NewEvent("ev", nil, nil)
	if _, err := d.Dispatch("", ev); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(order, []string{"b", "a"}) {
		t.Errorf("order = %v", order)
	}

	rev := ev.Rev()
	ev.StopPropagation()
	if ev.Rev() == rev || !ev.IsPropagationStopped() {
		t.Error("StopPropagation did not bump the revision")
	}

	pre := NewPreFileDownloadEvent(PreFileDownload, nil, "https://example.org/a.zip", "package", nil)
	rev = pre.Rev()
	pre.SetProcessedURL("https://mirror.example.org/a.zip")
	pre.SetCustomCacheKey(pkg.NullString{S: "key", Valid: true})
	if pre.Rev() != rev+2 || pre.ProcessedURL() != "https://mirror.example.org/a.zip" || pre.TransportOptions().Len() != 0 {
		t.Error("PreFileDownloadEvent setters")
	}
}
