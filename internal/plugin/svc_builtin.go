// maestro's own commands run from PHP (docs/PLUGINS.md §5.7): `builtin.*`.
// PHP sees the commands of maestro's Application as instances of their
// Composer classes (commandMirror); when PHP code runs one
// ($app->find('install')->run($input, $output)), Symfony's Command::run()
// runs in PHP as in Composer, and the hooks it calls (initialize(),
// interact(), execute(), and the commands' own run(), isProxyCommand() and
// complete()) are maestro's command's, run here on the input and output
// PHP passed.

package plugin

import (
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
)

// bindBuiltin binds in to the definition of cmd merged with its
// Application's, as Symfony's Command::run() bound the input PHP holds
// before calling the hook: the same definition, so the same arguments and
// options. An input already bound to it (by an earlier hook of the same
// run) is left as it is, with what the hooks changed.
func bindBuiltin(cmd console.Commander, in console.Input) error {
	base := cmd.Base()
	if def := definitionOf(in); def != nil && def == base.Definition() {
		return nil
	}
	if err := base.MergeApplicationDefinition(true); err != nil {
		return err
	}
	// PHP's bind() reported what fails here already, or the command
	// ignores validation errors (Command::run()'s try/catch).
	if err := in.Bind(base.Definition()); err != nil && !console.IsConsoleException(err) {
		return err
	}

	return nil
}

func (r *Runtime) registerBuiltinCommands() {
	command := func(a args) (console.Commander, error) {
		m, ok := a.at(0).(*commandMirror)
		if !ok {
			return nil, a.errorf("param 0 is not one of maestro's commands")
		}

		return m.cmd, nil
	}
	hook := func(name string, fn func(cmd console.Commander, in console.Input, out console.Output) (any, error)) {
		r.Handle("builtin."+name, func(v any) (any, error) {
			a := argsOf("builtin."+name, v)
			cmd, err := command(a)
			if err != nil {
				return nil, err
			}
			in, err := inputParam(a, 1)
			if err != nil {
				return nil, err
			}
			out, err := r.outputParam(a, 2)
			if err != nil {
				return nil, err
			}

			return fn(cmd, in, out)
		})
	}

	// Command::run() bound the input and calls initialize(), then
	// interact() when it is interactive, then execute().
	hook("initialize", func(cmd console.Commander, in console.Input, out console.Output) (any, error) {
		if err := bindBuiltin(cmd, in); err != nil {
			return nil, err
		}
		if h, ok := cmd.(console.Initializer); ok {
			return nil, h.Initialize(in, out)
		}

		return nil, nil
	})
	hook("interact", func(cmd console.Commander, in console.Input, out console.Output) (any, error) {
		if err := bindBuiltin(cmd, in); err != nil {
			return nil, err
		}
		if h, ok := cmd.(console.Interactor); ok {
			return nil, h.Interact(in, out)
		}

		return nil, nil
	})
	hook("execute", func(cmd console.Commander, in console.Input, out console.Output) (any, error) {
		if err := bindBuiltin(cmd, in); err != nil {
			return nil, err
		}
		h, ok := cmd.(console.Executor)
		if !ok {
			return nil, unsupportedf("maestro does not support %s::execute() in plugins yet", commandClass(cmd))
		}
		code, err := h.Execute(in, out)

		return int64(code), err
	})
	// GlobalCommand overrides run() itself.
	hook("run", func(cmd console.Commander, in console.Input, out console.Output) (any, error) {
		code, err := cmd.Run(in, out)

		return int64(code), err
	})

	r.Handle("builtin.isProxyCommand", func(v any) (any, error) {
		a := argsOf("builtin.isProxyCommand", v)
		cmd, err := command(a)
		if err != nil {
			return nil, err
		}
		if p, ok := cmd.(interface{ IsProxyCommand() bool }); ok {
			return p.IsProxyCommand(), nil
		}

		return false, nil
	})
	// complete($input, $suggestions): maestro completes the tokens PHP's
	// CompletionInput holds, bound as Symfony's CompleteCommand binds it;
	// PHP adds the suggested values and options to its suggestions.
	r.Handle("builtin.complete", func(v any) (any, error) {
		a := argsOf("builtin.complete", v)
		cmd, err := command(a)
		if err != nil {
			return nil, err
		}
		in := console.CompletionInputFromTokens(stringList(a.at(1)), a.integer(2))
		base := cmd.Base()
		if err := base.MergeApplicationDefinition(true); err != nil {
			return nil, err
		}
		if err := in.Bind(base.Definition()); err != nil {
			return nil, err
		}
		s := &console.CompletionSuggestions{}
		cmd.Complete(in, s)

		values := php.NewArray()
		for _, v := range s.ValueSuggestions() {
			values.Append(v.Value)
		}
		options := php.NewArray()
		for _, o := range s.OptionSuggestions() {
			options.Append(o.Name())
		}

		return php.ArrayOf("values", values, "options", options), nil
	})
}

// commandClass is the PHP class maestro's command crosses as.
func commandClass(cmd console.Commander) string {
	return (&commandMirror{cmd: cmd}).PHPClass()
}
