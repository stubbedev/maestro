// IOs created in PHP and given to maestro (docs/PLUGINS.md §5.9): a
// BufferIO, a plugin's own ConsoleIO or IOInterface implementation passed
// to Factory::create(), Installer::create(), new EventDispatcher(), ...
// maestro uses it as Composer would: every IOInterface method is the PHP
// object's (`object.call`), so its own output, verbosity and overrides
// apply.

package plugin

import (
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
)

// phpIO is an IO created in PHP as maestro uses it. maestro's parallel
// work (a process's output, a download's) calls it too: Composer makes
// those calls on its one thread, in the order the work makes them, so
// they reach PHP in that order through the goroutine driving the flow
// (callAnywhere). Failures of the calls that cannot return one (writes;
// PHP ending) are dropped; the flags then read false.
type phpIO struct {
	r   *Runtime
	obj *rpc.PHPObject
}

func (p *phpIO) phpObject() *rpc.PHPObject { return p.obj }

// ForeignIO implements io.Foreign.
func (*phpIO) ForeignIO() {}

func (p *phpIO) call(method string, params ...any) (any, error) {
	return p.r.callAnywhere(true, p.obj, method, params...)
}

func (p *phpIO) flag(method string) bool {
	v, err := p.call(method)

	return err == nil && php.ToBool(v)
}

// do is a call whose result does not matter: from parallel work, it is
// posted without waiting for it.
func (p *phpIO) do(method string, params ...any) {
	_, _ = p.r.callAnywhere(false, p.obj, method, params...)
}

// callAnywhere is callObject from any goroutine (rpc.Conn.Run): at once
// on the goroutine holding the PHP baton (or taking it, when free);
// from other goroutines posted to the holder, which makes the call when
// it next calls PHP or waits for parallel work (docs/PLUGINS.md §5.14),
// waiting for the result unless wait is false.
func (r *Runtime) callAnywhere(wait bool, obj *rpc.PHPObject, method string, params ...any) (any, error) {
	conn, err := r.started()
	if err != nil {
		return nil, err
	}

	var (
		v    any
		cerr error
	)
	done := make(chan struct{})
	if conn.Run(func() {
		v, cerr = r.callObject(obj, method, params...)
		close(done)
	}) {
		return v, cerr
	}
	if !wait {
		return nil, nil
	}
	<-done

	return v, cerr
}

// IsInteractive implements io.IO.
func (p *phpIO) IsInteractive() bool { return p.flag("isInteractive") }

// IsVerbose implements io.IO.
func (p *phpIO) IsVerbose() bool { return p.flag("isVerbose") }

// IsVeryVerbose implements io.IO.
func (p *phpIO) IsVeryVerbose() bool { return p.flag("isVeryVerbose") }

// IsDebug implements io.IO.
func (p *phpIO) IsDebug() bool { return p.flag("isDebug") }

// IsDecorated implements io.IO.
func (p *phpIO) IsDecorated() bool { return p.flag("isDecorated") }

// Write implements io.IO.
func (p *phpIO) Write(message string, newline bool, verbosity io.Verbosity) {
	p.do("write", message, newline, int64(verbosity))
}

// WriteMessages implements io.IO.
func (p *phpIO) WriteMessages(messages []string, newline bool, verbosity io.Verbosity) {
	p.do("write", php.StringList(messages), newline, int64(verbosity))
}

// WriteError implements io.IO.
func (p *phpIO) WriteError(message string, newline bool, verbosity io.Verbosity) {
	p.do("writeError", message, newline, int64(verbosity))
}

// WriteErrorMessages implements io.IO.
func (p *phpIO) WriteErrorMessages(messages []string, newline bool, verbosity io.Verbosity) {
	p.do("writeError", php.StringList(messages), newline, int64(verbosity))
}

// WriteRaw implements io.IO.
func (p *phpIO) WriteRaw(message string, newline bool, verbosity io.Verbosity) {
	p.do("writeRaw", message, newline, int64(verbosity))
}

// WriteErrorRaw implements io.IO.
func (p *phpIO) WriteErrorRaw(message string, newline bool, verbosity io.Verbosity) {
	p.do("writeErrorRaw", message, newline, int64(verbosity))
}

// sizeValue is PHP's ?int $size of overwrite(): -1 is null.
func sizeValue(size int) any {
	if size < 0 {
		return nil
	}

	return int64(size)
}

// Overwrite implements io.IO.
func (p *phpIO) Overwrite(message string, newline bool, size int, verbosity io.Verbosity) {
	p.do("overwrite", message, newline, sizeValue(size), int64(verbosity))
}

// OverwriteError implements io.IO.
func (p *phpIO) OverwriteError(message string, newline bool, size int, verbosity io.Verbosity) {
	p.do("overwriteError", message, newline, sizeValue(size), int64(verbosity))
}

// Ask implements io.IO.
func (p *phpIO) Ask(question string, def any) (any, error) { return p.call("ask", question, def) }

// AskConfirmation implements io.IO.
func (p *phpIO) AskConfirmation(question string, def bool) (bool, error) {
	v, err := p.call("askConfirmation", question, def)

	return php.ToBool(v), err
}

// AskAndValidate implements io.IO: maestro's validator crosses as a PHP
// callable (Maestro\Shim\GoCallable).
func (p *phpIO) AskAndValidate(question string, validator console.Validator, attempts int, def any) (any, error) {
	var a any
	if attempts > 0 {
		a = int64(attempts)
	}
	var callable any
	if validator != nil {
		callable = p.r.goCallable(func(args []any) (any, error) {
			var answer any
			if len(args) > 0 {
				answer = args[0]
			}

			return validator(answer)
		})
	}

	return p.call("askAndValidate", question, callable, a, def)
}

