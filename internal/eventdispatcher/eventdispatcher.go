// Ports src/Composer/EventDispatcher/EventDispatcher.php.

package eventdispatcher

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/script"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// Process is the part of Composer\Util\ProcessExecutor the dispatcher
// runs scripts with. *util.ProcessExecutor and processmock.Mock implement
// it.
type Process interface {
	Execute(command util.Command, output *string, cwd string) (int, error)
	ExecuteTty(command util.Command, cwd string) (int, error)
	GetErrorOutput() string
}

// EventDispatcher is Composer\EventDispatcher\EventDispatcher. Like
// Composer's, it runs on a single call stack: it is not safe for
// concurrent use.
type EventDispatcher struct {
	composer PartialComposer
	io       io.IO
	process  Process

	runtime              ScriptRuntime
	php                  PHP
	ensureComposerBinary func() error

	listeners         map[string]map[int][]Listener
	runScripts        bool
	eventStack        []string
	skipScripts       []string
	previousHash      *string
	previousListeners map[string]bool
	// pendingLoader is the loader makeAutoloader built that the runtime
	// has not installed yet: PHP only starts for a listener that needs it.
	pendingLoader *LoaderContents

	// listenersFor replaces getListeners in tests (PHPUnit's onlyMethods).
	listenersFor func(ev Event) []Listener
}

// New is new EventDispatcher($composer, $io, $process); a nil process is
// new ProcessExecutor($io).
func New(composer PartialComposer, ioi io.IO, process Process) *EventDispatcher {
	if process == nil {
		process = http.NewProcessExecutor(ioi)
	}

	d := &EventDispatcher{
		composer:          composer,
		io:                ioi,
		process:           process,
		php:               NewPlatformPHP(),
		listeners:         map[string]map[int][]Listener{},
		runScripts:        true,
		previousListeners: map[string]bool{},
	}

	skip, _ := util.GetEnv("COMPOSER_SKIP_SCRIPTS")
	for s := range strings.SplitSeq(skip, ",") {
		if s = php.Trim(s); s != "" {
			d.skipScripts = append(d.skipScripts, s)
		}
	}

	return d
}

// SetScriptRuntime sets the runtime PHP listeners run in.
func (d *EventDispatcher) SetScriptRuntime(rt ScriptRuntime) { d.runtime = rt }

// SetPHP sets the PHP Composer is considered to run on (NewPlatformPHP()
// otherwise).
func (d *EventDispatcher) SetPHP(p PHP) { d.php = p }

// SetEnsureComposerBinary sets the hook run before a script process
// starts. COMPOSER_BINARY holds the path of maestro's PHP launcher from
// startup on, as bin/composer sets it; the hook makes sure the launcher
// file exists there (docs/PLUGINS.md D13).
func (d *EventDispatcher) SetEnsureComposerBinary(fn func() error) { d.ensureComposerBinary = fn }

// SetRunScripts is setRunScripts(): whether script listeners (the root
// package's scripts) run. Other listeners always run.
func (d *EventDispatcher) SetRunScripts(runScripts bool) *EventDispatcher {
	d.runScripts = runScripts

	return d
}

// RunScripts is the $runScripts property.
func (d *EventDispatcher) RunScripts() bool { return d.runScripts }

// Dispatch is dispatch($eventName, $event): a nil event is new
// Event($eventName). It returns the highest return code of the listeners
// (for PHP callables false is 1, anything else 0).
func (d *EventDispatcher) Dispatch(eventName string, event Event) (int, error) {
	if event == nil {
		event = NewEvent(eventName, nil, nil)
	}

	return d.doDispatch(event)
}

// fullComposer is the `assert($this->composer instanceof Composer)` of the
// typed dispatch methods.
func (d *EventDispatcher) fullComposer() (Composer, error) {
	c, ok := d.composer.(Composer)
	if !ok {
		return nil, &Error{Class: "LogicException", Message: "This should only be reached with a fully loaded Composer"}
	}

	return c, nil
}

// DispatchScript is dispatchScript(): dispatch a Script\Event.
// additionalArgs are the arguments passed by the user, flags (nil for [])
// data passed not as arguments.
func (d *EventDispatcher) DispatchScript(eventName string, devMode bool, additionalArgs []string, flags *php.Array) (int, error) {
	c, err := d.fullComposer()
	if err != nil {
		return 0, err
	}

	ret, err := d.doDispatch(NewScriptEvent(eventName, c, d.io, devMode, additionalArgs, flags))

	return ret, err
}

