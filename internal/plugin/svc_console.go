// Symfony Console objects crossing the channel (docs/PLUGINS.md §5.7,
// §5.9, §5.11): input definitions as values, maestro's inputs and outputs
// as PHP mirrors (real vendored ArgvInput/ArrayInput/StringInput objects,
// a vendored ConsoleOutput writing to the inherited fds or a
// Maestro\Shim\GoOutput writing through maestro), and PHP's outputs in
// maestro (phpOutput). php/src/Maestro/Shim/Console.php and the Input,
// Output and Command adapters are the PHP half.

package plugin

import (
	"fmt"
	"slices"
	"sync"

	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
)

// The mirror families of console objects. They are not shim classes:
// their PHP objects are the vendored Symfony classes themselves, so the
// shim's adapters are found by these names (Maestro\Shim\Mirrors keeps the
// adapter of each such object).
const (
	inputBase   = `Maestro\Shim\Mirror\Input`
	outputBase  = `Maestro\Shim\Mirror\Output`
	commandBase = `Maestro\Shim\Mirror\Command`
	appBase     = `Maestro\Shim\Mirror\Application`
)

// The PHP classes of console objects.
const (
	classArgvInput     = `Symfony\Component\Console\Input\ArgvInput`
	classArrayInput    = `Symfony\Component\Console\Input\ArrayInput`
	classStringInput   = `Symfony\Component\Console\Input\StringInput`
	classConsoleOutput = `Symfony\Component\Console\Output\ConsoleOutput`
	classGoOutput      = `Maestro\Shim\GoOutput`
	classGoConsoleOut  = `Maestro\Shim\GoConsoleOutput`
	classApplication   = `Composer\Console\Application`
	classSymfonyCmd    = `Symfony\Component\Console\Command\Command`
)

// phpConsoleValue is an input value as PHP holds it: ints are int64,
// lists are arrays.
func phpConsoleValue(v any) any {
	switch v := v.(type) {
	case int:
		return int64(v)
	case []string:
		return php.StringList(v)
	case []any:
		a := php.NewArrayCap(len(v))
		for _, item := range v {
			a.Append(phpConsoleValue(item))
		}

		return a
	}

	return v
}

// consoleValue is a PHP value as maestro's console holds it: ints are int,
// arrays are lists of their values.
func consoleValue(v any) any {
	switch v := v.(type) {
	case int64:
		return int(v)
	case *php.Array:
		list := make([]any, 0, v.Len())
		for _, item := range v.Values() {
			list = append(list, consoleValue(item))
		}

		return list
	}

	return v
}

// argumentMode and optionMode are the modes of a definition's items, as
// PHP's constructors take them.
func argumentMode(a *console.InputArgument) int64 {
	mode := int64(console.ArgumentOptional)
	if a.IsRequired() {
		mode = console.ArgumentRequired
	}
	if a.IsArray() {
		mode |= console.ArgumentIsArray
	}

	return mode
}

func optionMode(o *console.InputOption) int64 {
	var mode int64
	switch {
	case o.IsValueRequired():
		mode = console.OptionValueRequired
	case o.IsValueOptional():
		mode = console.OptionValueOptional
	default:
		mode = console.OptionValueNone
	}
	if o.IsArray() {
		mode |= console.OptionValueIsArray
	}
	if o.IsNegatable() {
		mode |= console.OptionValueNegatable
	}

	return mode
}

// definitionValue is an input definition as PHP rebuilds it
// (Maestro\Shim\Definitions::build).
func definitionValue(d *console.InputDefinition) *php.Array {
	if d == nil {
		d = &console.InputDefinition{}
	}
	args := php.NewArrayCap(len(d.Arguments()))
	for _, a := range d.Arguments() {
		var def any
		if !a.IsRequired() {
			def = phpConsoleValue(a.Default())
		}
		args.Append(php.ArrayOf("name", a.Name(), "mode", argumentMode(a), "description", a.Description(), "default", def))
	}
	opts := php.NewArrayCap(len(d.Options()))
	for _, o := range d.Options() {
		var shortcut, def any
		if o.Shortcut() != "" {
			shortcut = o.Shortcut()
		}
		if o.AcceptValue() || o.IsNegatable() {
			def = phpConsoleValue(o.Default())
		}
		opts.Append(php.ArrayOf("name", o.Name(), "shortcut", shortcut, "mode", optionMode(o), "description", o.Description(), "default", def))
	}

	return php.ArrayOf("arguments", args, "options", opts)
}

