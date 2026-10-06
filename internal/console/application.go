// Ports src/Application.php (symfony/console).

package console

import (
	"errors"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
)

// Application overrides. A type embedding *Application implements any of
// these and registers itself with SetImpl, like a PHP subclass overriding
// the corresponding methods.
type (
	// AppDoRunner overrides doRun().
	AppDoRunner interface {
		DoRun(in Input, out Output) (int, error)
	}
	// AppLongVersioner overrides getLongVersion().
	AppLongVersioner interface{ LongVersion() string }
	// AppHelper overrides getHelp().
	AppHelper interface{ Help() string }
	// AppDefaultDefinitioner overrides getDefaultInputDefinition().
	AppDefaultDefinitioner interface {
		DefaultInputDefinition() *InputDefinition
	}
	// AppDefaultCommander overrides getDefaultCommands().
	AppDefaultCommander interface{ DefaultCommands() []Commander }
	// AppDefaultHelperSetter overrides getDefaultHelperSet().
	AppDefaultHelperSetter interface{ DefaultHelperSet() *HelperSet }
	// AppConfigurer overrides configureIO().
	AppConfigurer interface {
		ConfigureIO(in Input, out Output)
	}
	// AppRenderer overrides doRenderThrowable().
	AppRenderer interface {
		DoRenderThrowable(err error, out Output)
	}
	// AppDoRunCommander overrides doRunCommand().
	AppDoRunCommander interface {
		DoRunCommand(command Commander, in Input, out Output) (int, error)
	}
)

// Application is a collection of commands.
type Application struct {
	self     any
	callHook CallHook

	commands       map[string]Commander
	commandOrder   []string
	wantHelps      bool
	runningCommand Commander
	name           string
	version        string
	catchErrors    bool
	autoExit       bool
	definition     *InputDefinition
	helperSet      *HelperSet
	terminal       Terminal
	defaultCommand string
	singleCommand  bool
	initialized    bool
	// runCallers are the frames above run() in the PHP stack, innermost
	// first (SetRunCallers).
	runCallers []phperr.Frame
	// stack holds the frames of the calls in progress (Call), outermost
	// first.
	stack []phperr.Frame
}

// The symfony/console files exceptions' traces name.
const (
	applicationPHP = "vendor/symfony/console/Application.php"
	commandPHP     = "vendor/symfony/console/Command/Command.php"
)

// SetRunCallers sets the frames of the PHP stack above run(), innermost
// first, which Run adds to the trace of the exception it renders (a
// subclass's run() calling parent::run(), and the script calling that).
func (a *Application) SetRunCallers(frames ...phperr.Frame) { a.runCallers = frames }

// RunCallers returns the frames SetRunCallers set.
func (a *Application) RunCallers() []phperr.Frame { return a.runCallers }

// Call runs fn as PHP's call of f.Function at f.File:f.Line: the frame is
// on the application's stack while fn runs (Stack), and is added to the
// trace of the error fn returns (phperr.Call).
func (a *Application) Call(f phperr.Frame, fn func() (int, error)) (int, error) {
	return a.CallOn(f, nil, nil, fn)
}

// CallHook is told of the calls of methods on objects (CallOn) as they
// start, and returns what to do as they end (nil for nothing): the plugin
// runtime has the objects on the PHP call stack while plugin code runs
// under them (docs/PLUGINS.md §5.12).
type CallHook func(f phperr.Frame, object any, args []any) (end func())

// SetCallHook sets the hook CallOn tells of its calls.
func (a *Application) SetCallHook(h CallHook) { a.callHook = h }

// CallOn is Call of a method of object with args, which the CallHook is
// told of.
func (a *Application) CallOn(f phperr.Frame, object any, args []any, fn func() (int, error)) (int, error) {
	if a.callHook != nil && object != nil {
		if end := a.callHook(f, object, args); end != nil {
			defer end()
		}
	}
	n := len(a.stack)
	a.stack = append(a.stack, f)
	// popped on a panic too (runGuarded recovers Throwable panics)
	defer func() { a.stack = a.stack[:n] }()
	code, err := fn()

	return code, phperr.Call(err, f.Function, f.File, f.Line)
}

// Stack returns the frames of the PHP stack from the innermost call in
// progress (Call) up to the script: what the trace of an exception
// constructed now holds above the caller of the innermost Call. A command
// running the application again (OutdatedCommand) has its run() render
// the exceptions with the frames of the outer run.
func (a *Application) Stack() []phperr.Frame {
	frames := make([]phperr.Frame, 0, len(a.stack)+len(a.runCallers))
	for _, f := range slices.Backward(a.stack) {
		frames = append(frames, f)
	}

	return append(frames, a.runCallers...)
}

// appClass is the class declaring the application's method: the
// subclass's (ClassNamer) when it overrides it, Symfony's otherwise.
func (a *Application) appClass(overrides bool) string {
	if n, ok := a.self.(ClassNamer); ok && overrides {
		return n.ClassName()
	}

	return `Symfony\Component\Console\Application`
}

// NewApplication mirrors new Application($name, $version).
func NewApplication(name, version string) *Application {
	return &Application{
		commands:       map[string]Commander{},
		name:           name,
		version:        version,
		catchErrors:    true,
		autoExit:       true,
		defaultCommand: "list",
	}
}

// SetImpl registers the type embedding a, whose overrides a calls.
func (a *Application) SetImpl(impl any) { a.self = impl }