// DispatchPackageEvent is dispatchPackageEvent().
func (d *EventDispatcher) DispatchPackageEvent(eventName string, devMode bool, localRepo pkg.Repository, operations []Operation, operation Operation) (int, error) {
	c, err := d.fullComposer()
	if err != nil {
		return 0, err
	}

	ret, err := d.doDispatch(NewPackageEvent(eventName, c, d.io, devMode, localRepo, operations, operation))

	return ret, err
}

// DispatchInstallerEvent is dispatchInstallerEvent(); executeOperations
// is false in --dry-run.
func (d *EventDispatcher) DispatchInstallerEvent(eventName string, devMode, executeOperations bool, transaction Transaction) (int, error) {
	c, err := d.fullComposer()
	if err != nil {
		return 0, err
	}

	ret, err := d.doDispatch(NewInstallerEvent(eventName, c, d.io, devMode, executeOperations, transaction))

	return ret, err
}

// dispatchState is the per-dispatch bracket of PHP calls.
type dispatchState struct {
	depth int
	began bool
}

// doDispatch triggers the listeners of an event.
func (d *EventDispatcher) doDispatch(event Event) (returnMax int, err error) {
	if v, _ := util.GetEnv("COMPOSER_DEBUG_EVENTS"); php.ToBool(v) {
		var details string
		switch e := event.(type) {
		case *PackageEvent:
			details = e.Operation().String()
		case *CommandEvent:
			details = e.CommandName()
		case *PreCommandRunEvent:
			details = e.Command()
		}
		if php.ToBool(details) {
			details = " (" + details + ")"
		} else {
			details = ""
		}
		d.io.WriteError("Dispatching <info>"+event.Name()+"</info>"+details+" event", true, io.Normal)
	}

	listeners := d.getListeners(event)

	if err := d.pushEvent(event); err != nil {
		return 0, err
	}

	st := &dispatchState{depth: len(d.eventStack)}
	defer func() {
		d.popEvent()
		if st.began {
			if endErr := d.runtime.DispatchEnd(st.depth); err == nil {
				err = endErr
			}
		}
	}()

	for _, callable := range listeners {
		ret, skip, err := d.runListener(event, callable, st)
		if err != nil {
			return 0, err
		}
		if skip {
			continue
		}

		returnMax = max(returnMax, ret)

		if event.IsPropagationStopped() {
			break
		}
	}

	return returnMax, nil
}

var noAdditionalArgsPattern = php.MustCompile(`{ ?@no_additional_args}`)

// runListener runs one listener of doDispatch's loop. skip is PHP's
// `continue`, which bypasses the return code and propagation check.
func (d *EventDispatcher) runListener(event Event, callable Listener, st *dispatchState) (ret int, skip bool, err error) {
	if err := d.ensureBinDirIsInPath(); err != nil {
		return 0, false, err
	}

	additionalArgs := event.Arguments()
	if s, ok := callable.(Script); ok && strings.Contains(string(s), "@no_additional_args") {
		replaced, _, err := noAdditionalArgsPattern.Replace(string(s), "", -1)
		if err != nil {
			return 0, false, err
		}
		callable = Script(replaced)
		additionalArgs = nil
	}

	formattedEventNameWithArgs := event.Name()
	if len(additionalArgs) > 0 {
		formattedEventNameWithArgs += " (" + strings.Join(additionalArgs, ", ") + ")"
	}

	switch l := callable.(type) {
	case PHPCallable:
		return d.callPHPListener(event, l, formattedEventNameWithArgs, st)
	case scriptValue:
		return d.callScriptValue(event, l.v, formattedEventNameWithArgs, st)
	case GoFunc:
		if err := d.makeAutoloader(event, "closure"); err != nil {
			return 0, false, err
		}

		return 0, false, l(event)
	case Script:
		s := string(l)
		switch {
		case isComposerScript(s):
			return d.runComposerScript(event, s, formattedEventNameWithArgs, additionalArgs)
		case isPhpScript(s):
			return d.runPhpScript(event, s, st)
		case isCommandClass(s):
			return d.runCommandClass(event, s, additionalArgs, st)
		}

		return d.runShellScript(event, s, additionalArgs)
	}

	return 0, false, nil
}

// callPHPListener runs a PHP callable listener.
func (d *EventDispatcher) callPHPListener(event Event, l PHPCallable, formatted string, st *dispatchState) (int, bool, error) {
	if err := d.makeAutoloader(event, l.key()); err != nil {
		return 0, false, err
	}

	rt, err := d.enterRuntime(st, l.Class+"::"+l.Method)
	if err != nil {
		return 0, false, err
	}

	// `$callable($event)`
	status, returnedFalse, err := rt.CallListener(l, event, func() {
		if l.isArray() {
			d.io.WriteError("> "+formatted+": "+l.Class+"->"+l.Method, true, io.Verbose)
		}
	})
	if status == StatusNotCallable && err == nil {
		return 0, false, runtimeError("Subscriber " + l.Class + "::" + l.Method + " for event " + event.Name() + " is not callable, make sure the function is defined and public")
	}
	if err != nil {
		return 0, false, err
	}

	return boolToReturn(returnedFalse), false, nil
}

