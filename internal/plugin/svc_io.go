// IOInterface's methods (docs/PLUGINS.md §4.3, §5.9): `io.*`. maestro's IO
// is the only IO of a run; PHP's mirror calls it for everything but the
// verbosity, decoration and interactivity flags.

package plugin

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// messagesArg is a $messages param: one string or a list.
func messagesArg(a args, i int) (list []string, isList bool) {
	if arr, ok := a.at(i).(*php.Array); ok {
		for _, v := range arr.Values() {
			list = append(list, php.ToString(v))
		}

		return list, true
	}

	return []string{a.str(i)}, false
}

// newlineArg is a `bool $newline = true` param.
func newlineArg(a args, i int) bool {
	if !a.has(i) {
		return true
	}

	return a.boolean(i)
}

// verbosityArg is an `int $verbosity = self::NORMAL` param.
func verbosityArg(a args, i int) io.Verbosity {
	if !a.has(i) {
		return io.Normal
	}

	return io.Verbosity(a.integer(i))
}

// nullableStr is a ?string as Go holds it.
func nullableStr(v any) *string {
	if v == nil {
		return nil
	}
	s := php.ToString(v)

	return &s
}

func authenticationValue(auth io.Authentication) *php.Array {
	var user, pass any
	if auth.Username != nil {
		user = *auth.Username
	}
	if auth.Password != nil {
		pass = *auth.Password
	}

	return php.ArrayOf("username", user, "password", pass)
}