// Impl returns the type registered with SetImpl (nil if none): what PHP's
// $command->getApplication() returns when the application is a subclass.
func (a *Application) Impl() any { return a.self }

// Run runs the application. Errors are rendered and turned into an exit code
// unless SetCatchExceptions(false) was called; with auto-exit enabled (the
// default) the process exits with the code.
func (a *Application) Run(in Input, out Output) (exitCode int, err error) {
	_ = os.Setenv("LINES", strconv.Itoa(a.terminal.Height()))
	_ = os.Setenv("COLUMNS", strconv.Itoa(a.terminal.Width()))

	if in == nil {
		argv, _ := NewArgvInput(nil, nil)
		in = argv
	}
	if out == nil {
		out = NewConsoleOutput(VerbosityNormal, nil, nil)
	}

	renderException := func(e error) {
		a.RenderThrowable(e, ErrorOutputOf(out))
	}

	exitCode, err = a.runGuarded(in, out)
	if err != nil {
		// the frames above run() in the exception's trace
		phperr.Calls(err, a.runCallers...)

		if !a.catchErrors {
			return exitCode, err
		}

		renderException(err)

		// $e->getCode() of the exception itself, 1 unless positive.
		exitCode = 1
		if _, _, _, code, _ := throwableInfo(err); code > 0 {
			exitCode = code
		}
	}

	if a.autoExit {
		if exitCode > 255 {
			exitCode = 255
		}
		os.Exit(exitCode)
	}

	return exitCode, nil
}

// runGuarded runs configureIO and doRun, turning Throwable panics (the
// exceptions PHP throws from deep inside output formatting or input access)
// into errors.
func (a *Application) runGuarded(in Input, out Output) (code int, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = recoverThrowable(r)
		}
	}()

	if c, ok := a.self.(AppConfigurer); ok {
		c.ConfigureIO(in, out)
	} else {
		a.ConfigureIO(in, out)
	}

	// $this->doRun() at line 171
	doRun := a.DoRun
	d, overrides := a.self.(AppDoRunner)
	if overrides {
		doRun = d.DoRun
	}

	return a.CallOn(phperr.Frame{Function: a.appClass(overrides) + "->doRun", File: applicationPHP, Line: 171}, a.Impl(), []any{in, out}, func() (int, error) {
		return doRun(in, out)
	})
}

// DoRun runs the current application.
func (a *Application) DoRun(in Input, out Output) (int, error) {
	if in.HasParameterOption([]string{"--version", "-V"}, true) {
		out.Writeln(a.longVersion())

		return 0, nil
	}

	// Makes ArgvInput::getFirstArgument() able to distinguish an option from an argument.
	// Errors must be ignored, full binding/validation happens later when the command is known.
	_ = in.Bind(a.Definition())

	name := a.commandName(in)
	if in.HasParameterOption([]string{"--help", "-h"}, true) {
		if name == "" || name == "0" { // !$name
			name = "help"
			argIn, err := NewArrayInput([]Param{P("command_name", a.defaultCommand)}, nil)
			if err != nil {
				return 0, err
			}
			in = argIn
		} else {
			a.wantHelps = true
		}
	}

	if name == "" || name == "0" { // !$name
		name = a.defaultCommand
		definition := a.Definition()
		args := slices.Clone(definition.Arguments())
		cmdArg, err := definition.Argument("command")
		if err != nil {
			return 0, err
		}
		replacement, err := NewInputArgument("command", ArgumentOptional, cmdArg.Description(), name)
		if err != nil {
			return 0, err
		}
		args[definition.argIndex["command"]] = replacement
		if err := definition.SetArguments(args...); err != nil {
			return 0, err
		}
	}

	a.runningCommand = nil
	// the command name MUST be the first element of the input
	command, err := a.Find(name)
	if err != nil {
		phperr.Call(err, `Symfony\Component\Console\Application->find`, applicationPHP, 259)

		var cnf *Error
		if !errors.As(err, &cnf) || cnf.Kind != KindCommandNotFound || len(cnf.Alternatives) != 1 || !in.IsInteractive() {
			return 0, err
		}

		alternative := cnf.Alternatives[0]

		style := NewSymfonyStyle(in, out)
		out.Writeln("")
		formattedBlock := FormatBlock([]string{`Command "` + name + `" is not defined.`}, "error", true)
		out.Writeln(formattedBlock)
		ok, qerr := style.Confirm(`Do you want to run "`+alternative+`" instead? `, false)
		if qerr != nil {
			return 0, qerr
		}
		if !ok {
			return 1, nil
		}

		command, err = a.Find(alternative)
		if err != nil {
			return 0, phperr.Call(err, `Symfony\Component\Console\Application->find`, applicationPHP, 293)
		}
	}

	a.runningCommand = command
	run := a.DoRunCommand
	if d, ok := a.self.(AppDoRunCommander); ok {
		run = d.DoRunCommand
	}
	exitCode, err := a.CallOn(phperr.Frame{Function: `Symfony\Component\Console\Application->doRunCommand`, File: applicationPHP, Line: 301}, a.Impl(), []any{command, in, out}, func() (int, error) {
		return run(command, in, out)
	})
	if err != nil {
		// PHP leaves runningCommand set when the command throws, so the
		// rendered exception is followed by the command synopsis.
		return exitCode, err
	}
	a.runningCommand = nil

	return exitCode, nil
}

