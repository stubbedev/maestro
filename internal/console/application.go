// Ports src/Application.php (symfony/console).

package console

import (
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/ui"
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
	// AppErrorPresenter adds what the application knows about an error
	// to the Diagnostic RenderThrowable shows for it (hints, clearer
	// wording), as Composer's Application writes its hints along with
	// the exception Symfony renders.
	AppErrorPresenter interface {
		PresentError(err error, d *ui.Diagnostic)
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
}

// Call runs fn as PHP's call of function (as PHP names it,
// "Class->method").
func (a *Application) Call(function string, fn func() (int, error)) (int, error) {
	return a.CallOn(function, nil, nil, fn)
}

// CallHook is told of the calls of methods on objects (CallOn) as they
// start, and returns what to do as they end (nil for nothing): the plugin
// runtime has the objects on the PHP call stack while plugin code runs
// under them (docs/PLUGINS.md §5.12).
type CallHook func(function string, object any, args []any) (end func())

// SetCallHook sets the hook CallOn tells of its calls.
func (a *Application) SetCallHook(h CallHook) { a.callHook = h }

// CallOn is Call of a method of object with args, which the CallHook is
// told of.
func (a *Application) CallOn(function string, object any, args []any, fn func() (int, error)) (int, error) {
	if a.callHook != nil && object != nil {
		if end := a.callHook(function, object, args); end != nil {
			defer end()
		}
	}

	return fn()
}

// appClass is the class declaring the application's method: the
// subclass's (php.Classer) when it overrides it, Symfony's otherwise.
func (a *Application) appClass(overrides bool) string {
	if n, ok := a.self.(php.Classer); ok && overrides {
		return n.PHPClass()
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
//
// Symfony's run() starts with putenv('LINES=...') and putenv('COLUMNS=...'):
// a bare putenv(), which the processes Composer starts don't inherit
// (Symfony Process passes on getenv() ∩ $_SERVER), and which only makes
// Terminal read back the size it measured. maestro's environment is what
// those processes get, so the size stays out of it: Terminal measures once
// per process, and the plugin shim putenv()s it for PHP code
// (internal/plugin's boot).
func (a *Application) Run(in Input, out Output) (exitCode int, err error) {
	if in == nil {
		argv, _ := NewArgvInput(nil, nil)
		in = argv
	}
	if out == nil {
		out = NewConsoleOutput(VerbosityNormal, nil, nil)
	}

	exitCode, err = a.runGuarded(in, out)
	if err != nil {
		if !a.catchErrors {
			return exitCode, err
		}

		a.RenderThrowable(err, ErrorOutputOf(out))

		// $e->getCode() of the exception itself, 1 unless positive.
		exitCode = 1
		if _, code, _ := throwableInfo(err); code > 0 {
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

	return a.CallOn(a.appClass(overrides)+"->doRun", a.Impl(), []any{in, out}, func() (int, error) {
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
		if !php.Truthy(name) { // !$name
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

	if !php.Truthy(name) { // !$name
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
			return 0, err
		}
	}

	a.runningCommand = command
	run := a.DoRunCommand
	if d, ok := a.self.(AppDoRunCommander); ok {
		run = d.DoRunCommand
	}
	exitCode, err := a.CallOn(`Symfony\Component\Console\Application->doRunCommand`, a.Impl(), []any{command, in, out}, func() (int, error) {
		return run(command, in, out)
	})
	if err != nil {
		// PHP leaves runningCommand set when the command throws, so the
		// rendered error can show the command's usage.
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
			if php.Truthy(n) {
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
		return nil, newError(KindLogic, `Command class "%s" is not correctly initialized. You probably forgot to call the parent constructor.`, command.PHPClass())
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
		return nil, newError(KindLogic, `Command class "%s" is not correctly initialized. You probably forgot to call the parent constructor.`, command.PHPClass())
	}

	if !base.hasName || base.Name() == "" || base.Name() == "0" {
		return nil, newError(KindLogic, `The command defined in "%s" cannot have an empty name.`, command.PHPClass())
	}

	a.setCommand(base.Name(), command)

	for _, alias := range base.Aliases() {
		a.setCommand(alias, command)
	}

	return command, nil
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
		return nil, newError(KindCommandNotFound, `The command "%s" does not exist.`, name)
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
			if !php.Truthy(ns) || seen[ns] {
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

		alternatives := FindAlternatives(namespace, allNamespaces)
		if len(alternatives) > 0 {
			message += ui.DidYouMean(alternatives)
		}

		e := newError(KindNamespaceNotFound, "%s", message)
		e.Alternatives = alternatives

		return "", e
	}

	exact := slices.Contains(namespaces, namespace)
	if len(namespaces) > 1 && !exact {
		e := newError(KindNamespaceNotFound, "The namespace \"%s\" is ambiguous.\nDid you mean one of these?\n%s.", namespace, abbreviationSuggestions(namespaces))
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
		if before, _, ok := strings.CutLast(name, ":"); ok {
			// check if a namespace exists and contains commands
			if _, err := a.FindNamespace(before); err != nil {
				return nil, err
			}
		}

		message := `Command "` + name + `" is not defined.`

		alternatives := FindAlternatives(name, allCommands)
		if len(alternatives) > 0 {
			// remove hidden commands
			visible := alternatives[:0]
			for _, alt := range alternatives {
				if c, err := a.Get(alt); err == nil && !c.Base().IsHidden() {
					visible = append(visible, alt)
				}
			}
			alternatives = visible

			message += ui.DidYouMean(alternatives)
		}

		e := newError(KindCommandNotFound, "%s", message)
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

			e := newError(KindCommandNotFound, "Command \"%s\" is ambiguous.\nDid you mean one of these?\n%s.", name, suggestions)
			e.Alternatives = commands

			return nil, e
		}
	}

	if len(commands) == 0 {
		// Every candidate was hidden: reset($commands) is false, which
		// get() receives as "".
		return nil, newError(KindCommandNotFound, `The command "%s" does not exist.`, "")
	}

	command, err := a.Get(commands[0])
	if err != nil {
		return nil, err
	}

	if command.Base().IsHidden() {
		return nil, newError(KindCommandNotFound, `The command "%s" does not exist.`, name)
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
			in.HasParameterOption([]string{"--verbose"}, true) || inputTruthy(verbose):
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
	return a.CallOn(methodClass(command, "run")+"->run", command, []any{in, out}, func() (int, error) {
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

// FindAlternatives ports Application::findAlternatives: the names of
// collection close to name (a Levenshtein distance of at most a third of
// its length, or containing it), per ":"-separated part and as a whole,
// in natural order.
func FindAlternatives(name string, collection []string) []string {
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