// definitionFromValue builds the input definition PHP described
// (Maestro\Shim\Definitions::describe).
func definitionFromValue(v *php.Array) (*console.InputDefinition, error) {
	var items []any
	args, _ := v.GetArray("arguments")
	for _, item := range args.Values() {
		a, ok := item.(*php.Array)
		if !ok {
			return nil, &rpc.ProtocolError{Message: "an invalid input argument"}
		}
		name, _ := a.GetString("name")
		mode, _ := a.Get("mode")
		description, _ := a.GetString("description")
		def, _ := a.Get("default")
		arg, err := console.NewInputArgument(name, int(php.ToInt(mode)), description, consoleValue(def))
		if err != nil {
			return nil, err
		}
		items = append(items, arg)
	}
	opts, _ := v.GetArray("options")
	for _, item := range opts.Values() {
		o, ok := item.(*php.Array)
		if !ok {
			return nil, &rpc.ProtocolError{Message: "an invalid input option"}
		}
		name, _ := o.GetString("name")
		shortcut, _ := o.GetString("shortcut")
		mode, _ := o.Get("mode")
		description, _ := o.GetString("description")
		def, _ := o.Get("default")
		m := int(php.ToInt(mode))
		if m&console.OptionValueNone != 0 {
			def = nil
		}
		opt, err := console.NewInputOption(name, shortcut, m, description, consoleValue(def))
		if err != nil {
			return nil, err
		}
		items = append(items, opt)
	}

	return console.NewInputDefinition(items...)
}

// namedValues is a list of input values as a PHP array.
func namedValues(values []console.NamedValue) *php.Array {
	a := php.NewArrayCap(len(values))
	for _, nv := range values {
		a.Set(nv.Name, phpConsoleValue(nv.Value))
	}

	return a
}

// definitionOf returns the definition an input is bound to.
func definitionOf(in console.Input) *console.InputDefinition {
	if d, ok := in.(interface {
		Definition() *console.InputDefinition
	}); ok {
		return d.Definition()
	}

	return nil
}

// inputMirror is maestro's input as PHP mirrors it: a vendored ArgvInput,
// ArrayInput or StringInput with the same tokens or parameters, definition,
// arguments, options and interactivity (docs/PLUGINS.md §5.9). What PHP
// changes in it (setArgument(), setOption(), setInteractive() and the
// tokens, while it stays bound to maestro's definition) comes back.
type inputMirror struct {
	in console.Input

	mu   sync.Mutex
	last string
	rev  uint64
}

// PHPOpaque implements php.Opaque.
func (*inputMirror) PHPOpaque() {}

// PHPClass implements rpc.Object.
func (m *inputMirror) PHPClass() string {
	switch m.in.(type) {
	case *console.StringInput:
		return classStringInput
	case *console.ArrayInput:
		return classArrayInput
	}

	return classArgvInput
}

// MirrorBase implements rpc.Mirror.
func (*inputMirror) MirrorBase() string { return inputBase }

func (m *inputMirror) state() string {
	var tokens []string
	if t, ok := m.in.(interface{ Tokens() []string }); ok {
		tokens = t.Tokens()
	}

	return fmt.Sprintf("%p|%v|%v|%v|%q", definitionOf(m.in), m.in.Arguments(), m.in.Options(), m.in.IsInteractive(), tokens)
}

// Rev implements rpc.Mirror: it moves when the input changed since it was
// last asked.
func (m *inputMirror) Rev() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	if st := m.state(); st != m.last {
		m.last = st
		m.rev++
	}

	return m.rev
}

// MirrorSnapshot implements rpc.Mirror.
func (m *inputMirror) MirrorSnapshot() (*php.Array, error) {
	s := php.ArrayOf(
		"definition", definitionValue(definitionOf(m.in)),
		"arguments", namedValues(m.in.Arguments()),
		"options", namedValues(m.in.Options()),
		"interactive", m.in.IsInteractive(),
	)
	switch in := m.in.(type) {
	case *console.ArrayInput:
		params := php.NewArray()
		for _, p := range in.Parameters() {
			v := phpConsoleValue(p.Value)
			if p.Positional {
				params.Set(int64(p.Index), v)
			} else {
				params.Set(p.Key, v)
			}
		}
		s.Set("parameters", params)
	case interface{ Tokens() []string }:
		s.Set("tokens", php.StringList(in.Tokens()))
	default:
		return nil, fmt.Errorf("plugin: a %T input cannot cross to PHP", m.in)
	}

	return s, nil
}