// Reset implements ResetInterface (a no-op).
func (*Application) Reset() {}

// SetHelperSet sets the helper set.
func (a *Application) SetHelperSet(set *HelperSet) { a.helperSet = set }

// HelperSet returns the helper set, creating the default one.
func (a *Application) HelperSet() *HelperSet {
	if a.helperSet == nil {
		if d, ok := a.self.(AppDefaultHelperSetter); ok {
			a.helperSet = d.DefaultHelperSet()
		} else {
			a.helperSet = a.DefaultHelperSet()
		}
	}

	return a.helperSet
}

// SetDefinition sets the input definition.
func (a *Application) SetDefinition(definition *InputDefinition) { a.definition = definition }

// Definition returns the input definition, creating the default one.
func (a *Application) Definition() *InputDefinition {
	if a.definition == nil {
		if d, ok := a.self.(AppDefaultDefinitioner); ok {
			a.definition = d.DefaultInputDefinition()
		} else {
			a.definition = a.DefaultInputDefinition()
		}
	}

	if a.singleCommand {
		_ = a.definition.SetArguments()
	}

	return a.definition
}

// Complete adds suggestions for the application level (command names and
// global options).
func (a *Application) Complete(in *CompletionInput, suggestions *CompletionSuggestions) {
	if in.CompletionType() == CompletionTypeArgumentValue && in.CompletionName() == "command" {
		if a.init() != nil {
			return
		}
		var names []string
		for _, name := range a.allNames() {
			cmd := a.commands[name]
			// skip hidden commands and aliased commands as they already get added below
			if cmd.Base().IsHidden() || cmd.Base().Name() != name {
				continue
			}
			names = append(names, cmd.Base().Name())
			names = append(names, cmd.Base().Aliases()...)
		}
		filtered := names[:0]
		for _, n := range names {
			if n != "" && n != "0" {
				filtered = append(filtered, n)
			}
		}
		suggestions.SuggestStrings(filtered...)

		return
	}

	if in.CompletionType() == CompletionTypeOptionName {
		suggestions.SuggestOptions(a.Definition().Options()...)
	}
}

// Help returns the help message.
func (a *Application) Help() string { return a.longVersion() }

func (a *Application) help() string {
	if h, ok := a.self.(AppHelper); ok {
		return h.Help()
	}

	return a.Help()
}

// AreExceptionsCaught reports whether Run renders errors.
func (a *Application) AreExceptionsCaught() bool { return a.catchErrors }

// SetCatchExceptions sets whether Run renders errors.
func (a *Application) SetCatchExceptions(catch bool) { a.catchErrors = catch }

// IsAutoExitEnabled reports whether Run exits the process.
func (a *Application) IsAutoExitEnabled() bool { return a.autoExit }

// SetAutoExit sets whether Run exits the process.
func (a *Application) SetAutoExit(autoExit bool) { a.autoExit = autoExit }

// Name returns the application name.
func (a *Application) Name() string { return a.name }

// SetName sets the application name.
func (a *Application) SetName(name string) { a.name = name }

// Version returns the application version.
func (a *Application) Version() string { return a.version }

// SetVersion sets the application version.
func (a *Application) SetVersion(version string) { a.version = version }

// LongVersion returns the long version of the application.
func (a *Application) LongVersion() string {
	if a.Name() != "UNKNOWN" {
		if a.Version() != "UNKNOWN" {
			return a.Name() + " <info>" + a.Version() + "</info>"
		}

		return a.Name()
	}

	return "Console Tool"
}

func (a *Application) longVersion() string {
	if l, ok := a.self.(AppLongVersioner); ok {
		return l.LongVersion()
	}

	return a.LongVersion()
}

// Register creates and adds a command.
func (a *Application) Register(name string) (Commander, error) {
	return a.Add(NewCommand(name))
}

// AddCommands adds commands.
func (a *Application) AddCommands(commands ...Commander) error {
	for _, c := range commands {
		if _, err := a.Add(c); err != nil {
			return err
		}
	}

	return nil
}

// Add adds a command, replacing one with the same name. Disabled commands
// are not added (nil is returned).
func (a *Application) Add(command Commander) (Commander, error) {
	if err := a.init(); err != nil {
		return nil, err
	}

	base := command.Base()
	if base == nil {
		// A type embedding a nil *Command: the parent constructor was never
		// called.
		return nil, newError(KindLogic, "Command.php", 420, `Command class "%s" is not correctly initialized. You probably forgot to call the parent constructor.`, commandClass(command))
	}
	if base.self == nil {
		base.self = command
	}
	base.SetApplication(a)

	if !commandEnabled(command) {
		base.SetApplication(nil)

		return nil, nil //nolint:nilnil // PHP returns null for disabled commands.
	}

	if base.definition == nil {
		return nil, newError(KindLogic, "Command.php", 420, `Command class "%s" is not correctly initialized. You probably forgot to call the parent constructor.`, commandClass(command))
	}

	if !base.hasName || base.Name() == "" || base.Name() == "0" {
		return nil, newError(KindLogic, "Application.php", 541, `The command defined in "%s" cannot have an empty name.`, commandClass(command))
	}

	a.setCommand(base.Name(), command)

	for _, alias := range base.Aliases() {
		a.setCommand(alias, command)
	}

	return command, nil
}

