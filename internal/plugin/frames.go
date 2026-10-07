// Backtrace frames (docs/PLUGINS.md §5.12; tier 6): plugins look for
// objects on Composer's PHP call stack with debug_backtrace() (symfony/flex
// and symfony/thanks for the Application and its ArgvInput, flex and
// php-http/discovery for the running Composer\Installer, flex for a
// GlobalCommand). maestro's console (the Application's and the command's
// methods), Installer and PluginManager push the method calls Composer's
// stack holds on composer.Runtime's stack, each named as PHP names it;
// every call into PHP code carries the frames pushed since PHP last called
// maestro, and the shim enters each by calling Composer's method itself on
// its object (Maestro\Shim\Frames), whose implementation resumes the call,
// so PHP's real stack holds them as Composer's does, class, function,
// object and arguments, nested calls included. Frames of methods the
// shim does not implement (Symfony's) are left out.

package plugin

import (
	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
)

// frameRuntime is the process runtime whose frame stack PHP mirrors; nil
// without one (tests building a bare Runtime).
func (r *Runtime) frameRuntime() *composer.Runtime {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.composerFactory == nil {
		return nil
	}

	return r.composerFactory.Runtime
}

// frameBoundary wraps a handler of a PHP → Go call: the frames pushed
// before it are on PHP's stack already, below the PHP code calling.
func (r *Runtime) frameBoundary(h rpc.Handler) rpc.Handler {
	return func(v any) (any, error) {
		depth := 0
		if crt := r.frameRuntime(); crt != nil {
			depth = len(crt.Frames())
		}
		r.mu.Lock()
		r.frameBases = append(r.frameBases, depth)
		r.mu.Unlock()
		defer func() {
			r.mu.Lock()
			r.frameBases = r.frameBases[:len(r.frameBases)-1]
			r.mu.Unlock()
		}()

		return h(v)
	}
}

// framesValue is the `frames` param of a call into PHP code: the frames
// pushed since PHP last called maestro, outermost first, each a list of
// its object and arguments; nil for none.
func (r *Runtime) framesValue() *php.Array {
	crt := r.frameRuntime()
	if crt == nil {
		return nil
	}
	frames := crt.Frames()
	r.mu.Lock()
	base := 0
	if n := len(r.frameBases); n > 0 {
		base = r.frameBases[n-1]
	}
	r.mu.Unlock()
	if base > len(frames) {
		base = len(frames)
	}
	if base == len(frames) {
		return nil
	}

	out := php.NewArrayCap(len(frames) - base)
	for _, f := range frames[base:] {
		obj := r.frameObject(f.Object)
		if obj == nil {
			continue
		}
		args := php.NewArrayCap(len(f.Args))
		for _, a := range f.Args {
			args.Append(r.frameObject(a))
		}
		out.Append(php.ListOf(obj, args, f.Function))
	}
	if out.Len() == 0 {
		return nil
	}

	return out
}

// framed adds the frames to the params of a call into PHP code.
func (r *Runtime) framed(params *php.Array) *php.Array {
	if f := r.framesValue(); f != nil {
		params.Set("frames", f)
	}

	return params
}

// frameObject is the PHP form of a frame's object or argument.
func (r *Runtime) frameObject(v any) any {
	switch v := v.(type) {
	case *composer.Installer:
		r.mu.Lock()
		own := r.phpRunInstallers[v]
		r.mu.Unlock()
		if own {
			return nil
		}

		return r.installerObject(v)
	case *command.Application:
		return r.appValue(v)
	case console.Input:
		return r.inputObject(v)
	case console.Output:
		return r.outputObject(v)
	case console.Commander:
		return r.commandObject(v)
	}

	return r.value(v)
}