// AskAndHideAnswer implements io.IO.
func (p *phpIO) AskAndHideAnswer(question string) (any, error) {
	return p.call("askAndHideAnswer", question)
}

// Select implements io.IO.
func (p *phpIO) Select(question string, choices *php.Array, def any, attempts int, errorMessage string, multiselect bool) (any, error) {
	var a any = false
	if attempts > 0 {
		a = int64(attempts)
	}

	return p.call("select", question, choices, def, a, errorMessage, multiselect)
}

// authentication is an array{username: string|null, password: string|null}.
func authentication(v any) io.Authentication {
	var out io.Authentication
	a, ok := v.(*php.Array)
	if !ok {
		return out
	}
	if u, ok := a.Get("username"); ok && u != nil {
		s := php.ToString(u)
		out.Username = &s
	}
	if pw, ok := a.Get("password"); ok && pw != nil {
		s := php.ToString(pw)
		out.Password = &s
	}

	return out
}

// Authentications implements io.IO.
func (p *phpIO) Authentications() []io.RepositoryAuthentication {
	v, err := p.call("getAuthentications")
	a, ok := v.(*php.Array)
	if err != nil || !ok {
		return nil
	}
	var out []io.RepositoryAuthentication
	for k, auth := range a.All() {
		out = append(out, io.RepositoryAuthentication{Repository: k.String(), Authentication: authentication(auth)})
	}

	return out
}

// HasAuthentication implements io.IO.
func (p *phpIO) HasAuthentication(repositoryName string) bool {
	v, err := p.call("hasAuthentication", repositoryName)

	return err == nil && php.ToBool(v)
}

// Authentication implements io.IO.
func (p *phpIO) Authentication(repositoryName string) io.Authentication {
	v, err := p.call("getAuthentication", repositoryName)
	if err != nil {
		return io.Authentication{}
	}

	return authentication(v)
}

// SetAuthentication implements io.IO.
func (p *phpIO) SetAuthentication(repositoryName, username string, password *string) {
	var pw any
	if password != nil {
		pw = *password
	}
	p.do("setAuthentication", repositoryName, username, pw)
}

// LoadConfiguration implements io.IO: the PHP IO's loadConfiguration()
// with maestro's Config (its process-timeout reaches maestro through
// ProcessExecutor::setTimeout(), which PHP syncs).
func (p *phpIO) LoadConfiguration(cfg io.Config, _ func(timeout int)) error {
	c, ok := cfg.(interface{ Config() *config.Config })
	if !ok {
		return nil
	}
	p.do("loadConfiguration", p.r.value(c.Config()))

	return nil
}

func contextValue(context *php.Array) *php.Array {
	if context == nil {
		return php.NewArray()
	}

	return context
}

// Emergency implements io.IO.
func (p *phpIO) Emergency(message string, context *php.Array) {
	p.do("emergency", message, contextValue(context))
}

// Alert implements io.IO.
func (p *phpIO) Alert(message string, context *php.Array) {
	p.do("alert", message, contextValue(context))
}

// Critical implements io.IO.
func (p *phpIO) Critical(message string, context *php.Array) {
	p.do("critical", message, contextValue(context))
}

// Error implements io.IO.
func (p *phpIO) Error(message string, context *php.Array) {
	p.do("error", message, contextValue(context))
}

// Warning implements io.IO.
func (p *phpIO) Warning(message string, context *php.Array) {
	p.do("warning", message, contextValue(context))
}

// Notice implements io.IO.
func (p *phpIO) Notice(message string, context *php.Array) {
	p.do("notice", message, contextValue(context))
}

// Info implements io.IO.
func (p *phpIO) Info(message string, context *php.Array) {
	p.do("info", message, contextValue(context))
}

// Debug implements io.IO.
func (p *phpIO) Debug(message string, context *php.Array) {
	p.do("debug", message, contextValue(context))
}

// Log implements io.IO.
func (p *phpIO) Log(level, message string, context *php.Array) {
	p.do("log", level, message, contextValue(context))
}

// phpIOFor returns maestro's proxy of an IO object created in PHP, the
// same each time (so an IO maestro hands back to PHP is the object PHP
// gave it).
func (r *Runtime) phpIOFor(obj *rpc.PHPObject) *phpIO {
	r.phpObjs.mu.Lock()
	defer r.phpObjs.mu.Unlock()

	if r.phpObjs.ios == nil {
		r.phpObjs.ios = map[*rpc.PHPObject]*phpIO{}
	}
	p, ok := r.phpObjs.ios[obj]
	if !ok {
		p = &phpIO{r: r, obj: obj}
		r.phpObjs.ios[obj] = p
	}

	return p
}

// goCallable is a Go function as PHP calls it: a Maestro\Shim\GoCallable
// whose __invoke() calls maestro (`callable.go`).
type goCallable struct {
	fn func(args []any) (any, error)
}

// PHPOpaque implements php.Opaque.
func (*goCallable) PHPOpaque() {}

// PHPClass implements rpc.Object.
func (*goCallable) PHPClass() string { return `Maestro\Shim\GoCallable` }

// goCallable returns the PHP callable standing for fn.
func (r *Runtime) goCallable(fn func(args []any) (any, error)) any {
	c := &goCallable{fn: fn}

	return r.bridge.object(c, func() rpc.Object { return c })
}

func (r *Runtime) registerGoCallables() {
	r.Handle("callable.go", func(v any) (any, error) {
		a := argsOf("callable.go", v)
		c, ok := a.at(0).(*goCallable)
		if !ok {
			return nil, a.errorf("param 0 is not a callable maestro knows")
		}
		var params []any
		if list, ok := a.at(1).(*php.Array); ok {
			params = list.Values()
		}

		return c.fn(params)
	})
}