// ClassNamer is optionally implemented by commands (and other objects whose
// class PHP reports with get_debug_type()) to name the PHP class they port,
// e.g. `Composer\Command\InstallCommand`. Implement it on the concrete
// type, never on a type meant to be embedded.
type ClassNamer interface {
	ClassName() string
}

// commandClass is get_debug_type($command): the ClassNamer name, the
// Symfony class for a bare *Command, else the Go type name without its
// package qualifier.
func commandClass(c Commander) string {
	if n, ok := c.(ClassNamer); ok {
		return n.ClassName()
	}
	if _, ok := c.(*Command); ok {
		return `Symfony\Component\Console\Command\Command`
	}
	name := typeString(c)
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		name = name[i+1:]
	}

	return name
}

func (a *Application) setCommand(name string, c Commander) {
	if _, ok := a.commands[name]; !ok {
		a.commandOrder = append(a.commandOrder, name)
	}
	a.commands[name] = c
}

func (a *Application) allNames() []string { return a.commandOrder }

// Get returns a registered command by name or alias.
func (a *Application) Get(name string) (Commander, error) {
	if err := a.init(); err != nil {
		return nil, err
	}

	command, ok := a.commands[name]
	if !ok {
		return nil, newError(KindCommandNotFound, "Application.php", 565, `The command "%s" does not exist.`, name)
	}

	if a.wantHelps {
		a.wantHelps = false

		helpCommand, err := a.Get("help")
		if err != nil {
			return nil, err
		}
		if h, ok := helpCommand.(HelpSetter); ok {
			h.SetCommand(command)
		}

		return helpCommand, nil
	}

	return command, nil
}

// Has reports whether a command exists.
func (a *Application) Has(name string) bool {
	if a.init() != nil {
		return false
	}
	_, ok := a.commands[name]

	return ok
}

// Namespaces returns all unique namespaces of the visible commands, except
// the global one.
func (a *Application) Namespaces() []string {
	if a.init() != nil {
		return nil
	}
	var namespaces []string
	seen := map[string]bool{}
	add := func(list []string) {
		for _, ns := range list {
			if ns == "" || ns == "0" || seen[ns] {
				continue
			}
			seen[ns] = true
			namespaces = append(namespaces, ns)
		}
	}
	for _, name := range a.allNames() {
		cmd := a.commands[name].Base()
		if cmd.IsHidden() {
			continue
		}

		add(extractAllNamespaces(cmd.Name()))

		for _, alias := range cmd.Aliases() {
			add(extractAllNamespaces(alias))
		}
	}

	return namespaces
}

// abbreviation is the expression find() and findNamespace() build from a
// name: '{^'.implode('[^:]*:', array_map('preg_quote', explode(':', $name))).'[^:]*}'.
// It is matched without regexps: the quoted parts contain no ":", so each
// "[^:]*:" can only stop at the next colon. Like PCRE without /u, case
// folding is ASCII only, and any bytes are accepted.
type abbreviation []string

func newAbbreviation(name string) abbreviation { return strings.Split(name, ":") }

// match reports whether s matches; anchored adds the trailing "$" (which,
// since "[^:]*" also consumes a final newline, means no further colon).
func (a abbreviation) match(s string, caseInsensitive, anchored bool) bool {
	pos := 0
	for i, p := range a {
		if i > 0 {
			j := strings.IndexByte(s[pos:], ':')
			if j < 0 {
				return false
			}
			pos += j + 1
		}
		if len(s)-pos < len(p) {
			return false
		}
		if seg := s[pos : pos+len(p)]; seg != p && (!caseInsensitive || !asciiEqualFold(seg, p)) {
			return false
		}
		pos += len(p)
	}

	return !anchored || strings.IndexByte(s[pos:], ':') < 0
}

// grep is preg_grep() of the expression over list.
func (a abbreviation) grep(list []string, caseInsensitive, anchored bool) []string {
	var out []string
	for _, s := range list {
		if a.match(s, caseInsensitive, anchored) {
			out = append(out, s)
		}
	}

	return out
}

func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range len(a) {
		x, y := a[i], b[i]
		if x >= 'A' && x <= 'Z' {
			x += 'a' - 'A'
		}
		if y >= 'A' && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}

	return true
}

// FindNamespace finds a registered namespace by name or abbreviation.
func (a *Application) FindNamespace(namespace string) (string, error) {
	allNamespaces := a.Namespaces()
	namespaces := newAbbreviation(namespace).grep(allNamespaces, false, false)

	if len(namespaces) == 0 {
		message := `There are no commands defined in the "` + namespace + `" namespace.`

		alternatives := a.findAlternatives(namespace, allNamespaces)
		if len(alternatives) > 0 {
			if len(alternatives) == 1 {
				message += "\n\nDid you mean this?\n    "
			} else {
				message += "\n\nDid you mean one of these?\n    "
			}

			message += strings.Join(alternatives, "\n    ")
		}

		e := newError(KindNamespaceNotFound, "Application.php", 650, "%s", message)
		e.Alternatives = alternatives

		return "", e
	}

	exact := slices.Contains(namespaces, namespace)
	if len(namespaces) > 1 && !exact {
		e := newError(KindNamespaceNotFound, "Application.php", 655, "The namespace \"%s\" is ambiguous.\nDid you mean one of these?\n%s.", namespace, abbreviationSuggestions(namespaces))
		e.Alternatives = namespaces

		return "", e
	}

	if exact {
		return namespace, nil
	}

	return namespaces[0], nil
}

