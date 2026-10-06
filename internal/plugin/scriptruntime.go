// plugin.Runtime as internal/eventdispatcher's ScriptRuntime
// (docs/PLUGINS.md §5.5, §7): PHP callable listeners, Class::method
// scripts, command-class scripts, makeAutoloader's class loader and the
// dispatch bracket.

package plugin

import (
	"errors"

	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
)

var _ eventdispatcher.ScriptRuntime = (*Runtime)(nil)

// beforeFrame is the hook of one call into PHP code: PHP calls
// `dispatch.before` once its checks passed, right before the code runs.
type beforeFrame struct {
	fn     func() bool
	called bool
}

// callWithBefore calls method in PHP with before as its dispatch.before
// hook. ran reports whether PHP got past its checks (an error then was
// thrown by the PHP code).
func (r *Runtime) callWithBefore(method string, params *php.Array, before func() bool) (res *php.Array, ran bool, err error) {
	frame := &beforeFrame{fn: before}
	r.mu.Lock()
	r.befores = append(r.befores, frame)
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.befores = r.befores[:len(r.befores)-1]
		r.mu.Unlock()
	}()

	v, err := r.Call(method, params)
	if err != nil {
		if _, ok := errors.AsType[*rpc.PHPException](err); !ok {
			// PHP ended (exit(), a fatal error) or broke the protocol:
			// nothing is reported about the script, as in Composer, whose
			// process ends right there.
			return nil, false, err
		}

		return nil, frame.called, err
	}
	res, _ = v.(*php.Array)
	if res == nil {
		return nil, frame.called, &rpc.ProtocolError{Message: method + " returned no result"}
	}

	return res, frame.called, nil
}

// dispatchBefore serves `dispatch.before`.
func (r *Runtime) dispatchBefore(any) (any, error) {
	r.mu.Lock()
	var frame *beforeFrame
	if n := len(r.befores); n > 0 {
		frame = r.befores[n-1]
	}
	r.mu.Unlock()

	if frame == nil {
		return nil, &rpc.ProtocolError{Message: "dispatch.before outside a call"}
	}
	frame.called = true

	return frame.fn(), nil
}

// status reads a ScriptRuntime result's status.
func status(res *php.Array) eventdispatcher.ScriptStatus {
	s, _ := res.GetString("status")

	return eventdispatcher.ScriptStatus(s)
}

// ranStatus is the status of a call that failed: StatusOK when the PHP
// code was running (it threw), none before.
func ranStatus(ran bool) eventdispatcher.ScriptStatus {
	if ran {
		return eventdispatcher.StatusOK
	}

	return ""
}

// CallListener implements eventdispatcher.ScriptRuntime: `$callable($event)`
// after is_callable().
func (r *Runtime) CallListener(l eventdispatcher.PHPCallable, ev eventdispatcher.Event, before func()) (eventdispatcher.ScriptStatus, bool, error) {
	if err := r.flushPending(); err != nil {
		return "", false, err
	}
	res, ran, err := r.callWithBefore("listener.call", r.framed(php.ArrayOf("h", rpc.Handle(l.H), "event", r.eventObject(ev))), func() bool {
		before()

		return true
	})
	if err != nil {
		return ranStatus(ran), false, err
	}
	returnedFalse, _ := res.Get("returnedFalse")

	return status(res), returnedFalse == true, nil
}

// CallPHPScript implements eventdispatcher.ScriptRuntime:
// `$className::$methodName($event)` after class_exists() and
// is_callable().
func (r *Runtime) CallPHPScript(className, methodName string, ev eventdispatcher.Event, before func()) (eventdispatcher.ScriptStatus, bool, error) {
	if err := r.startFor("script "+className+"::"+methodName, eventIO(ev)); err != nil {
		return "", false, err
	}
	res, ran, err := r.callWithBefore("script.php", r.framed(php.ArrayOf("class", className, "method", methodName, "event", r.eventObject(ev))), func() bool {
		before()

		return true
	})
	if err != nil {
		return ranStatus(ran), false, err
	}
	returnedFalse, _ := res.Get("returnedFalse")

	return status(res), returnedFalse == true, nil
}

// RunCommandClass implements eventdispatcher.ScriptRuntime: the command
// class className run as EventDispatcher::doDispatch does, in a fresh
// Symfony Application with the run's output.
func (r *Runtime) RunCommandClass(className string, ev eventdispatcher.Event, input string, before func() bool) (eventdispatcher.ScriptStatus, int, error) {
	out := eventIO(ev)
	if err := r.startFor("script "+className, out); err != nil {
		return "", 0, err
	}
	// Composer reuses its ConsoleIO's output (a new ConsoleOutput for any
	// other IO).
	var output any
	if out != nil {
		output = r.outputObject(ioOutput(out))
	}
	res, ran, err := r.callWithBefore("script.commandClass", r.framed(php.ArrayOf(
		"class", className,
		"event", r.eventObject(ev),
		"input", input,
		"output", output,
	)), before)
	if err != nil {
		return ranStatus(ran), 0, err
	}
	code, _ := res.Get("code")

	return status(res), int(php.ToInt(code)), nil
}