// ApplyMirror implements rpc.Mirror: what PHP changed in the input.
func (m *inputMirror) ApplyMirror(fields *php.Array) error {
	def := definitionOf(m.in)
	if args, ok := fields.GetArray("arguments"); ok {
		for k, v := range args.All() {
			if name := k.String(); def != nil && def.HasArgument(name) {
				m.in.SetArgument(name, consoleValue(v))
			}
		}
	}
	if opts, ok := fields.GetArray("options"); ok {
		for k, v := range opts.All() {
			if name := k.String(); def != nil && def.HasOption(name) {
				m.in.SetOption(name, consoleValue(v))
			}
		}
	}
	if v, ok := fields.Get("interactive"); ok {
		m.in.SetInteractive(php.ToBool(v))
	}
	if tokens, ok := fields.GetArray("tokens"); ok {
		if t, ok := m.in.(interface{ SetTokens([]string) }); ok {
			var list []string
			for _, v := range tokens.Values() {
				list = append(list, php.ToString(v))
			}
			t.SetTokens(list)
		}
	}

	return nil
}

// inputObject returns the object an input crosses to PHP as.
func (r *Runtime) inputObject(in console.Input) any {
	if in == nil || !hashable(in) {
		return nil
	}
	return r.bridge.object(in, func() rpc.Object {
		m := &inputMirror{in: in}
		m.last = m.state()

		return m
	})
}

// inputParam returns param i as an input: maestro's own (or one created in
// PHP, which maestro adopted when it first crossed: adoptInput), or one
// built from the description of a PHP input that crossed before as a plain
// object (Maestro\Shim\Console::inputValue).
func inputParam(a args, i int) (console.Input, error) {
	switch v := a.at(i).(type) {
	case *inputMirror:
		return v.in, nil
	case *php.Array:
		return inputFromValue(v)
	}

	return nil, a.errorf("param %d is a %T, not an input", i, a.at(i))
}

// inputFromValue builds the input a PHP input described.
func inputFromValue(v *php.Array) (console.Input, error) {
	var in console.Input
	switch kind, _ := v.GetString("kind"); kind {
	case "array":
		var params []console.Param
		if p, ok := v.GetArray("parameters"); ok {
			for k, val := range p.All() {
				if k.IsInt() {
					params = append(params, console.PI(int(k.Int()), consoleValue(val)))
				} else {
					params = append(params, console.P(k.String(), consoleValue(val)))
				}
			}
		}
		ai, err := console.NewArrayInput(params, nil)
		if err != nil {
			return nil, err
		}
		in = ai
	case "argv", "string":
		var tokens []string
		if t, ok := v.GetArray("tokens"); ok {
			for _, tok := range t.Values() {
				tokens = append(tokens, php.ToString(tok))
			}
		}
		argv, err := console.NewArgvInput([]string{""}, nil)
		if err != nil {
			return nil, err
		}
		argv.SetTokens(tokens)
		in = argv
		if kind == "string" {
			in = &console.StringInput{ArgvInput: argv}
		}
	default:
		return nil, &rpc.ProtocolError{Message: "an input of an unknown kind " + kind}
	}
	if interactive, ok := v.Get("interactive"); ok {
		in.SetInteractive(php.ToBool(interactive))
	}

	return in, nil
}

// adoptInput builds the Go side of an input created in PHP when it first
// crosses to maestro (passed to an Application's run()): maestro runs it,
// and PHP's object mirrors maestro's from then on (so binding it binds the
// object PHP holds, as in Composer).
func (r *Runtime) adoptInput(_ string, snapshot *php.Array) (rpc.Mirror, error) {
	in, err := inputFromValue(snapshot)
	if err != nil {
		return nil, err
	}
	m := &inputMirror{in: in}
	m.last = m.state()
	r.bridge.object(in, func() rpc.Object { return m })

	return m, nil
}

// outputMirror is maestro's output as PHP mirrors it (docs/PLUGINS.md
// §5.9, D11): for maestro's console output on the inherited stdout and
// stderr, a vendored ConsoleOutput writing to them directly; for any other
// output a Maestro\Shim\GoOutput (a GoConsoleOutput when it has an error
// output) whose writes, already formatted by PHP, come to maestro. Its
// verbosity and decoration follow maestro's, and PHP's changes to them
// come back.
type outputMirror struct {
	r   *Runtime
	out console.Output

	mu   sync.Mutex
	last outputState
	rev  uint64
}