// Find finds a command by name, alias or abbreviation.
func (a *Application) Find(name string) (Commander, error) {
	if err := a.init(); err != nil {
		return nil, err
	}

	for _, n := range slices.Clone(a.allNames()) {
		command := a.commands[n]
		for _, alias := range command.Base().Aliases() {
			if !a.Has(alias) {
				a.setCommand(alias, command)
			}
		}
	}

	if a.Has(name) {
		return a.Get(name)
	}

	allCommands := slices.Clone(a.allNames())
	abbrev := newAbbreviation(name)
	commands := abbrev.grep(allCommands, false, false)

	if len(commands) == 0 {
		commands = abbrev.grep(allCommands, true, false)
	}

	// if no commands matched or we just matched namespaces
	if len(commands) == 0 || len(abbrev.grep(commands, true, true)) < 1 {
		if pos := strings.LastIndexByte(name, ':'); pos >= 0 {
			// check if a namespace exists and contains commands
			if _, err := a.FindNamespace(name[:pos]); err != nil {
				return nil, err
			}
		}

		message := `Command "` + name + `" is not defined.`

		alternatives := a.findAlternatives(name, allCommands)
		if len(alternatives) > 0 {
			// remove hidden commands
			visible := alternatives[:0]
			for _, alt := range alternatives {
				if c, err := a.Get(alt); err == nil && !c.Base().IsHidden() {
					visible = append(visible, alt)
				}
			}
			alternatives = visible

			if len(alternatives) == 1 {
				message += "\n\nDid you mean this?\n    "
			} else {
				message += "\n\nDid you mean one of these?\n    "
			}
			message += strings.Join(alternatives, "\n    ")
		}

		e := newError(KindCommandNotFound, "Application.php", 720, "%s", message)
		e.Alternatives = alternatives

		return nil, e
	}

	// filter out aliases for commands which are already on the list
	if len(commands) > 1 {
		filtered := make([]string, 0, len(commands))
		seen := map[string]bool{}
		for _, nameOrAlias := range commands {
			commandName := a.commands[nameOrAlias].Base().Name()
			if (commandName == nameOrAlias || !slices.Contains(commands, commandName)) && !seen[nameOrAlias] {
				seen[nameOrAlias] = true
				filtered = append(filtered, nameOrAlias)
			}
		}
		commands = filtered
	}

	if len(commands) > 1 {
		usableWidth := a.terminal.Width() - 10
		maxLen := 0
		for _, abbrev := range commands {
			maxLen = max(Width(abbrev), maxLen)
		}
		var abbrevs []string
		visible := commands[:0:0]
		for _, cmd := range commands {
			c := a.commands[cmd].Base()
			if c.IsHidden() {
				continue
			}
			visible = append(visible, cmd)

			abbrev := strPadRight(cmd, maxLen) + " " + c.Description()
			if Width(abbrev) > usableWidth {
				abbrev = Substr(abbrev, 0, usableWidth-3, false) + "..."
			}
			abbrevs = append(abbrevs, abbrev)
		}
		commands = visible

		if len(commands) > 1 {
			suggestions := abbreviationSuggestions(abbrevs)

			e := newError(KindCommandNotFound, "Application.php", 761, "Command \"%s\" is ambiguous.\nDid you mean one of these?\n%s.", name, suggestions)
			e.Alternatives = commands

			return nil, e
		}
	}

	if len(commands) == 0 {
		// Every candidate was hidden: reset($commands) is false, which
		// get() receives as "".
		return nil, newError(KindCommandNotFound, "Application.php", 565, `The command "%s" does not exist.`, "")
	}

	command, err := a.Get(commands[0])
	if err != nil {
		return nil, err
	}

	if command.Base().IsHidden() {
		return nil, newError(KindCommandNotFound, "Application.php", 768, `The command "%s" does not exist.`, name)
	}

	return command, nil
}

// All returns the commands (in the given namespace if not empty), keyed by
// name or alias, in registration order.
func (a *Application) All(namespace string) []NamedCommand {
	if a.init() != nil {
		return nil
	}

	out := make([]NamedCommand, 0, len(a.commandOrder))
	for _, name := range a.commandOrder {
		if namespace == "" || namespace == ExtractNamespace(name, strings.Count(namespace, ":")+1) {
			out = append(out, NamedCommand{name, a.commands[name]})
		}
	}

	return out
}

// NamedCommand is one entry of Application::all().
type NamedCommand struct {
	Name    string
	Command Commander
}

// Abbreviations returns the possible abbreviations of names.
func Abbreviations(names []string) map[string][]string {
	abbrevs := map[string][]string{}
	for _, name := range names {
		for l := len(name); l > 0; l-- {
			abbrev := name[:l]
			abbrevs[abbrev] = append(abbrevs[abbrev], name)
		}
	}

	return abbrevs
}

// RenderThrowable renders an error like PHP renders an uncaught exception.
func (a *Application) RenderThrowable(err error, out Output) {
	out.Write("", true, VerbosityQuiet)

	if r, ok := a.self.(AppRenderer); ok {
		r.DoRenderThrowable(err, out)
	} else {
		a.DoRenderThrowable(err, out)
	}

	if a.runningCommand != nil {
		synopsis := phpSprintf(a.runningCommand.Base().Synopsis(false), a.Name())
		out.Write("<info>"+Escape(synopsis)+"</info>", true, VerbosityQuiet)
		out.Write("", true, VerbosityQuiet)
	}
}

