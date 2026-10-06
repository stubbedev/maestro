// Events, operations and the event dispatcher (docs/PLUGINS.md §4.4, §4.7,
// §5.5): maestro's events and operations as PHP mirrors them, the
// dispatcher's methods (`ed.*`), the events' setters (`event.*`) and the
// plugin manager's (`pm.*`).

package plugin

import (
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
	"github.com/stubbedev/maestro/internal/resolver/operation"
)

// eventBase is the shim base class of the event family.
const eventBase = `Composer\EventDispatcher\Event`

// eventMirror is a maestro event as PHP mirrors it
// (Maestro\Shim\Adapter\EventAdapter).
type eventMirror struct {
	r *Runtime
	e eventdispatcher.Event
}

// PHPOpaque implements php.Opaque.
func (*eventMirror) PHPOpaque() {}

// PHPClass implements rpc.Object.
func (m *eventMirror) PHPClass() string { return m.e.Class() }

// MirrorBase implements rpc.Mirror.
func (*eventMirror) MirrorBase() string { return eventBase }

// Rev implements rpc.Mirror.
func (m *eventMirror) Rev() uint64 { return m.e.Rev() }

// ApplyMirror implements rpc.Mirror: the shim changes events through their
// setters (`event.*`).
func (*eventMirror) ApplyMirror(*php.Array) error {
	return &rpc.ProtocolError{Message: "event fields are not synced from PHP"}
}

// MirrorSnapshot implements rpc.Mirror: Event's fields and those of the
// class declaring the rest.
func (m *eventMirror) MirrorSnapshot() (*php.Array, error) {
	e := m.e
	s := php.ArrayOf(
		"name", e.Name(),
		"args", php.StringList(e.Arguments()),
		"flags", e.Flags(),
		"propagationStopped", e.IsPropagationStopped(),
	)

	if ce, ok := e.(eventdispatcher.ComposerEvent); ok {
		s.Set("composer", m.r.composerValue(ce.Composer()))
		s.Set("io", m.r.value(ce.IO()))
		s.Set("devMode", ce.IsDevMode())
	}

	switch e := e.(type) {
	case *eventdispatcher.ScriptEvent:
		var orig any
		if o := e.OriginatingEvent(); o != nil {
			orig = m.r.eventObject(o)
		}
		s.Set("originatingEvent", orig)
	case *eventdispatcher.PackageEvent:
		s.Set("localRepo", m.r.value(e.LocalRepo()))
		ops := php.NewArrayCap(len(e.Operations()))
		for _, op := range e.Operations() {
			ops.Append(m.r.value(op))
		}
		s.Set("operations", ops)
		s.Set("operation", m.r.value(e.Operation()))
	case *eventdispatcher.InstallerEvent:
		s.Set("executeOperations", e.IsExecutingOperations())
		s.Set("transaction", m.r.value(e.Transaction()))
	case *eventdispatcher.CommandEvent:
		s.Set("commandName", e.CommandName())
		s.Set("input", m.r.inputObject(e.Input()))
		s.Set("output", m.r.outputObject(e.Output()))
	case *eventdispatcher.PreCommandRunEvent:
		s.Set("command", e.Command())
		s.Set("input", m.r.inputObject(e.Input()))
	default:
		m.r.phase5EventFields(s, e)
	}

	return s, nil
}

// composerValue is an event's Composer as PHP holds it.
func (r *Runtime) composerValue(c eventdispatcher.Composer) any {
	switch c := c.(type) {
	case *composer.Composer:
		return r.value(c)
	case nil:
		return nil
	}

	return nil
}

// eventObject returns the object an event crosses to PHP as.
func (r *Runtime) eventObject(e eventdispatcher.Event) any {
	if e == nil || !hashable(e) {
		return nil
	}

	return r.bridge.object(e, func() rpc.Object { return &eventMirror{r: r, e: e} })
}

// eventParam returns param i as a maestro event.
func eventParam(a args, i int) (eventdispatcher.Event, error) {
	if m, ok := a.at(i).(*eventMirror); ok {
		return m.e, nil
	}

	return nil, a.errorf("param %d is not an event maestro knows (a %T)", i, a.at(i))
}