type outputState struct {
	verbosity int
	decorated bool
}

func (m *outputMirror) state() outputState {
	return outputState{verbosity: m.out.Verbosity(), decorated: m.out.IsDecorated()}
}

// direct reports whether the output is maestro's console output on the
// child's own stdout and stderr, which PHP can write to itself.
func (m *outputMirror) direct() bool {
	co, ok := m.out.(*console.ConsoleOutput)
	if !ok || co.Stream() != any(m.r.opts.Stdout) {
		return false
	}
	errOut, ok := co.ErrorOutput().(*console.StreamOutput)

	return ok && errOut.Stream() == any(m.r.opts.Stderr)
}

// PHPOpaque implements php.Opaque.
func (*outputMirror) PHPOpaque() {}

// PHPClass implements rpc.Object.
func (m *outputMirror) PHPClass() string {
	if m.direct() {
		return classConsoleOutput
	}
	if _, ok := m.out.(console.ConsoleOutputInterface); ok {
		return classGoConsoleOut
	}

	return classGoOutput
}

// MirrorBase implements rpc.Mirror.
func (*outputMirror) MirrorBase() string { return outputBase }

// Rev implements rpc.Mirror.
func (m *outputMirror) Rev() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	if st := m.state(); st != m.last {
		m.last = st
		m.rev++
	}

	return m.rev
}

// MirrorSnapshot implements rpc.Mirror.
func (m *outputMirror) MirrorSnapshot() (*php.Array, error) {
	st := m.state()

	return php.ArrayOf("verbosity", int64(st.verbosity), "decorated", st.decorated), nil
}

// ApplyMirror implements rpc.Mirror: PHP set the verbosity or decoration.
func (m *outputMirror) ApplyMirror(fields *php.Array) error {
	if v, ok := fields.Get("verbosity"); ok {
		m.out.SetVerbosity(int(php.ToInt(v)))
	}
	if v, ok := fields.Get("decorated"); ok {
		m.out.SetDecorated(php.ToBool(v))
	}

	return nil
}

// outputObject returns the object an output crosses to PHP as.
func (r *Runtime) outputObject(out console.Output) any {
	if out == nil || !hashable(out) {
		return nil
	}
	if p, ok := out.(phpOutputObject); ok {
		return p.phpObject()
	}

	return r.bridge.object(out, func() rpc.Object {
		m := &outputMirror{r: r, out: out}
		m.last = m.state()

		return m
	})
}

// ioOutput is the output of an IO that is a ConsoleIO (ConsoleIO's
// protected $output); nil for any other IO.
func ioOutput(out io.IO) console.Output {
	if c, ok := out.(interface{ ConsoleOutput() console.Output }); ok {
		return c.ConsoleOutput()
	}

	return nil
}

// outputParam returns param i as an output: maestro's own, or a proxy of
// a PHP output object; nil for null.
func (r *Runtime) outputParam(a args, i int) (console.Output, error) {
	switch v := a.at(i).(type) {
	case nil:
		return nil, nil
	case *outputMirror:
		return v.out, nil
	case *php.Array:
		return r.phpOutputFromValue(v)
	}

	return nil, a.errorf("param %d is a %T, not an output", i, a.at(i))
}

// phpOutputObject is implemented by maestro's proxies of PHP outputs.
type phpOutputObject interface{ phpObject() *rpc.PHPObject }

// phpOutput is an output created in PHP (a BufferedOutput, a plugin's
// own) as maestro writes to it: every method is the PHP object's, so PHP's
// formatter and verbosity apply (docs/PLUGINS.md §5.11 proxyOutput).
// Failures of the calls (PHP ending) are reported by the next call that
// can return one.
type phpOutput struct {
	r   *Runtime
	obj *rpc.PHPObject
	// The PHP output's verbosity and decoration as maestro last knew or
	// set them: maestro reads them while it syncs state with PHP, where it
	// cannot call PHP.
	verbosity int
	decorated bool
}

// phpConsoleOutput is a PHP ConsoleOutputInterface.
type phpConsoleOutput struct {
	*phpOutput
	errOut console.Output
}

func (o *phpOutput) phpObject() *rpc.PHPObject { return o.obj }