// callScriptValue runs a composer.json listener that is not a string as
// doDispatch runs a non-string $callable: makeAutoloader() reads it as an
// array callable, and is_callable() refuses all but ['Class', 'method'],
// which is called as `$callable($event)`. A scalar fails where the
// not-callable exception's message reads $callable[0], a warning
// Composer's ErrorHandler throws.
func (d *EventDispatcher) callScriptValue(event Event, v any, formatted string, st *dispatchState) (int, bool, error) {
	arr, isArray := v.(*php.Array)
	callableKey := "unknown"
	if _, full := d.composer.(Composer); full && isArray {
		key, err := arrayCallableKey(arr)
		if err != nil {
			return 0, false, err
		}
		callableKey = key
	}
	if err := d.makeAutoloader(event, callableKey); err != nil {
		return 0, false, err
	}

	className, methodName, ok := staticCallable(arr)
	if !ok {
		return 0, false, notCallableError(event, v)
	}
	rt, err := d.enterRuntime(st, className+"::"+methodName)
	if err != nil {
		return 0, false, err
	}
	status, returnedFalse, err := rt.CallPHPScript(className, methodName, event, func() {
		d.io.WriteError("> "+formatted+": "+className+"->"+methodName, true, io.Verbose)
	})
	if (status == StatusNotAutoloadable || status == StatusNotCallable) && err == nil {
		return 0, false, notCallableError(event, v)
	}
	if err != nil {
		return 0, false, err
	}

	return boolToReturn(returnedFalse), false, nil
}

// arrayCallableKey is makeAutoloader's $callableKey of an array callable:
// `$callable[0] . '::' . $callable[1]`, or get_class($callable[0]) when
// that is not a string.
func arrayCallableKey(arr *php.Array) (string, error) {
	first, ok := arr.GetKey(php.IntKey(0))
	if !ok {
		return "", &util.ErrorException{Message: "Undefined array key 0"}
	}
	class, isString := first.(string)
	if !isString {
		return "", &php.EngineError{Class: "TypeError", Message: "get_class(): Argument #1 ($object) must be of type object, " + php.ZvalValueName(first) + " given"}
	}
	method, err := arrayElementString(arr, 1)
	if err != nil {
		return "", err
	}

	return class + "::" + method, nil
}

// staticCallable reports whether arr has the shape is_callable() accepts
// for a static method, ['Class', 'method'], and returns its parts.
func staticCallable(arr *php.Array) (className, methodName string, ok bool) {
	if arr == nil || arr.Len() != 2 {
		return "", "", false
	}
	first, _ := arr.GetKey(php.IntKey(0))
	second, _ := arr.GetKey(php.IntKey(1))
	className, ok1 := first.(string)
	methodName, ok2 := second.(string)

	return className, methodName, ok1 && ok2
}

// notCallableError is doDispatch's exception for a listener is_callable()
// refuses: 'Subscriber '.$callable[0].'::'.$callable[1].' ... is not
// callable', or the warning reading those offsets raises first.
func notCallableError(event Event, v any) error {
	arr, ok := v.(*php.Array)
	if !ok {
		return &util.ErrorException{Message: "Trying to access array offset on " + php.ZvalValueName(v)}
	}
	className, err := arrayElementString(arr, 0)
	if err != nil {
		return err
	}
	methodName, err := arrayElementString(arr, 1)
	if err != nil {
		return err
	}

	return runtimeError("Subscriber " + className + "::" + methodName + " for event " + event.Name() + " is not callable, make sure the function is defined and public")
}

// arrayElementString is $arr[$i] in a string concatenation, with the
// warnings Composer's ErrorHandler throws for a missing key or an array.
func arrayElementString(arr *php.Array, i int64) (string, error) {
	v, ok := arr.GetKey(php.IntKey(i))
	if !ok {
		return "", &util.ErrorException{Message: "Undefined array key " + strconv.FormatInt(i, 10)}
	}
	if _, isArray := v.(*php.Array); isArray {
		return "", &util.ErrorException{Message: "Array to string conversion"}
	}

	return php.ToString(v), nil
}