// eventIO is the IO of an event that has one.
func eventIO(ev eventdispatcher.Event) io.IO {
	switch e := ev.(type) {
	case eventdispatcher.ComposerEvent:
		return e.IO()
	case *eventdispatcher.PHPEvent:
		if e.Context != nil {
			return e.Context.IO
		}
	}

	return nil
}

// InstallAutoloader implements eventdispatcher.ScriptRuntime
// (makeAutoloader). Before PHP runs, the loader waits for the start.
func (r *Runtime) InstallAutoloader(loader *eventdispatcher.LoaderContents) error {
	r.mu.Lock()
	if r.conn == nil {
		r.pendingLoader = loader
		r.mu.Unlock()

		return nil
	}
	r.mu.Unlock()

	return r.installAutoloader(loader)
}

func (r *Runtime) installAutoloader(loader *eventdispatcher.LoaderContents) error {
	a := php.ArrayOf("vendorDir", loader.VendorDir)
	if loader.Psr0 != nil {
		a.Set("psr0", loader.Psr0)
	}
	if loader.Psr4 != nil {
		a.Set("psr4", loader.Psr4)
	}
	if loader.ClassMap != nil {
		a.Set("classmap", loader.ClassMap)
	}
	_, err := r.Call("autoload.install", a)

	return err
}

// DispatchBegin implements eventdispatcher.ScriptRuntime. Before PHP runs,
// the bracket opens when it starts (its autoloaders are the shim's then).
func (r *Runtime) DispatchBegin(depth int) error {
	r.mu.Lock()
	if r.conn == nil {
		r.pendingBegins = append(r.pendingBegins, depth)
		r.mu.Unlock()

		return nil
	}
	r.mu.Unlock()

	_, err := r.Call("dispatch.begin", php.ArrayOf("depth", int64(depth)))

	return err
}

// DispatchEnd implements eventdispatcher.ScriptRuntime.
func (r *Runtime) DispatchEnd(depth int) error {
	r.mu.Lock()
	for i, d := range r.pendingBegins {
		if d == depth {
			// PHP never ran in this dispatch.
			r.pendingBegins = append(r.pendingBegins[:i:i], r.pendingBegins[i+1:]...)
			r.mu.Unlock()

			return nil
		}
	}
	started := r.conn != nil
	r.mu.Unlock()
	if !started {
		return nil
	}

	_, err := r.Call("dispatch.end", php.ArrayOf("depth", int64(depth)))

	return err
}

// startFor starts PHP for purpose, with out as the IO of its
// ErrorHandler when this starts it, then sends what waited for the start.
func (r *Runtime) startFor(purpose string, out io.IO) error {
	if out != nil {
		r.mu.Lock()
		if r.bootIO == nil {
			r.bootIO = out
		}
		r.mu.Unlock()
	}
	if err := r.Start(purpose); err != nil {
		return err
	}

	return r.flushPending()
}

// flushPending sends the project class loaders, the dispatch brackets and
// the class loader that waited for PHP to start, in their order.
func (r *Runtime) flushPending() error {
	r.commands.mu.Lock()
	projectLoaders := r.commands.pendingLoaders
	r.commands.pendingLoaders = nil
	r.commands.mu.Unlock()
	for _, l := range projectLoaders {
		if err := r.sendProjectLoader(l); err != nil {
			return err
		}
	}

	r.mu.Lock()
	begins, loader := r.pendingBegins, r.pendingLoader
	r.pendingBegins, r.pendingLoader = nil, nil
	r.mu.Unlock()

	for _, depth := range begins {
		if _, err := r.Call("dispatch.begin", php.ArrayOf("depth", int64(depth))); err != nil {
			return err
		}
	}
	if loader != nil {
		return r.installAutoloader(loader)
	}

	return nil
}

// ReloadInstalledVersions is where Composer reloads its InstalledVersions
// class: FilesystemRepository::write() with the installed.php data and its
// directory (selfDir). Before PHP runs, the latest data waits for the
// start (docs/PLUGINS.md §5.4).
func (r *Runtime) ReloadInstalledVersions(data *php.Array, selfDir string) error {
	r.mu.Lock()
	if r.conn == nil {
		r.pendingIV = php.ArrayOf("data", data, "selfDir", selfDir)
		r.mu.Unlock()

		return nil
	}
	r.mu.Unlock()

	_, err := r.Call("iv.reload", php.ArrayOf("data", data, "selfDir", selfDir))

	return err
}