// adoptEvent builds the Go side of an event created in PHP (a plugin's
// Event subclass) when it first crosses to maestro: dispatched by maestro,
// PHP listeners get the very same object.
func (r *Runtime) adoptEvent(class string, snapshot *php.Array) (rpc.Mirror, error) {
	name, _ := snapshot.GetString("name")
	var argv []string
	if list, ok := snapshot.GetArray("args"); ok {
		for _, v := range list.Values() {
			argv = append(argv, php.ToString(v))
		}
	}
	flags, _ := snapshot.GetArray("flags")
	if flags == nil {
		flags = php.NewArray()
	}

	var ctx *eventdispatcher.EventContext
	switch group, _ := snapshot.GetString("class"); group {
	case `Composer\Script\Event`, `Composer\Installer\PackageEvent`, `Composer\Installer\InstallerEvent`:
		ctx = &eventdispatcher.EventContext{}
		if v, ok := snapshot.Get("composer"); ok {
			if c, ok := serviceValue(v).(*composer.Composer); ok {
				ctx.Composer = c
			}
		}
		if v, ok := snapshot.Get("io"); ok {
			if m, ok := v.(*ioMirror); ok {
				ctx.IO = m.io
			}
		}
		if v, ok := snapshot.Get("devMode"); ok {
			ctx.DevMode = php.ToBool(v)
		}
	}

	e := eventdispatcher.NewPHPEvent(0, class, name, argv, flags, ctx)
	if stopped, _ := snapshot.Get("propagationStopped"); stopped == true {
		e.StopPropagation()
	}
	m := &eventMirror{r: r, e: e}
	r.bridge.object(e, func() rpc.Object { return m })

	return m, nil
}

// operationMirror is a maestro operation as PHP mirrors it
// (Maestro\Shim\Adapter\OperationAdapter).
type operationMirror struct {
	r  *Runtime
	op operation.Operation
}

// PHPOpaque implements php.Opaque.
func (*operationMirror) PHPOpaque() {}

// PHPClass implements rpc.Object.
func (m *operationMirror) PHPClass() string {
	switch m.op.(type) {
	case *operation.InstallOperation:
		return `Composer\DependencyResolver\Operation\InstallOperation`
	case *operation.UpdateOperation:
		return `Composer\DependencyResolver\Operation\UpdateOperation`
	case *operation.UninstallOperation:
		return `Composer\DependencyResolver\Operation\UninstallOperation`
	case *operation.MarkAliasInstalledOperation:
		return `Composer\DependencyResolver\Operation\MarkAliasInstalledOperation`
	case *operation.MarkAliasUninstalledOperation:
		return `Composer\DependencyResolver\Operation\MarkAliasUninstalledOperation`
	}

	return `Composer\DependencyResolver\Operation\SolverOperation`
}

// MirrorBase implements rpc.Mirror.
func (*operationMirror) MirrorBase() string {
	return `Composer\DependencyResolver\Operation\SolverOperation`
}

// Rev implements rpc.Mirror: operations do not change (their packages are
// mirrors of their own).
func (*operationMirror) Rev() uint64 { return 0 }

// ApplyMirror implements rpc.Mirror.
func (*operationMirror) ApplyMirror(*php.Array) error {
	return &rpc.ProtocolError{Message: "operations are not synced from PHP"}
}

// MirrorSnapshot implements rpc.Mirror.
func (m *operationMirror) MirrorSnapshot() (*php.Array, error) {
	switch op := m.op.(type) {
	case *operation.InstallOperation:
		return php.ArrayOf("package", m.r.packageObject(op.Package())), nil
	case *operation.UpdateOperation:
		return php.ArrayOf("initialPackage", m.r.packageObject(op.InitialPackage()), "targetPackage", m.r.packageObject(op.TargetPackage())), nil
	case *operation.UninstallOperation:
		return php.ArrayOf("package", m.r.packageObject(op.Package())), nil
	case *operation.MarkAliasInstalledOperation:
		return php.ArrayOf("package", m.r.packageObject(op.Package())), nil
	case *operation.MarkAliasUninstalledOperation:
		return php.ArrayOf("package", m.r.packageObject(op.Package())), nil
	}

	return php.NewArray(), nil
}