// runComposerScript runs an @script reference or an @composer command.
func (d *EventDispatcher) runComposerScript(event Event, callable, formatted string, additionalArgs []string) (int, bool, error) {
	d.io.WriteError("> "+formatted+": "+callable, true, io.Verbose)

	parts := strings.Split(callable[1:], " ")
	scriptName := parts[0]
	scriptParts := parts[1:]

	var args []string
	if index := slices.Index(scriptParts, "@additional_args"); index >= 0 {
		// array_splice($script, $index, 0, $additionalArgs) with $index the
		// key of "@additional_args" in the 1-based $script: the arguments
		// are inserted after it, and $args gets the (empty) removed slice.
		scriptParts = slices.Insert(slices.Clone(scriptParts), index+1, additionalArgs...)
	} else {
		args = append(slices.Clone(scriptParts), additionalArgs...)
	}

	flags := event.Flags().Clone()
	if v, ok := flags.Get("script-alias-input"); ok && v != nil {
		escaped := make([]string, len(scriptParts))
		for i, arg := range scriptParts {
			escaped[i] = util.Escape(arg)
		}
		flags.Set("script-alias-input", strings.Join(escaped, " ")+" "+php.ToString(v))
	}

	if strings.HasPrefix(callable, "@composer ") {
		phpCmd, err := d.getPhpExecCommand()
		if err != nil {
			return 0, false, err
		}
		binary, _ := util.GetEnv("COMPOSER_BINARY")
		exec := phpCmd + " " + util.Escape(binary) + " " + strings.Join(args, " ")

		return 0, false, d.executeScript(event, callable, exec)
	}

	if len(d.getListeners(NewEvent(scriptName, nil, nil))) == 0 {
		d.io.WriteError("<warning>You made a reference to a non-existent script "+callable+"</warning>", true, io.Quiet)
	}

	ctx, ok := composerContext(event)
	if !ok {
		return 0, false, &Error{Class: "Error", Message: "Call to undefined method " + event.Class() + "::getComposer()"}
	}

	scriptEvent := NewScriptEvent(scriptName, ctx.Composer, ctx.IO, ctx.DevMode, args, flags)
	scriptEvent.SetOriginatingEvent(event)

	ret, err := d.Dispatch(scriptName, scriptEvent)
	if err != nil {
		if _, ok := errors.AsType[*ScriptExecutionError](err); ok {
			d.io.WriteError("<error>Script "+callable+" was called via "+event.Name()+"</error>", true, io.Quiet)
		}

		return 0, false, err
	}

	return ret, false, nil
}

// runPhpScript runs a Class::method script.
func (d *EventDispatcher) runPhpScript(event Event, callable string, st *dispatchState) (int, bool, error) {
	className, methodName, _ := strings.Cut(callable, "::")

	if err := d.makeAutoloader(event, callable); err != nil {
		return 0, false, err
	}

	if native := nativeScript(className, methodName); native != nil {
		d.echoPhpScript(event, className, methodName)
		native()

		return 0, false, nil
	}

	rt, err := d.enterRuntime(st, callable)
	if err != nil {
		return 0, false, err
	}

	// `$className::$methodName($event)` in executeEventPhpScript(), called
	// by doDispatch()
	status, returnedFalse, err := rt.CallPHPScript(className, methodName, event, func() {
		d.echoPhpScript(event, className, methodName)
	})

	switch {
	case status == StatusNotAutoloadable && err == nil:
		d.io.WriteError("<warning>Class "+className+" is not autoloadable, can not call "+event.Name()+" script</warning>", true, io.Quiet)

		return 0, true, nil
	case status == StatusNotCallable && err == nil:
		d.io.WriteError("<warning>Method "+callable+" is not callable, can not call "+event.Name()+" script</warning>", true, io.Quiet)

		return 0, true, nil
	case err != nil:
		if status == StatusOK {
			d.writeTerminated(callable, event, err)
		}

		return 0, false, err
	}

	return boolToReturn(returnedFalse), false, nil
}

// echoPhpScript is the output of executeEventPhpScript.
func (d *EventDispatcher) echoPhpScript(event Event, className, methodName string) {
	if d.io.IsVerbose() {
		d.io.WriteError("> "+event.Name()+": "+className+"::"+methodName, true, io.Normal)
	} else if eventNeedsToOutput(event) {
		d.io.WriteError("> "+className+"::"+methodName, true, io.Normal)
	}
}

// writeTerminated is the `catch (\Exception $e)` message of PHP scripts
// and command classes.
func (d *EventDispatcher) writeTerminated(callable string, event Event, err error) {
	if isPHPError(err) {
		return
	}

	d.io.WriteError("<error>Script "+callable+" handling the "+event.Name()+" event terminated with an exception</error>", true, io.Quiet)
}