// throwableInfo extracts the PHP exception details of err itself (not of
// errors it wraps, as PHP only looks at the object it holds).
func throwableInfo(err error) (class, file string, line, code int, prev error) {
	if t, ok := err.(Throwable); ok { //nolint:errorlint // PHP inspects the exception object itself, not what it wraps.
		return t.ThrowableClass(), t.ThrowableFile(), t.ThrowableLine(), t.ThrowableCode(), t.ThrowablePrevious()
	}
	if site, ok := phperr.SiteOf(err); ok {
		return "Exception", phperr.AbsPath(site.File), site.Line, 0, phperr.PreviousOf(err)
	}

	return "Exception", "", 0, 0, nil
}

// throwableTrace is $e->getTrace(): the frames of a Tracer (a PHP
// exception of the plugin runtime), or those maestro recorded for the error
// as it went up the ports of the PHP calls (phperr.Call).
func throwableTrace(err error) []TraceFrame {
	if tr, ok := err.(Tracer); ok { // getTrace() of the object itself.
		return tr.ThrowableTrace()
	}
	frames := phperr.TraceOf(err)
	trace := make([]TraceFrame, len(frames))
	for i, f := range frames {
		trace[i] = TraceFrame{Function: f.Function, File: phperr.AbsPath(f.File), Line: f.Line}
	}

	return trace
}

// isConsoleExceptionValue is "$e instanceof ExceptionInterface", for the
// console's own errors and for exceptions that know their PHP classes (the
// plugin runtime's).
func isConsoleExceptionValue(err error) bool {
	if i, ok := err.(interface{ InstanceOf(class string) bool }); ok {
		return i.InstanceOf(`Symfony\Component\Console\Exception\ExceptionInterface`)
	}
	e, ok := err.(*Error) //nolint:errorlint // instanceof applies to the object itself.

	return ok && e.Is(ErrConsole)
}

// DoRenderThrowable renders err and its previous errors.
//
// The PHP exception details come from the Throwable interface: class
// (get_debug_type), code, file and line of the throw site, and the previous
// exception. Errors that do not implement it render as a plain "Exception"
// with code 0 and file/line "n/a"; their wrapped errors are not followed
// (PHP only follows getPrevious()). In verbose mode the "Exception trace:"
// block lists the throw site ("  at FILE:LINE") followed by the frames of
// the trace (throwableTrace). maestro's errors name Composer's files by
// their absolute paths there, as PHP does (phperr.AbsPath).
func (a *Application) DoRenderThrowable(err error, out Output) {
	for e := err; e != nil; {
		class, file, line, code, prev := throwableInfo(e)
		message := php.Trim(e.Error())
		verbose := out.Verbosity() >= VerbosityVerbose

		var title string
		var length int
		if message == "" || verbose {
			if code != 0 {
				title = "  [" + class + " (" + strconv.Itoa(code) + ")]  "
			} else {
				title = "  [" + class + "]  "
			}
			length = Width(title)
		}

		width := a.terminal.Width()
		if width != 0 {
			width--
		} else {
			width = math.MaxInt // PHP_INT_MAX
		}
		type lineInfo struct {
			text string
			len  int
		}
		var lines []lineInfo
		if message != "" {
			for _, l := range splitCRLF(message) {
				for _, part := range splitStringByWidth(l, width-4) {
					// pre-format lines to get the right string length
					lineLength := Width(part) + 4
					lines = append(lines, lineInfo{part, lineLength})

					length = max(lineLength, length)
				}
			}
		}

		messages := make([]string, 0, len(lines)+5)
		if !isConsoleExceptionValue(e) || verbose {
			fileName := "n/a"
			if b := php.Basename(file, ""); b != "" && b != "0" {
				fileName = b
			}
			lineStr := "n/a"
			if line != 0 {
				lineStr = strconv.Itoa(line)
			}
			messages = append(messages, "<comment>"+Escape("In "+fileName+" line "+lineStr+":")+"</comment>")
		}
		emptyLine := "<error>" + strings.Repeat(" ", length) + "</error>"
		messages = append(messages, emptyLine)
		if message == "" || verbose {
			messages = append(messages, "<error>"+title+strings.Repeat(" ", max(0, length-Width(title)))+"</error>")
		}
		for _, l := range lines {
			messages = append(messages, "<error>  "+Escape(l.text)+"  "+strings.Repeat(" ", max(0, length-l.len))+"</error>")
		}
		messages = append(messages, emptyLine, "")

		WriteMessages(out, messages, true, VerbosityQuiet)

		if verbose {
			out.Write("<comment>Exception trace:</comment>", true, VerbosityQuiet)

			// exception related properties
			lineStr := "n/a"
			if line != 0 {
				lineStr = strconv.Itoa(line)
			}
			// The first frame is the throw site, with an empty class and
			// function: sprintf(' %s%s at ...', '', '').
			fileStr := file
			if fileStr == "" {
				fileStr = "n/a"
			}
			out.Write("  at <info>"+fileStr+":"+lineStr+"</info>", true, VerbosityQuiet)

			for _, f := range throwableTrace(e) {
				fn := ""
				if f.Function != "" {
					fn = f.Type + f.Function + "()"
				}
				ln := "n/a"
				if f.Line != 0 {
					ln = strconv.Itoa(f.Line)
				}
				fl := f.File
				if fl == "" {
					fl = "n/a"
				}
				out.Write(" "+f.Class+fn+" at <info>"+fl+":"+ln+"</info>", true, VerbosityQuiet)
			}

			out.Write("", true, VerbosityQuiet)
		}

		e = prev
	}
}

