package rpc

import (
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/phperr"
)

// A PHP exception's trace ends with the shim's call into the code that
// threw (open); maestro's frames complete it as the error goes up its
// ports (docs/PLUGINS.md §5.12): a frame for the same callee or a Locate
// sets that call's location, other frames follow it, and the previous
// exceptions get them too.
func TestPHPException_Trace(t *testing.T) {
	phperr.SetRoot("phar:///m")
	t.Cleanup(func() { phperr.SetRoot("") })

	plugin := console.TraceFrame{File: "/p/Plugin.php", Line: 9, Class: `P\Plugin`, Type: "->", Function: "helper"}
	open := func(class, typ, fn string) *PHPException {
		return &PHPException{
			Message: "boom",
			Trace:   []console.TraceFrame{plugin, {File: "/shim/src/Maestro/Shim/Dispatch.php", Line: 80, Class: class, Type: typ, Function: fn}},
			open:    true,
		}
	}

	// the callee maestro names: Symfony's Command::run() of a PHP command
	e := open(`Symfony\Component\Console\Command\Command`, "->", "run")
	phperr.Call(e, `Symfony\Component\Console\Command\Command->run`, "vendor/symfony/console/Application.php", 1040)
	phperr.Call(e, `Symfony\Component\Console\Application->doRunCommand`, "vendor/symfony/console/Application.php", 301)
	want := []console.TraceFrame{
		plugin,
		{File: "phar:///m/vendor/symfony/console/Application.php", Line: 1040, Class: `Symfony\Component\Console\Command\Command`, Type: "->", Function: "run"},
		{File: "phar:///m/vendor/symfony/console/Application.php", Line: 301, Class: `Symfony\Component\Console\Application`, Type: "->", Function: "doRunCommand"},
	}
	if !slices.Equal(e.Trace, want) {
		t.Errorf("trace %+v\nwant %+v", e.Trace, want)
	}

	// a callee maestro cannot name (a listener), with a previous exception
	e = open(`P\Plugin`, "::", "onEvent")
	e.Previous = open(`P\Plugin`, "::", "onEvent")
	phperr.Locate(e, "EventDispatcher.php", 232)
	phperr.Call(e, `Composer\EventDispatcher\EventDispatcher->doDispatch`, "EventDispatcher.php", 126)
	for _, x := range []*PHPException{e, e.Previous} {
		if got := x.Trace[1]; got.File != "phar:///m/src/Composer/EventDispatcher/EventDispatcher.php" || got.Line != 232 || got.Function != "onEvent" {
			t.Errorf("located frame %+v", got)
		}
		if got := x.Trace[2]; got.Class != `Composer\EventDispatcher\EventDispatcher` || got.Function != "doDispatch" || got.Line != 126 {
			t.Errorf("added frame %+v", got)
		}
		if _, ok := x.OpenFrame(); ok {
			t.Error("the frame is still open")
		}
	}

	// another callee: the open frame keeps the shim's location, the frame
	// follows it, and a later Locate changes nothing
	e = open(`P\Plugin`, "->", "install")
	phperr.Call(e, `Composer\Installer\InstallationManager->install`, "InstallationManager.php", 100)
	phperr.Locate(e, "InstallationManager.php", 1)
	if len(e.Trace) != 3 || e.Trace[1].Line != 80 || e.Trace[2].Function != "install" || e.Trace[2].Class != `Composer\Installer\InstallationManager` {
		t.Errorf("trace %+v", e.Trace)
	}

	for fn, want := range map[string]console.TraceFrame{
		`A\B->c`:                        {Class: `A\B`, Type: "->", Function: "c"},
		`A\B::c`:                        {Class: `A\B`, Type: "::", Function: "c"},
		`A->{closure:A::b():3}`:         {Class: "A", Type: "->", Function: "{closure:A::b():3}"},
		`{closure:A::b():3}`:            {Function: "{closure:A::b():3}"},
		`Composer\Autoload\includeFile`: {Function: `Composer\Autoload\includeFile`},
	} {
		if got := traceFrame(phperr.Frame{Function: fn}); got != want {
			t.Errorf("traceFrame(%s) = %+v, want %+v", fn, got, want)
		}
	}
}