// runCommandClass runs a Symfony command class script.
func (d *EventDispatcher) runCommandClass(event Event, className string, additionalArgs []string, st *dispatchState) (int, bool, error) {
	if err := d.makeAutoloader(event, className+"::run"); err != nil {
		return 0, false, err
	}

	rt, err := d.enterRuntime(st, className)
	if err != nil {
		return 0, false, err
	}

	input := escapeArgs(additionalArgs)
	if v, ok := event.Flags().Get("script-alias-input"); ok && v != nil {
		input = php.ToString(v)
	}

	// `$app->run(new StringInput(...), $output)`
	status, code, err := rt.RunCommandClass(className, event, input, func() bool {
		if script.HasConstant(strings.ReplaceAll(php.Strtoupper(event.Name()), "-", "_")) {
			d.io.WriteError("<warning>You cannot bind "+event.Name()+" to a Command class, use a non-reserved name</warning>", true, io.Quiet)

			return false
		}

		return true
	})

	switch {
	case status == StatusNotAutoloadable && err == nil:
		d.io.WriteError("<warning>Class "+className+" is not autoloadable, can not call "+event.Name()+" script</warning>", true, io.Quiet)

		return 0, true, nil
	case status == StatusNotCommand && err == nil:
		d.io.WriteError("<warning>Class "+className+` does not extend Symfony\Component\Console\Command\Command, can not call `+event.Name()+" script</warning>", true, io.Quiet)

		return 0, true, nil
	case status == StatusSkipped && err == nil:
		return 0, true, nil
	case err != nil:
		if status == StatusOK {
			d.writeTerminated(className, event, err)
		}

		// `$app->run(new StringInput(...), $output)`
		return 0, false, err
	}

	return code, false, nil
}

var (
	phpExecPathPattern = php.MustCompile(`{^[^'"\s/\\]+}`)
	firstWordPattern   = php.MustCompile(`{^\S+}`)
	windowsExtPattern  = php.MustCompile(`{\.(exe|bat|cmd|com)$}i`)
)

// runShellScript runs a shell command, @php or @putenv line.
func (d *EventDispatcher) runShellScript(event Event, callable string, additionalArgs []string) (int, bool, error) {
	args := escapeArgs(additionalArgs)

	var exec string
	switch {
	case strings.HasPrefix(callable, "@putenv "):
		// @putenv does not receive arguments
		exec = callable
	case strings.Contains(callable, "@additional_args"):
		exec = strings.ReplaceAll(callable, "@additional_args", args)
	case args == "":
		exec = callable
	default:
		exec = callable + " " + args
	}

	if d.io.IsVerbose() {
		d.io.WriteError("> "+event.Name()+": "+exec, true, io.Normal)
	} else if eventNeedsToOutput(event) {
		d.io.WriteError("> "+exec, true, io.Normal)
	}

	exec, err := d.replaceLocalBinary(callable, exec)
	if err != nil {
		return 0, false, err
	}

	if rest, ok := strings.CutPrefix(exec, "@putenv "); ok {
		if name, value, found := strings.Cut(rest, "="); found {
			util.PutEnv(name, value)
		} else {
			util.ClearEnv(rest)
		}

		return 0, true, nil
	}

	if pathAndArgs, ok := strings.CutPrefix(exec, "@php "); ok {
		if pathAndArgs, err = d.resolvePhpScriptPath(pathAndArgs); err != nil {
			return 0, false, err
		}
		phpCmd, err := d.getPhpExecCommand()
		if err != nil {
			return 0, false, err
		}
		exec = phpCmd + " " + pathAndArgs
	} else {
		if phpPath, ok := d.php.Binary(); ok {
			util.PutEnv("PHP_BINARY", phpPath)
		}

		if util.IsWindows() {
			if exec, err = backslashFirstWord(exec); err != nil {
				return 0, false, err
			}
		}
	}

	// if composer is being executed, make sure it runs the expected composer from current path
	// resolution, even if bin-dir contains composer too because the project requires composer/composer
	// see https://github.com/composer/composer/issues/8748
	if rest, ok := strings.CutPrefix(exec, "composer "); ok {
		phpCmd, err := d.getPhpExecCommand()
		if err != nil {
			return 0, false, err
		}
		binary, _ := util.GetEnv("COMPOSER_BINARY")
		exec = phpCmd + " " + util.Escape(binary) + " " + rest
	}

	return 0, false, d.executeScript(event, callable, exec)
}