func (r *Runtime) registerIO() {
	method := func(name string, fn func(out io.IO, a args) (any, error)) {
		r.Handle("io."+name, func(v any) (any, error) {
			a := argsOf("io."+name, v)
			m, ok := a.at(0).(*ioMirror)
			if !ok {
				return nil, a.errorf("param 0 is not an IO maestro knows")
			}

			return fn(m.io, a)
		})
	}
	write := func(name string, single, multi func(out io.IO) func([]string, bool, io.Verbosity)) {
		method(name, func(out io.IO, a args) (any, error) {
			messages, isList := messagesArg(a, 1)
			fn := single(out)
			if isList {
				fn = multi(out)
			}
			fn(messages, newlineArg(a, 2), verbosityArg(a, 3))

			return nil, nil
		})
	}
	one := func(fn func(string, bool, io.Verbosity)) func([]string, bool, io.Verbosity) {
		return func(m []string, newline bool, v io.Verbosity) { fn(m[0], newline, v) }
	}

	write("write",
		func(out io.IO) func([]string, bool, io.Verbosity) { return one(out.Write) },
		func(out io.IO) func([]string, bool, io.Verbosity) { return out.WriteMessages })
	write("writeError",
		func(out io.IO) func([]string, bool, io.Verbosity) { return one(out.WriteError) },
		func(out io.IO) func([]string, bool, io.Verbosity) { return out.WriteErrorMessages })
	write("writeRaw",
		func(out io.IO) func([]string, bool, io.Verbosity) { return one(out.WriteRaw) },
		func(out io.IO) func([]string, bool, io.Verbosity) {
			return func(m []string, newline bool, v io.Verbosity) { out.WriteRaw(joinMessages(m, newline), newline, v) }
		})
	write("writeErrorRaw",
		func(out io.IO) func([]string, bool, io.Verbosity) { return one(out.WriteErrorRaw) },
		func(out io.IO) func([]string, bool, io.Verbosity) {
			return func(m []string, newline bool, v io.Verbosity) {
				out.WriteErrorRaw(joinMessages(m, newline), newline, v)
			}
		})

	overwrite := func(name string, fn func(out io.IO) func(string, bool, int, io.Verbosity)) {
		method(name, func(out io.IO, a args) (any, error) {
			messages, isList := messagesArg(a, 1)
			message := messages[0]
			if isList {
				message = joinMessages(messages, newlineArg(a, 2))
			}
			size := -1
			if a.has(3) {
				size = a.integer(3)
			}
			fn(out)(message, newlineArg(a, 2), size, verbosityArg(a, 4))

			return nil, nil
		})
	}
	overwrite("overwrite", func(out io.IO) func(string, bool, int, io.Verbosity) { return out.Overwrite })
	overwrite("overwriteError", func(out io.IO) func(string, bool, int, io.Verbosity) { return out.OverwriteError })

	method("ask", func(out io.IO, a args) (any, error) { return out.Ask(a.str(1), a.at(2)) })
	method("askConfirmation", func(out io.IO, a args) (any, error) {
		def := true
		if a.has(2) {
			def = a.boolean(2)
		}

		return out.AskConfirmation(a.str(1), def)
	})
	method("askAndValidate", func(out io.IO, a args) (any, error) {
		callable := a.at(2)
		validator := func(answer any) (any, error) {
			return r.Call("callable.invoke", php.ArrayOf("callable", callable, "args", php.ListOf(answer)))
		}
		attempts := 0
		if a.has(3) {
			attempts = a.integer(3)
		}

		return out.AskAndValidate(a.str(1), validator, attempts, a.at(4))
	})
	method("askAndHideAnswer", func(out io.IO, a args) (any, error) { return out.AskAndHideAnswer(a.str(1)) })
	method("select", func(out io.IO, a args) (any, error) {
		attempts := 0
		if v := a.at(4); v != nil && v != false {
			attempts = a.integer(4)
		}
		errorMessage := io.DefaultSelectErrorMessage
		if a.has(5) {
			errorMessage = a.str(5)
		}

		return out.Select(a.str(1), a.arrayOrEmpty(2), a.at(3), attempts, errorMessage, a.boolean(6))
	})

	method("getAuthentications", func(out io.IO, _ args) (any, error) {
		auths := php.NewArray()
		for _, ra := range out.Authentications() {
			auths.Set(ra.Repository, authenticationValue(ra.Authentication))
		}

		return auths, nil
	})
	method("resetAuthentications", func(out io.IO, a args) (any, error) {
		reset, ok := out.(interface{ ResetAuthentications() })
		if !ok {
			return nil, a.errorf("%T cannot reset its authentications", out)
		}
		reset.ResetAuthentications()

		return nil, nil
	})
	method("hasAuthentication", func(out io.IO, a args) (any, error) { return out.HasAuthentication(a.str(1)), nil })
	method("getAuthentication", func(out io.IO, a args) (any, error) {
		return authenticationValue(out.Authentication(a.str(1))), nil
	})
	method("setAuthentication", func(out io.IO, a args) (any, error) {
		out.SetAuthentication(a.str(1), a.str(2), nullableStr(a.at(3)))

		return nil, nil
	})
	method("checkAndSetAuthentication", func(out io.IO, a args) (any, error) {
		name, username, password := a.str(1), a.str(2), nullableStr(a.at(3))
		if out.HasAuthentication(name) {
			auth := out.Authentication(name)
			if auth.Username != nil && *auth.Username == username && sameNullable(auth.Password, password) {
				return nil, nil
			}

			out.WriteError(fmt.Sprintf("<warning>Warning: You should avoid overwriting already defined auth settings for %s.</warning>", name), true, io.Normal)
		}
		out.SetAuthentication(name, username, password)

		return nil, nil
	})
	method("loadConfiguration", func(out io.IO, a args) (any, error) {
		cfg, err := param[*config.Config](a, 1)
		if err != nil {
			return nil, err
		}
		return nil, out.LoadConfiguration(cfg.ForIO(), util.SetProcessTimeout)
	})
	// ConsoleIO::enableDebugging($startTime): microtime(true) in PHP.
	method("enableDebugging", func(out io.IO, a args) (any, error) {
		c, ok := out.(interface{ EnableDebugging(time.Time) })
		if !ok {
			return nil, unsupportedf("maestro does not support %s::enableDebugging() in plugins yet", ioClass(out))
		}
		sec, frac := math.Modf(php.ToFloat(a.at(1)))
		c.EnableDebugging(time.Unix(int64(sec), int64(frac*1e9)))

		return nil, nil
	})
	// ConsoleIO::enableTimestamps($format): each message prefixed with
	// (new \DateTime())->format($format), in PHP's default time zone
	// (param 2).
	method("enableTimestamps", func(out io.IO, a args) (any, error) {
		c, ok := out.(interface {
			EnableTimestampsFunc(func(time.Time) string)
		})
		if !ok {
			return nil, unsupportedf("maestro does not support %s::enableTimestamps() in plugins yet", ioClass(out))
		}
		loc, err := time.LoadLocation(a.str(2))
		if err != nil {
			loc = time.UTC
		}
		format := a.str(1)
		c.EnableTimestampsFunc(func(t time.Time) string { return php.DateFormat(format, t.In(loc)) })

		return nil, nil
	})
	method("getOutput", func(out io.IO, a args) (any, error) {
		b, ok := out.(*io.BufferIO)
		if !ok {
			return nil, a.errorf("%T is not a BufferIO", out)
		}

		return b.Output(), nil
	})
	method("setUserInputs", func(out io.IO, a args) (any, error) {
		b, ok := out.(*io.BufferIO)
		if !ok {
			return nil, a.errorf("%T is not a BufferIO", out)
		}
		var inputs []string
		for _, v := range a.arrayOrEmpty(1).Values() {
			inputs = append(inputs, php.ToString(v))
		}
		b.SetUserInputs(inputs)

		return nil, nil
	})
	r.Handle("io.sanitize", func(v any) (any, error) {
		a := argsOf("io.sanitize", v)
		allowNewlines := true
		if a.has(1) {
			allowNewlines = a.boolean(1)
		}
		messages, isList := messagesArg(a, 0)
		if isList {
			return php.StringList(io.SanitizeMessages(messages, allowNewlines)), nil
		}

		return io.Sanitize(messages[0], allowNewlines), nil
	})
}

func sameNullable(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}

	return *a == *b
}

// joinMessages is implode($newline ? "\n" : ”, $messages).
func joinMessages(messages []string, newline bool) string {
	if newline {
		return strings.Join(messages, "\n")
	}

	return strings.Join(messages, "")
}

// ioClass is the PHP class maestro's IO crosses as.
func ioClass(out io.IO) string { return (&ioMirror{io: out}).PHPClass() }