// adoptOperation builds the Go side of an operation created in PHP (`new
// UninstallOperation($package)`, cweagans/composer-patches) when it first
// crosses to maestro.
func (r *Runtime) adoptOperation(class string, snapshot *php.Array) (rpc.Mirror, error) {
	pkgOf := func(key string) (pkg.PackageInterface, error) {
		v, _ := snapshot.Get(key)
		m, ok := v.(*packageMirror)
		if !ok {
			return nil, &rpc.ProtocolError{Message: "an operation created in PHP without a package maestro knows"}
		}

		return m.p, nil
	}

	// A plugin's subclass is maestro's operation of the Composer class it
	// extends; PHP keeps its own object.
	if c, ok := snapshot.GetString("composerClass"); ok {
		class = c
	}
	var op operation.Operation
	if class == `Composer\DependencyResolver\Operation\UpdateOperation` {
		initial, err := pkgOf("initialPackage")
		if err != nil {
			return nil, err
		}
		target, err := pkgOf("targetPackage")
		if err != nil {
			return nil, err
		}
		op = operation.NewUpdateOperation(initial, target)
	} else {
		p, err := pkgOf("package")
		if err != nil {
			return nil, err
		}
		switch class {
		case `Composer\DependencyResolver\Operation\UninstallOperation`:
			op = operation.NewUninstallOperation(p)
		case `Composer\DependencyResolver\Operation\MarkAliasInstalledOperation`, `Composer\DependencyResolver\Operation\MarkAliasUninstalledOperation`:
			alias, ok := p.(pkg.Alias)
			if !ok {
				return nil, &rpc.ProtocolError{Message: class + " of a package that is not an alias"}
			}
			if class == `Composer\DependencyResolver\Operation\MarkAliasInstalledOperation` {
				op = operation.NewMarkAliasInstalledOperation(alias)
			} else {
				op = operation.NewMarkAliasUninstalledOperation(alias)
			}
		case `Composer\DependencyResolver\Operation\InstallOperation`:
			op = operation.NewInstallOperation(p)
		default:
			return nil, unsupportedf("maestro does not support giving it a %s created in PHP yet", class)
		}
	}
	m := &operationMirror{r: r, op: op}
	r.bridge.object(op, func() rpc.Object { return m })

	return m, nil
}

// operationObject returns the object an operation crosses to PHP as.
func (r *Runtime) operationObject(op operation.Operation) any {
	if op == nil || !hashable(op) {
		return nil
	}

	return r.bridge.object(op, func() rpc.Object { return &operationMirror{r: r, op: op} })
}

// phpHandle is the handle of a PHP-owned object param; 0 for anything
// else.
func phpHandle(v any) eventdispatcher.Handle {
	if o, ok := v.(*rpc.PHPObject); ok {
		return eventdispatcher.Handle(o.H)
	}

	return 0
}

// listenerArg is addListener()'s $listener as the shim describes it
// (Maestro\Shim\Listeners::describe).
func listenerArg(a args, i int) (eventdispatcher.Listener, error) {
	switch v := a.at(i).(type) {
	case string:
		return eventdispatcher.Script(v), nil
	case *php.Array:
		h, _ := v.Get("h")
		object, _ := v.Get("object")
		class, _ := v.GetString("class")
		method, _ := v.GetString("method")
		closure, _ := v.Get("closure")
		l := eventdispatcher.PHPCallable{H: phpHandle(h), ObjectH: phpHandle(object), Class: class, Method: method, Closure: closure == true}
		if l.H == 0 {
			return nil, a.errorf("a listener without a PHP object")
		}

		return l, nil
	}

	return nil, a.errorf("param %d is not a listener", i)
}