// replaceLocalBinary runs a script naming one of the root package's
// binaries through that binary's interpreter.
func (d *EventDispatcher) replaceLocalBinary(callable, exec string) (string, error) {
	binaries := d.composer.Package().Binaries()
	if binaries == nil || binaries.Len() == 0 {
		return exec, nil
	}

	quoted := php.PregQuote(callable, "")
	match, err := php.Compile(`{\b` + quoted + `$}`)
	if err != nil {
		return "", err
	}

	for _, v := range binaries.All() {
		localExec := php.ToString(v)
		ok, err := match.IsMatch(localExec)
		if err != nil {
			return "", err
		}
		if !ok {
			continue
		}

		caller, err := util.DetermineBinaryCaller(localExec)
		if err != nil {
			return "", err
		}

		prefix, err := php.Compile(`{^` + quoted + `}`)
		if err != nil {
			return "", err
		}
		exec, _, err = prefix.Replace(exec, caller+" "+localExec, -1)

		return exec, err
	}

	return exec, nil
}

// resolvePhpScriptPath resolves the script of an "@php path args" line:
// a bare name that is not a file in the working directory is looked up in
// the PATH, so `@php foo` runs a foo binary.
func (d *EventDispatcher) resolvePhpScriptPath(pathAndArgs string) (string, error) {
	var err error
	if util.IsWindows() {
		if pathAndArgs, err = backslashFirstWord(pathAndArgs); err != nil {
			return "", err
		}
	}

	m, err := phpExecPathPattern.MatchStrictGroups(pathAndArgs)
	if err != nil || m == nil {
		return pathAndArgs, err
	}

	name := m.Get(0)
	if fileExists(name) {
		return pathAndArgs, nil
	}

	pathToExec, ok := util.NewExecutableFinder().Find(name)
	if !ok || pathToExec == "" {
		return pathAndArgs, nil
	}

	if util.IsWindows() {
		execWithoutExt, _, err := windowsExtPattern.Replace(pathToExec, "", -1)
		if err != nil {
			return "", err
		}
		// prefer non-extension file if it exists when executing with PHP
		if fileExists(execWithoutExt) {
			pathToExec = execWithoutExt
		}
	}

	return pathToExec + pathAndArgs[len(name):], nil
}