// splitCRLF is preg_split('/\r?\n/', $s).
func splitCRLF(s string) []string {
	parts := strings.Split(s, "\n")
	for i, p := range parts[:len(parts)-1] {
		parts[i] = strings.TrimSuffix(p, "\r")
	}

	return parts
}

// ConfigureIO configures input and output from the global options.
func (*Application) ConfigureIO(in Input, out Output) {
	if in.HasParameterOption([]string{"--ansi"}, true) {
		out.SetDecorated(true)
	} else if in.HasParameterOption([]string{"--no-ansi"}, true) {
		out.SetDecorated(false)
	}

	if in.HasParameterOption([]string{"--no-interaction", "-n"}, true) {
		in.SetInteractive(false)
	}

	shellVerbosity := phpIntval(os.Getenv("SHELL_VERBOSITY"))
	switch shellVerbosity {
	case -1:
		out.SetVerbosity(VerbosityQuiet)
	case 1:
		out.SetVerbosity(VerbosityVerbose)
	case 2:
		out.SetVerbosity(VerbosityVeryVerbose)
	case 3:
		out.SetVerbosity(VerbosityDebug)
	default:
		shellVerbosity = 0
	}

	if in.HasParameterOption([]string{"--quiet", "-q"}, true) {
		out.SetVerbosity(VerbosityQuiet)
		shellVerbosity = -1
	} else {
		verbose := in.ParameterOption([]string{"--verbose"}, false, true)
		switch {
		case in.HasParameterOption([]string{"-vvv"}, true) || in.HasParameterOption([]string{"--verbose=3"}, true) || phpIdentical(verbose, 3):
			out.SetVerbosity(VerbosityDebug)
			shellVerbosity = 3
		case in.HasParameterOption([]string{"-vv"}, true) || in.HasParameterOption([]string{"--verbose=2"}, true) || phpIdentical(verbose, 2):
			out.SetVerbosity(VerbosityVeryVerbose)
			shellVerbosity = 2
		case in.HasParameterOption([]string{"-v"}, true) || in.HasParameterOption([]string{"--verbose=1"}, true) ||
			in.HasParameterOption([]string{"--verbose"}, true) || phpTruthy(verbose):
			out.SetVerbosity(VerbosityVerbose)
			shellVerbosity = 1
		}
	}

	if shellVerbosity == -1 {
		in.SetInteractive(false)
	}

	_ = os.Setenv("SHELL_VERBOSITY", strconv.Itoa(shellVerbosity))
}

// DoRunCommand runs a command.
func (a *Application) DoRunCommand(command Commander, in Input, out Output) (int, error) {
	return a.CallOn(phperr.Frame{Function: methodClass(command, "run") + "->run", File: applicationPHP, Line: 1040}, command, []any{in, out}, func() (int, error) {
		return command.Run(in, out)
	})
}

// RunningCommand returns the command being run, if any.
func (a *Application) RunningCommand() Commander { return a.runningCommand }

func (a *Application) commandName(in Input) string {
	if a.singleCommand {
		return a.defaultCommand
	}

	return in.FirstArgument()
}

// DefaultInputDefinition returns the default input definition.
func (a *Application) DefaultInputDefinition() *InputDefinition {
	return MustDefinition(
		MustArgument("command", ArgumentRequired, "The command to execute", nil),
		MustOption("--help", "-h", OptionValueNone, "Display help for the given command. When no command is given display help for the <info>"+a.defaultCommand+"</info> command", nil),
		MustOption("--quiet", "-q", OptionValueNone, "Do not output any message", nil),
		MustOption("--verbose", "-v|vv|vvv", OptionValueNone, "Increase the verbosity of messages: 1 for normal output, 2 for more verbose output and 3 for debug", nil),
		MustOption("--version", "-V", OptionValueNone, "Display this application version", nil),
		MustOption("--ansi", "", OptionValueNegatable, "Force (or disable --no-ansi) ANSI output", nil),
		MustOption("--no-interaction", "-n", OptionValueNone, "Do not ask any interactive question", nil),
	)
}

// DefaultCommands returns the commands that are always available.
func (*Application) DefaultCommands() []Commander {
	return []Commander{NewHelpCommand(), NewListCommand(), NewCompleteCommand(), NewDumpCompletionCommand()}
}

// DefaultHelperSet returns the default helper set.
func (*Application) DefaultHelperSet() *HelperSet {
	return NewHelperSet(&FormatterHelper{}, NewQuestionHelper())
}

func abbreviationSuggestions(abbrevs []string) string {
	return "    " + strings.Join(abbrevs, "\n    ")
}

// ExtractNamespace returns the namespace part of a command name, limited to
// limit parts when limit > 0 (PHP's null limit is 0 here).
func ExtractNamespace(name string, limit int) string {
	parts := strings.Split(name, ":")
	parts = parts[:len(parts)-1]
	if limit > 0 && limit < len(parts) {
		parts = parts[:limit]
	}

	return strings.Join(parts, ":")
}