func (r *Runtime) registerEvents() {
	r.RegisterMirrorFactory(eventBase, r.adoptEvent)
	r.registerNewDispatcher()
	r.RegisterMirrorFactory(`Composer\DependencyResolver\Operation\SolverOperation`, r.adoptOperation)

	method := func(name string, fn func(d *eventdispatcher.EventDispatcher, a args) (any, error)) {
		r.Handle("ed."+name, func(v any) (any, error) {
			a := argsOf("ed."+name, v)
			d, err := receiver[*eventdispatcher.EventDispatcher](a)
			if err != nil {
				return nil, err
			}

			return fn(d, a)
		})
	}

	method("addListener", func(d *eventdispatcher.EventDispatcher, a args) (any, error) {
		l, err := listenerArg(a, 2)
		if err != nil {
			return nil, err
		}
		d.AddListener(a.str(1), l, a.integer(3))

		return nil, nil
	})
	method("removeListener", func(d *eventdispatcher.EventDispatcher, a args) (any, error) {
		m := a.arrayOrEmpty(1)
		switch kind, _ := m.GetString("kind"); kind {
		case "script":
			s, _ := m.GetString("script")
			d.RemoveListener(eventdispatcher.MatchScript(s))
		case "object":
			h, _ := m.Get("h")
			if ph := phpHandle(h); ph != 0 {
				d.RemoveListener(eventdispatcher.MatchObject(ph))
			}
		case "callable":
			h, _ := m.Get("h")
			if ph := phpHandle(h); ph != 0 {
				d.RemoveListener(eventdispatcher.MatchCallable(ph))
			}
		}

		return nil, nil
	})
	method("dispatch", func(d *eventdispatcher.EventDispatcher, a args) (any, error) {
		var ev eventdispatcher.Event
		if a.has(2) {
			e, err := eventParam(a, 2)
			if err != nil {
				return nil, err
			}
			ev = e
		}
		name := a.str(1)
		if !a.has(1) && ev != nil {
			name = ev.Name()
		}
		code, err := d.Dispatch(name, ev)

		return int64(code), err
	})
	method("dispatchScript", func(d *eventdispatcher.EventDispatcher, a args) (any, error) {
		var argv []string
		for _, v := range a.arrayOrEmpty(3).Values() {
			argv = append(argv, php.ToString(v))
		}
		code, err := d.DispatchScript(a.str(1), a.boolean(2), argv, a.array(4))

		return int64(code), err
	})
	method("dispatchPackageEvent", func(d *eventdispatcher.EventDispatcher, a args) (any, error) {
		repo, ok := goRepository(a.at(3))
		if !ok {
			return nil, a.errorf("param 3 is not a repository maestro knows")
		}
		var ops []eventdispatcher.Operation
		for _, v := range a.arrayOrEmpty(4).Values() {
			m, ok := v.(*operationMirror)
			if !ok {
				return nil, a.errorf("maestro does not support dispatching operations created in PHP yet")
			}
			ops = append(ops, m.op)
		}
		m, ok := a.at(5).(*operationMirror)
		if !ok {
			return nil, a.errorf("maestro does not support dispatching operations created in PHP yet")
		}
		code, err := d.DispatchPackageEvent(a.str(1), a.boolean(2), repo, ops, m.op)

		return int64(code), err
	})
	method("dispatchInstallerEvent", func(d *eventdispatcher.EventDispatcher, a args) (any, error) {
		code, err := d.DispatchInstallerEvent(a.str(1), a.boolean(2), a.boolean(3), serviceValue(a.at(4)))

		return int64(code), err
	})
	method("hasEventListeners", func(d *eventdispatcher.EventDispatcher, a args) (any, error) {
		e, err := eventParam(a, 1)
		if err != nil {
			return nil, err
		}

		return d.HasEventListeners(e), nil
	})
	method("setRunScripts", func(d *eventdispatcher.EventDispatcher, a args) (any, error) {
		d.SetRunScripts(a.boolean(1))

		return nil, nil
	})

	r.Handle("event.stopPropagation", func(v any) (any, error) {
		a := argsOf("event.stopPropagation", v)
		e, err := eventParam(a, 0)
		if err != nil {
			return nil, err
		}
		e.StopPropagation()

		return nil, nil
	})
	r.Handle("event.setOriginatingEvent", func(v any) (any, error) {
		a := argsOf("event.setOriginatingEvent", v)
		e, err := eventParam(a, 0)
		if err != nil {
			return nil, err
		}
		se, ok := e.(*eventdispatcher.ScriptEvent)
		if !ok {
			return nil, a.errorf("%s has no originating event", e.Class())
		}
		orig, err := eventParam(a, 1)
		if err != nil {
			return nil, err
		}
		se.SetOriginatingEvent(orig)

		return nil, nil
	})
}