// backslashFirstWord turns the slashes of the first word into
// backslashes (Windows paths).
func backslashFirstWord(s string) (string, error) {
	out, _, err := firstWordPattern.ReplaceCallback(s, func(m *php.Match) string {
		return strings.ReplaceAll(m.Get(0), "/", `\`)
	}, -1)

	return out, err
}

// executeScript runs exec for the script callable, turning a non-zero exit
// code into a ScriptExecutionError.
func (d *EventDispatcher) executeScript(event Event, callable, exec string) error {
	if d.ensureComposerBinary != nil {
		if err := d.ensureComposerBinary(); err != nil {
			return err
		}
	}

	exitCode, err := d.executeTty(exec)
	if err != nil {
		return err
	}
	if exitCode != 0 {
		d.io.WriteError("<error>Script "+callable+" handling the "+event.Name()+" event returned with error code "+strconv.Itoa(exitCode)+"</error>", true, io.Quiet)

		return &ScriptExecutionError{Message: "Error Output: " + d.process.GetErrorOutput(), Code: exitCode}
	}

	return nil
}

// executeTty runs exec with a TTY when the IO is interactive.
func (d *EventDispatcher) executeTty(exec string) (int, error) {
	if d.io.IsInteractive() {
		return d.process.ExecuteTty(util.ShellCmd(exec), "")
	}

	return d.process.Execute(util.ShellCmd(exec), nil, "")
}

// getPhpExecCommand is the command line running the PHP Composer runs on,
// with the ini settings scripts inherit from it.
func (d *EventDispatcher) getPhpExecCommand() (string, error) {
	phpPath, ok := d.php.Binary()
	if !ok {
		return "", runtimeError("Failed to locate PHP binary to execute ")
	}

	var cmd strings.Builder
	cmd.WriteString(util.Escape(phpPath))
	for _, name := range []string{"allow_url_fopen", "disable_functions", "memory_limit"} {
		value, err := d.php.IniGet(name)
		if err != nil {
			return "", err
		}
		cmd.WriteString(" -d " + name + "=" + util.Escape(value))
	}

	return cmd.String(), nil
}

// eventNeedsToOutput reports whether the "> command" line is written in
// non-verbose mode.
func eventNeedsToOutput(event Event) bool {
	// do not output the command being run when using `composer exec` as it is fairly obvious the user is running it
	if event.Name() == "__exec_command" {
		return false
	}

	// do not output the command being run when using `composer <script-name>` as it is also fairly obvious the user is running it
	if v, ok := event.Flags().Get("script-alias-input"); ok && v != nil {
		return false
	}

	return true
}

// AddListener is addListener(): a higher priority runs earlier.
func (d *EventDispatcher) AddListener(eventName string, listener Listener, priority int) {
	byPriority := d.listeners[eventName]
	if byPriority == nil {
		byPriority = map[int][]Listener{}
		d.listeners[eventName] = byPriority
	}

	byPriority[priority] = append(byPriority[priority], listener)
}

// RemoveListener is removeListener(): it removes every listener match
// accepts (see MatchCallable, MatchObject, MatchScript).
func (d *EventDispatcher) RemoveListener(match func(Listener) bool) {
	for _, byPriority := range d.listeners {
		for priority, listeners := range byPriority {
			byPriority[priority] = slices.DeleteFunc(listeners, match)
		}
	}
}

// AddSubscriber is addSubscriber() for a Go subscriber.
func (d *EventDispatcher) AddSubscriber(subscriber EventSubscriber) {
	for _, s := range subscriber.SubscribedEvents() {
		d.AddListener(s.Event, s.Listener, s.Priority)
	}
}

// getListeners returns all listeners of an event, by descending priority,
// the root package's scripts last among priority 0.
func (d *EventDispatcher) getListeners(event Event) []Listener {
	return d.listenersOf(event, true)
}

// listenersOf is getListeners; notice prints getScriptListeners' notice
// about scripts COMPOSER_SKIP_SCRIPTS skips.
func (d *EventDispatcher) listenersOf(event Event, notice bool) []Listener {
	if d.listenersFor != nil {
		return d.listenersFor(event)
	}

	var scriptListeners []Listener
	if d.runScripts {
		scriptListeners = d.getScriptListeners(event, notice)
	}

	byPriority := d.listeners[event.Name()]
	if byPriority == nil {
		byPriority = map[int][]Listener{}
		d.listeners[event.Name()] = byPriority
	}
	if _, ok := byPriority[0]; !ok {
		byPriority[0] = nil
	}

	var listeners []Listener
	for _, priority := range slices.Backward(slices.Sorted(maps.Keys(byPriority))) {
		listeners = append(listeners, byPriority[priority]...)
		if priority == 0 {
			listeners = append(listeners, scriptListeners...)
		}
	}

	return listeners
}

// HasEventListeners is hasEventListeners().
func (d *EventDispatcher) HasEventListeners(event Event) bool {
	return len(d.getListeners(event)) > 0
}

// WillDispatchTo reports whether dispatching event now would call a
// listener: HasEventListeners without its output, for code preparing for
// an event it dispatches later (which prints the output then).
func (d *EventDispatcher) WillDispatchTo(event Event) bool {
	return len(d.listenersOf(event, false)) > 0
}

// getScriptListeners returns the root package's scripts for an event.
func (d *EventDispatcher) getScriptListeners(event Event, notice bool) []Listener {
	scripts := d.composer.Package().Scripts()
	if scripts == nil {
		return nil
	}

	v, _ := scripts.Get(event.Name())
	if !php.ToBool(v) {
		return nil
	}

	if slices.Contains(d.skipScripts, event.Name()) {
		if !notice {
			return nil
		}

		d.io.WriteError("Skipped script listeners for <info>"+event.Name()+"</info> because of COMPOSER_SKIP_SCRIPTS", true, io.Verbose)

		return nil
	}

	list, ok := v.(*php.Array)
	if !ok {
		return []Listener{Script(php.ToString(v))}
	}

	listeners := make([]Listener, 0, list.Len())
	for _, s := range list.All() {
		if str, ok := s.(string); ok {
			listeners = append(listeners, Script(str))
		} else {
			listeners = append(listeners, scriptValue{s})
		}
	}

	return listeners
}

// isPhpScript reports whether callable references a class and method.
func isPhpScript(callable string) bool {
	return !strings.Contains(callable, " ") && strings.Contains(callable, "::")
}

// isCommandClass reports whether callable references a command class.
func isCommandClass(callable string) bool {
	return strings.Contains(callable, `\`) && !strings.Contains(callable, " ") && strings.HasSuffix(callable, "Command")
}

// isComposerScript reports whether callable references a script or runs
// composer (@script, @composer).
func isComposerScript(callable string) bool {
	return strings.HasPrefix(callable, "@") && !strings.HasPrefix(callable, "@php ") && !strings.HasPrefix(callable, "@putenv ")
}

// pushEvent pushes an event onto the stack of active events, refusing
// recursion.
func (d *EventDispatcher) pushEvent(event Event) error {
	name := event.Name()
	for _, active := range d.eventStack {
		if php.StringsLooseEqual(active, name) {
			return runtimeError("Circular call to script handler '" + name + "' detected")
		}
	}

	d.eventStack = append(d.eventStack, name)

	return nil
}

// popEvent pops the active event from the stack.
func (d *EventDispatcher) popEvent() {
	d.eventStack = d.eventStack[:len(d.eventStack)-1]
}

// ensureBinDirIsInPath prepends bin-dir to the PATH, so local binaries of
// dependencies are usable in scripts.
func (d *EventDispatcher) ensureBinDirIsInPath() error {
	pathEnv := "PATH"

	// checking if only Path and not PATH is set then we probably need to update the Path env
	if _, ok := util.GetEnv(pathEnv); !ok {
		if _, ok := util.GetEnv("Path"); ok {
			pathEnv = "Path"
		}
	}

	v, err := d.composer.Config().Get("bin-dir", 0)
	if err != nil {
		return err
	}
	binDir := php.ToString(v)
	if !isDir(binDir) {
		return nil
	}

	binDir = util.Realpath(binDir)
	pathValue, _ := util.GetEnv(pathEnv)
	sep := string(os.PathListSeparator)
	pattern, err := php.Compile("{(^|" + sep + ")" + php.PregQuote(binDir, "") + "($|" + sep + ")}")
	if err != nil {
		return err
	}
	found, err := pattern.IsMatch(pathValue)
	if err != nil {
		return err
	}
	if !found {
		util.PutEnv(pathEnv, binDir+sep+pathValue)
	}

	return nil
}

// makeAutoloader gives PHP code run by a listener the project's class
// loader. The loader is built here, as Composer does (with the generator's
// output), and handed to the runtime before its next PHP call.
func (d *EventDispatcher) makeAutoloader(event Event, callableKey string) error {
	c, ok := d.composer.(Composer)
	if !ok {
		return nil
	}

	if d.previousListeners[callableKey] {
		return nil
	}
	d.previousListeners[callableKey] = true

	sa := c.ScriptAutoloader()
	packages, err := sa.CanonicalLocalPackages()
	if err != nil {
		return err
	}

	ids := make([]string, len(packages))
	for i, p := range packages {
		ids[i] = p.Name() + "/" + p.Version()
	}
	hash := strings.Join(ids, ",")
	if ctx, ok := composerContext(event); ok {
		sa.SetDevMode(ctx.DevMode)
		if ctx.DevMode {
			hash += "/dev"
		}
	}
	sum := sha256.Sum256([]byte(hash))
	hash = hex.EncodeToString(sum[:])

	if d.previousHash != nil && *d.previousHash == hash {
		return nil
	}
	d.previousHash = &hash

	loader, err := sa.CreateLoader(packages)
	if err != nil {
		return err
	}
	d.pendingLoader = loader

	return nil
}

// enterRuntime readies the runtime for a PHP call of the current dispatch:
// it opens the dispatch bracket and installs a pending class loader.
func (d *EventDispatcher) enterRuntime(st *dispatchState, callable string) (ScriptRuntime, error) {
	if d.runtime == nil {
		return nil, runtimeError("maestro: script " + callable + " requires PHP but no PHP runtime is available")
	}

	if !st.began {
		if err := d.runtime.DispatchBegin(st.depth); err != nil {
			return nil, err
		}
		st.began = true
	}

	if d.pendingLoader != nil {
		loader := d.pendingLoader
		d.pendingLoader = nil
		if err := d.runtime.InstallAutoloader(loader); err != nil {
			return nil, err
		}
	}

	return d.runtime, nil
}

// nativeScript returns the Go implementation of a Class::method script of
// Composer's own that only changes Composer's state, so no PHP has to
// start for it (docs/PLUGINS.md §5.5). PHP class and method names are
// case-insensitive.
func nativeScript(className, methodName string) func() {
	if php.Strtolower(className) == `composer\config` && php.Strtolower(methodName) == "disableprocesstimeout" {
		return config.DisableProcessTimeout
	}

	return nil
}

func escapeArgs(args []string) string {
	escaped := make([]string, len(args))
	for i, arg := range args {
		escaped[i] = util.Escape(arg)
	}

	return strings.Join(escaped, " ")
}

func boolToReturn(returnedFalse bool) int {
	if returnedFalse {
		return 1
	}

	return 0
}

func fileExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

func isDir(path string) bool {
	fi, err := os.Stat(path)

	return err == nil && fi.IsDir()
}