// phpOutputFromValue builds the proxy of a PHP output
// (Maestro\Shim\Console::describeOutput).
func (r *Runtime) phpOutputFromValue(v *php.Array) (console.Output, error) {
	obj, ok := v.Get("object")
	o, isObj := obj.(*rpc.PHPObject)
	if !ok || !isObj {
		return nil, &rpc.ProtocolError{Message: "an output without its object"}
	}
	verbosity, _ := v.Get("verbosity")
	decorated, _ := v.Get("decorated")
	out := &phpOutput{r: r, obj: o, verbosity: int(php.ToInt(verbosity)), decorated: php.ToBool(decorated)}
	if errDesc, ok := v.GetArray("error"); ok {
		errOut, err := r.phpOutputFromValue(errDesc)
		if err != nil {
			return nil, err
		}

		return &phpConsoleOutput{phpOutput: out, errOut: errOut}, nil
	}

	return out, nil
}

func (o *phpOutput) call(method string, params ...any) any {
	v, _ := o.r.Call("object.call", php.ArrayOf("object", o.obj, "method", method, "args", php.ListOf(params...)))

	return v
}

// Write implements console.Output.
func (o *phpOutput) Write(message string, newline bool, options int) {
	o.call("write", message, newline, int64(options))
}

// Writeln implements console.Output.
func (o *phpOutput) Writeln(message string) { o.Write(message, true, console.OutputNormal) }

// SetVerbosity implements console.Output.
func (o *phpOutput) SetVerbosity(level int) {
	o.verbosity = level
	o.call("setVerbosity", int64(level))
}

// Verbosity implements console.Output.
func (o *phpOutput) Verbosity() int { return o.verbosity }

// IsQuiet implements console.Output.
func (o *phpOutput) IsQuiet() bool { return o.Verbosity() == console.VerbosityQuiet }

// IsVerbose implements console.Output.
func (o *phpOutput) IsVerbose() bool { return o.Verbosity() >= console.VerbosityVerbose }

// IsVeryVerbose implements console.Output.
func (o *phpOutput) IsVeryVerbose() bool { return o.Verbosity() >= console.VerbosityVeryVerbose }

// IsDebug implements console.Output.
func (o *phpOutput) IsDebug() bool { return o.Verbosity() >= console.VerbosityDebug }

// SetDecorated implements console.Output.
func (o *phpOutput) SetDecorated(decorated bool) {
	o.decorated = decorated
	o.call("setDecorated", decorated)
}

// IsDecorated implements console.Output.
func (o *phpOutput) IsDecorated() bool { return o.decorated }

// SetFormatter implements console.Output: maestro's formatters do not
// cross; the PHP output keeps its own.
func (*phpOutput) SetFormatter(console.Formatter) {}

// Formatter implements console.Output: a formatter like the PHP output's
// (its decoration, Composer's styles), for maestro code formatting text
// itself; the output formats what it is given.
func (o *phpOutput) Formatter() console.Formatter {
	return console.NewOutputFormatter(o.IsDecorated(), composer.CreateAdditionalStyles()...)
}

// ErrorOutput implements console.ConsoleOutputInterface.
func (o *phpConsoleOutput) ErrorOutput() console.Output { return o.errOut }

// SetErrorOutput implements console.ConsoleOutputInterface.
func (o *phpConsoleOutput) SetErrorOutput(out console.Output) { o.errOut = out }

func (r *Runtime) registerConsole() {
	r.RegisterMirrorFactory(inputBase, r.adoptInput)

	r.Handle("output.write", func(v any) (any, error) {
		a := argsOf("output.write", v)
		m, ok := a.at(0).(*outputMirror)
		if !ok {
			return nil, a.errorf("param 0 is not an output maestro knows")
		}
		// PHP formatted and filtered the message: written as is, whatever
		// the verbosity.
		m.out.Write(a.str(1), a.boolean(2), console.OutputRaw|console.VerbosityQuiet)

		return nil, nil
	})
	r.Handle("output.errorOutput", func(v any) (any, error) {
		a := argsOf("output.errorOutput", v)
		m, ok := a.at(0).(*outputMirror)
		if !ok {
			return nil, a.errorf("param 0 is not an output maestro knows")
		}

		return r.outputObject(console.ErrorOutputOf(m.out)), nil
	})

	r.registerApplications()
}

// stringList is a PHP list of strings (nil for anything else).
func stringList(v any) []string {
	a, ok := v.(*php.Array)
	if !ok {
		return nil
	}
	out := make([]string, 0, a.Len())
	for _, item := range a.Values() {
		out = append(out, php.ToString(item))
	}

	return slices.Clip(out)
}
