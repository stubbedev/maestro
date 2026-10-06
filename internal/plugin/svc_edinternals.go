// EventDispatcher's protected methods on maestro's dispatchers
// (docs/PLUGINS.md §4.4): what a subclass written in PHP calls on itself.
// maestro's dispatch does not call a subclass's overrides of them.

package plugin

import (
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
)

// listenerValue is a listener as getListeners() returns it to PHP: a
// script string, a PHP callable as itself (by handle, which
// Maestro\Shim\Listeners::callable() resolves), a Go-internal one (a
// Closure in Composer) as a callable calling it.
func (r *Runtime) listenerValue(l eventdispatcher.Listener) any {
	switch l := l.(type) {
	case eventdispatcher.Script:
		return string(l)
	case eventdispatcher.PHPCallable:
		return php.ArrayOf("h", rpc.Handle(l.H))
	case eventdispatcher.GoFunc:
		return php.ArrayOf("go", r.goCallable(func(params []any) (any, error) {
			if len(params) == 0 {
				return nil, &rpc.ProtocolError{Message: "a listener called without its event"}
			}
			ev, err := eventParam(args{method: "listener", list: params}, 0)
			if err != nil {
				return nil, err
			}

			return nil, l(ev)
		}))
	}

	return nil
}

func (r *Runtime) registerDispatcherInternals() {
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
	listeners := func(list []eventdispatcher.Listener) *php.Array {
		out := php.NewArrayCap(len(list))
		for _, l := range list {
			out.Append(r.listenerValue(l))
		}

		return out
	}

	method("doDispatch", func(d *eventdispatcher.EventDispatcher, a args) (any, error) {
		ev, err := eventParam(a, 1)
		if err != nil {
			return nil, err
		}
		code, err := d.DoDispatch(ev)

		return int64(code), err
	})
	method("executeTty", func(d *eventdispatcher.EventDispatcher, a args) (any, error) {
		code, err := d.ExecuteTty(a.str(1))

		return int64(code), err
	})
	method("getPhpExecCommand", func(d *eventdispatcher.EventDispatcher, _ args) (any, error) {
		return d.PhpExecCommand()
	})
	// executeEventPhpScript()'s "> Class::method" line; PHP then calls
	// the method.
	method("echoPhpScript", func(d *eventdispatcher.EventDispatcher, a args) (any, error) {
		ev, err := eventParam(a, 3)
		if err != nil {
			return nil, err
		}
		d.EchoPhpScript(ev, a.str(1), a.str(2))

		return nil, nil
	})
	method("getListeners", func(d *eventdispatcher.EventDispatcher, a args) (any, error) {
		ev, err := eventParam(a, 1)
		if err != nil {
			return nil, err
		}

		return listeners(d.Listeners(ev)), nil
	})
	method("getScriptListeners", func(d *eventdispatcher.EventDispatcher, a args) (any, error) {
		ev, err := eventParam(a, 1)
		if err != nil {
			return nil, err
		}

		return listeners(d.ScriptListeners(ev)), nil
	})
	method("pushEvent", func(d *eventdispatcher.EventDispatcher, a args) (any, error) {
		ev, err := eventParam(a, 1)
		if err != nil {
			return nil, err
		}
		n, err := d.PushEvent(ev)

		return int64(n), err
	})
	method("popEvent", func(d *eventdispatcher.EventDispatcher, _ args) (any, error) {
		if name, ok := d.PopEvent(); ok {
			return name, nil
		}

		return nil, nil
	})
}