func (r *Runtime) registerPluginManager() {
	method := func(name string, fn func(m *Manager, a args) (any, error)) {
		r.Handle("pm."+name, func(v any) (any, error) {
			a := argsOf("pm."+name, v)
			m, ok := a.at(0).(*Manager)
			if !ok {
				return nil, a.errorf("param 0 is not a plugin manager maestro knows")
			}

			return fn(m, a)
		})
	}

	method("loadInstalledPlugins", func(m *Manager, _ args) (any, error) { return nil, m.LoadInstalledPlugins() })
	method("deactivateInstalledPlugins", func(m *Manager, _ args) (any, error) { return nil, m.DeactivateInstalledPlugins() })
	method("getRegisteredPlugins", func(m *Manager, _ args) (any, error) { return php.StringList(m.RegisteredPlugins()), nil })
	method("registerPackage", func(m *Manager, a args) (any, error) {
		p, err := packageParam(a, 1)
		if err != nil {
			return nil, err
		}

		return nil, m.RegisterPackage(p, a.boolean(2), a.boolean(3))
	})
	method("deactivatePackage", func(m *Manager, a args) (any, error) {
		p, err := packageParam(a, 1)
		if err != nil {
			return nil, err
		}

		return nil, m.DeactivatePackage(p)
	})
	method("uninstallPackage", func(m *Manager, a args) (any, error) {
		p, err := packageParam(a, 1)
		if err != nil {
			return nil, err
		}

		return nil, m.UninstallPackage(p)
	})
	method("disablePlugins", func(m *Manager, _ args) (any, error) { m.DisablePlugins(); return nil, nil })
	method("isPluginAllowed", func(m *Manager, a args) (any, error) {
		optional := a.boolean(3)
		prompt := true
		if a.has(4) {
			prompt = a.boolean(4)
		}

		return m.isPluginAllowed(a.str(1), a.boolean(2), optional, prompt)
	})
	method("setRunningInGlobalDir", func(m *Manager, a args) (any, error) {
		m.SetRunningInGlobalDir(a.boolean(1))

		return nil, nil
	})
}

// keep pkg referenced for the package helpers used across files.
var _ pkg.PackageInterface

// registerNewDispatcher registers `ed.new`: new EventDispatcher($composer,
// $io) in PHP (magento) is a second dispatcher of maestro's, set up as
// Factory sets up a Composer's.
func (r *Runtime) registerNewDispatcher() {
	r.Handle("ed.new", func(v any) (any, error) {
		a := argsOf("ed.new", v)
		var c eventdispatcher.PartialComposer
		switch cv := serviceValue(a.at(1)).(type) {
		case *composer.Composer:
			c = cv
		case *composer.PartialComposer:
			c = cv
		default:
			return nil, a.errorf("param 1 is not a Composer instance maestro knows")
		}
		out, _, err := ioParam(a, 2)
		if err != nil {
			return nil, err
		}
		d := eventdispatcher.New(c, out, nil)
		if f := r.composerFactory; f != nil && f.Runtime != nil {
			d.SetPHP(f.Runtime.PlatformPHP())
		}
		d.SetScriptRuntime(r)
		d.SetEnsureComposerBinary(r.EnsureComposerBinary)

		return nil, r.adopt(a, d)
	})
}