// findAlternatives finds alternatives of name among collection.
func (*Application) findAlternatives(name string, collection []string) []string {
	const threshold = 1000
	alternatives := map[string]int{}
	var order []string
	set := func(k string, v int) {
		if _, ok := alternatives[k]; !ok {
			order = append(order, k)
		}
		alternatives[k] = v
	}

	collectionParts := make([][]string, len(collection))
	for i, item := range collection {
		collectionParts[i] = strings.Split(item, ":")
	}

	for i, subname := range strings.Split(name, ":") {
		for ci, parts := range collectionParts {
			collectionName := collection[ci]
			cur, exists := alternatives[collectionName]
			if i >= len(parts) {
				if exists {
					set(collectionName, cur+threshold)
				}

				continue
			}

			lev := php.Levenshtein(subname, parts[i])
			if float64(lev) <= float64(len(subname))/3 || (subname != "" && strings.Contains(parts[i], subname)) {
				if exists {
					set(collectionName, cur+lev)
				} else {
					set(collectionName, lev)
				}
			} else if exists {
				set(collectionName, cur+threshold)
			}
		}
	}

	for _, item := range collection {
		lev := php.Levenshtein(name, item)
		if float64(lev) <= float64(len(name))/3 || strings.Contains(item, name) {
			if cur, ok := alternatives[item]; ok {
				set(item, cur-lev)
			} else {
				set(item, lev)
			}
		}
	}

	keys := make([]string, 0, len(order))
	for _, k := range order {
		if alternatives[k] < 2*threshold {
			keys = append(keys, k)
		}
	}
	// ksort($alternatives, SORT_NATURAL | SORT_FLAG_CASE)
	slices.SortStableFunc(keys, func(x, y string) int {
		return php.Strnatcasecmp(x, y)
	})

	return keys
}

// SetDefaultCommand sets the default command name.
func (a *Application) SetDefaultCommand(commandName string, isSingleCommand bool) error {
	a.defaultCommand = strings.Split(strings.TrimLeft(commandName, "|"), "|")[0]

	if isSingleCommand {
		// Ensure the command exist
		if _, err := a.Find(commandName); err != nil {
			return err
		}

		a.singleCommand = true
	}

	return nil
}

// IsSingleCommand reports whether the application runs a single command.
func (a *Application) IsSingleCommand() bool { return a.singleCommand }

// splitStringByWidth ports Application::splitStringByWidth().
func splitStringByWidth(s string, width int) []string {
	// str_split is not suitable for multi-byte characters, we should use preg_split to get char array properly.
	// additionally, array_slice() is not enough as some character has doubled width.
	// we need a function to split string not by character count but by string width
	if !utf8.ValidString(s) {
		// str_split($string, $width)
		if width < 1 {
			panic(errors.New("str_split(): Argument #2 ($length) must be greater than 0"))
		}
		var out []string
		for len(s) > width {
			out = append(out, s[:width])
			s = s[width:]
		}

		return append(out, s)
	}

	var lines []string
	var line strings.Builder
	lineWidth := 0
	// add tests whether a character of width w fits on the current line,
	// otherwise it starts a new one.
	add := func(r rune, w int) {
		// test if $char could be appended to current line
		if lineWidth+w <= width {
			if r >= 0 {
				line.WriteRune(r)
			}
			lineWidth += w

			return
		}
		// if not, push current line to array and make new line
		lines = append(lines, strPadRight(line.String(), width))
		line.Reset()
		if r >= 0 {
			line.WriteRune(r)
		}
		lineWidth = w
	}
	// PHP walks chunks of up to 10000 characters (preg_match('/.{1,10000}/u'))
	// and splits each with preg_split('//u'), which yields an empty string
	// before and after the characters; those count as zero-width characters
	// and matter when not even one character fits.
	n := 0
	for _, r := range s {
		if n%10000 == 0 {
			if n > 0 {
				add(-1, 0)
			}
			add(-1, 0)
		}
		add(r, mbCharWidth(r))
		n++
	}
	if n > 0 {
		add(-1, 0)
	}

	if len(lines) > 0 {
		return append(lines, strPadRight(line.String(), width))
	}

	return append(lines, line.String())
}

// mbCharWidth is mb_strwidth() for one character: 2 for East Asian wide
// and fullwidth characters, 1 otherwise.
func mbCharWidth(r rune) int {
	if inRuneTable(wcwidthWide[:], r) {
		return 2
	}

	return 1
}

// extractAllNamespaces returns all namespaces of a command name.
func extractAllNamespaces(name string) []string {
	// -1 as third argument is needed to skip the command short name when exploding
	parts := strings.Split(name, ":")
	parts = parts[:len(parts)-1]
	namespaces := make([]string, 0, len(parts))

	for _, part := range parts {
		if len(namespaces) > 0 {
			namespaces = append(namespaces, namespaces[len(namespaces)-1]+":"+part)
		} else {
			namespaces = append(namespaces, part)
		}
	}

	return namespaces
}

func (a *Application) init() error {
	if a.initialized {
		return nil
	}
	a.initialized = true

	var defaults []Commander
	if d, ok := a.self.(AppDefaultCommander); ok {
		defaults = d.DefaultCommands()
	} else {
		defaults = a.DefaultCommands()
	}
	for _, command := range defaults {
		if _, err := a.Add(command); err != nil {
			return err
		}
	}

	return nil
}
