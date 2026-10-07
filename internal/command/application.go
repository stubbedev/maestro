// Ports src/Composer/Console/Application.php.

package command

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/json/jsonlint"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/ui"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// logo heads the banner of `list` and a bare run: maestro's own where
// Composer prints its logo (docs/PORTING.md deviation 8).
const logo = `                              __
   ____ ___  ____ ____  _____/ /__________
  / __ ` + "`" + `__ \/ __ ` + "`" + `/ _ \/ ___/ __/ ___/ __ \
 / / / / / / /_/ /  __(__  ) /_/ /  / /_/ /
/_/ /_/ /_/\__,_/\___/____/\__/_/   \____/
`

// PluginCommandProvider is implemented by a composer.PluginManager that
// loads plugins (the plugin runtime): the commands of every
// CommandProvider capability, as Application::getPluginCommands collects
// them (including its UnexpectedValueException checks).
type PluginCommandProvider interface {
	PluginCommands(c *composer.Composer, out io.IO) ([]console.Commander, error)
}

// ScriptCommandProvider is implemented by a composer.PluginManager that
// can run PHP (the plugin runtime): the command for a composer.json script
// that names a Symfony Command subclass (`class_exists($dummy) &&
// is_subclass_of($dummy, SymfonyCommand::class)`), ok false when class is
// not one, which makes it a ScriptAliasCommand. It prints the
// SingleCommandApplication warning itself.
type ScriptCommandProvider interface {
	ScriptCommand(c *composer.Composer, out io.IO, script, class string) (cmd console.Commander, ok bool, err error)
}

// ProjectLoaderRegistrar is implemented by a composer.PluginManager that
// can run PHP (the plugin runtime): before looking for script commands
// Composer registers a class loader for the root package's autoload
// (createLoader, which may write warnings, then register()).
type ProjectLoaderRegistrar interface {
	RegisterProjectLoader(c *composer.Composer) error
}

// Application is Composer\Console\Application.
type Application struct {
	*console.Application

	factory  *composer.Factory
	composer *composer.Composer
	io       io.IO

	hasPluginCommands       bool
	disablePluginsByDefault bool
	disableScriptsByDefault bool
	// commandName is $commandName: "" with commandNameFalse for false.
	commandName      string
	commandNameFalse bool

	initialWorkingDirectory string

	// errorHints are the hints hintCommonErrors found for the error the
	// run ends with, until PresentError shows them.
	errorHints []string

	// Exit is PHP's exit($code) where the Application calls it
	// (getComposer); os.Exit by default.
	Exit func(code int)
	// Stdin is the stream doRun checks with Platform::isTty; os.Stdin by
	// default.
	Stdin *os.File
}

// NewApplication ports new Application(): the Composer application, named
// "Composer" with Composer::getVersion(). factory creates the Composer
// instances; nil is a Factory with a new composer.Runtime.
func NewApplication(factory *composer.Factory) *Application {
	return NewNamedApplication(factory, "Composer", "")
}

// NewNamedApplication ports new Application($name, $version); an empty
// version is Composer::getVersion().
func NewNamedApplication(factory *composer.Factory, name, version string) *Application {
	if version == "" {
		version = composer.GetVersion()
	}
	if factory == nil {
		factory = &composer.Factory{}
	}
	if factory.Runtime == nil {
		factory.Runtime = composer.NewRuntime("", nil)
	}
	a := &Application{
		Application: console.NewApplication(name, version),
		factory:     factory,
		io:          io.NewNullIO(),
		Exit:        os.Exit,
		Stdin:       os.Stdin,
	}
	a.SetImpl(a)
	a.SetCallHook(a.pushCallFrame)
	a.initialWorkingDirectory, _ = util.GetCwd(true)

	return a
}

// Factory returns the Factory the Application creates Composer instances
// with (Factory::create in PHP).
func (a *Application) Factory() *composer.Factory { return a.factory }

// Runtime returns the process runtime (Composer's statics).
func (a *Application) Runtime() *composer.Runtime { return a.factory.Runtime }

// Run ports run(): a nil output is Factory::createOutput(). It is
// bin/composer's call (line 113) as far as exception traces go.
func (a *Application) Run(in console.Input, out console.Output) (int, error) {
	return a.RunFrom("bin/composer", 113, in, out)
}

// RunFrom is Run called from file:line of Composer's sources (the frame
// above run() in an exception's trace): OutdatedCommand and GlobalCommand
// run the application again.
func (a *Application) RunFrom(file string, line int, in console.Input, out console.Output) (int, error) {
	// run() and parent::run() on the stack plugin code sees
	// (docs/PLUGINS.md §5.12), with the arguments PHP passes: none from
	// bin/composer ($application->run()), then run()'s $input and $output
	// (null there)
	if rt := a.Runtime(); rt != nil {
		var args, parentArgs []any
		if file == "bin/composer" {
			parentArgs = []any{nil, nil}
		} else {
			args = []any{in, out}
			parentArgs = args
		}
		rt.PushFrame(`Composer\Console\Application->run`, a, args...)
		defer rt.PopFrame()
		rt.PushFrame(`Symfony\Component\Console\Application->run`, a, parentArgs...)
		defer rt.PopFrame()
	}

	if out == nil {
		out = composer.CreateOutput()
	}

	return a.Application.Run(in, out)
}

// PHPClass implements php.Classer.
func (*Application) PHPClass() string { return `Composer\Console\Application` }

// pushCallFrame is the console's CallHook: the methods Composer's PHP
// stack holds while they run, with their objects and arguments, go on the
// frame stack the plugin runtime reports to debug_backtrace()
// (docs/PLUGINS.md §5.12): the Application's doRun($input, $output) (where
// symfony/flex and symfony/thanks find the Application and its input),
// doRunCommand(), the command's run(), initialize(), interact() and
// execute() (flex looks for a GlobalCommand).
func (a *Application) pushCallFrame(function string, object any, args []any) func() {
	rt := a.Runtime()
	if rt == nil {
		return nil
	}
	rt.PushFrame(function, object, args...)

	return rt.PopFrame
}

// DoRun ports doRun. An ExitCoder error ends the run with its code and
// nothing rendered.
func (a *Application) DoRun(in console.Input, out console.Output) (int, error) {
	code, err := a.doRun(in, out)
	if err == nil {
		return code, nil
	}
	if ec, ok := errors.AsType[ExitCoder](err); ok {
		return ec.ExitCode(), nil
	}

	return code, err
}

func (a *Application) doRun(in console.Input, out console.Output) (int, error) {
	a.disablePluginsByDefault = in.HasParameterOption([]string{"--no-plugins"}, false)
	a.disableScriptsByDefault = in.HasParameterOption([]string{"--no-scripts"}, false)

	if v, _ := util.GetEnv("COMPOSER_TESTS_ARE_RUNNING"); v != "1" && (util.EnvTruthy("COMPOSER_NO_INTERACTION") || a.Stdin == nil || !util.IsTty(a.Stdin)) {
		in.SetInteractive(false)
	}

	cio := io.NewConsoleIO(in, out, console.NewHelperSet(console.NewQuestionHelper()))
	a.io = cio

	// Register error handler again to pass it the IO instance
	util.RegisterErrorHandler(cio)

	if in.HasParameterOption([]string{"--no-cache"}, false) {
		cio.WriteError("Disabling cache usage", true, io.Debug)
		util.PutEnv("COMPOSER_CACHE_DIR", util.GetDevNull())
	}

	// switch working dir
	newWorkDir, err := a.newWorkingDir(in)
	if err != nil {
		return 0, err
	}
	var oldWorkingDir string
	if newWorkDir != nil {
		oldWorkingDir, _ = util.GetCwd(true)
		if err := os.Chdir(*newWorkDir); err != nil {
			return 0, &util.ErrorException{Message: "chdir(): " + err.Error()}
		}
		a.initialWorkingDirectory, _ = util.GetCwd(true)
		cwd, _ := util.GetCwd(true)
		if cwd == "" {
			cwd = *newWorkDir
		}
		cio.WriteError("Changed CWD to "+cwd, true, io.Debug)
	}

	// determine command name to be executed without including plugin commands
	commandName, commandNameFalse := "", false
	rawCommandName := a.commandNameBeforeBinding(in)
	if php.Truthy(rawCommandName) {
		cmd, err := a.Find(rawCommandName)
		switch {
		case err == nil:
			commandName = cmd.Base().Name()
		case phperr.InstanceOf(err, console.ClassCommandNotFound):
			// we'll check command validity again later after plugins are loaded
			commandNameFalse = true
		case phperr.InstanceOf(err, ClassInvalidArgument):
		default:
			return 0, err
		}
	}
	a.commandName, a.commandNameFalse = commandName, commandNameFalse
	a.Runtime().SetFetching(fetchingCommands[commandName])

	// prompt user for dir change if no composer.json is present in current dir
	// (in_array(false, [...], true) is false: an unknown command prompts too)
	if newWorkDir == nil && (commandNameFalse || !noComposerJSONCommands[commandName]) {
		if changed, err := a.promptParentDir(in, cio, commandName); err != nil {
			return 0, err
		} else if changed != "" {
			oldWorkingDir = changed
		}
	}

	needsSudoCheck := !util.IsWindows() && !util.EnvTruthy("COMPOSER_ALLOW_SUPERUSER") && !util.IsDocker()
	isNonAllowedRoot := false

	// Clobber sudo credentials if COMPOSER_ALLOW_SUPERUSER is not set before loading plugins
	if needsSudoCheck {
		isNonAllowedRoot = isRunningAsRoot()

		if isNonAllowedRoot {
			if uid := php.ToInt(os.Getenv("SUDO_UID")); uid != 0 {
				// Silently clobber any sudo credentials on the invoking user to avoid privilege escalations later on
				// ref. https://github.com/composer/composer/issues/5119
				silentExec(`sudo -u \#` + strconv.FormatInt(uid, 10) + ` sudo -K > /dev/null 2>&1`)
			}
		}

		// Silently clobber any remaining sudo leases on the current user as well to avoid privilege escalations
		silentExec("sudo -K > /dev/null 2>&1")
	}

	// avoid loading plugins/initializing the Composer instance earlier than necessary if no plugin command is needed
	// if showing the version, we never need plugin commands
	mayNeedPluginCommand := !in.HasParameterOption([]string{"--version", "-V"}, false) &&
		(commandNameFalse ||
			commandName == "" || commandName == "list" || commandName == "help" ||
			(commandName == "_complete" && !isNonAllowedRoot))

	// $rawCommandName !== $commandName: null (no command) never equals a
	// string, false never equals a string.
	mayNeedScriptCommand := mayNeedPluginCommand || commandName == "run-script" || rawCommandName == "" || commandNameFalse || rawCommandName != commandName

	if mayNeedPluginCommand && !a.disablePluginsByDefault && !a.hasPluginCommands {
		// at this point plugins are needed, so if we are running as root and it is not allowed we need to prompt
		// if interactive, and abort otherwise
		if isNonAllowedRoot {
			cio.WriteError("<warning>Do not run Composer as root/super user! See https://getcomposer.org/root for details</warning>", true, io.Normal)

			ok := false
			if cio.IsInteractive() {
				if ok, err = cio.AskConfirmation("<info>Continue as root/super user</info> [<comment>yes</comment>]? ", true); err != nil {
					return 0, err
				}
			}
			if ok {
				// avoid a second prompt later
				isNonAllowedRoot = false
			} else {
				cio.WriteError("<warning>Aborting as no plugin should be loaded if running as super user is not explicitly allowed</warning>", true, io.Normal)

				return 1, nil
			}
		}

		if err := a.addPluginCommands(cio); err != nil {
			return 0, err
		}

		a.hasPluginCommands = true
	}

	if !a.disablePluginsByDefault && isNonAllowedRoot && !cio.IsInteractive() {
		cio.WriteError("<error>Composer plugins have been disabled for safety in this non-interactive session.</error>", true, io.Normal)
		cio.WriteError("<error>Set COMPOSER_ALLOW_SUPERUSER=1 if you want to allow plugins to run as root/super user.</error>", true, io.Normal)
		a.disablePluginsByDefault = true
	}

	// determine command name to be executed incl plugin commands, and check if it's a proxy command
	isProxyCommand := false
	if name := a.commandNameBeforeBinding(in); php.Truthy(name) {
		cmd, err := a.Find(name)
		switch {
		case err == nil:
			commandName = cmd.Base().Name()
			a.commandName, a.commandNameFalse = commandName, false
			if p, ok := cmd.(ProxyCommander); ok && isBaseCommand(cmd) {
				isProxyCommand = p.IsProxyCommand()
			}
		case phperr.InstanceOf(err, ClassInvalidArgument):
		default:
			return 0, err
		}
	}

	if !isProxyCommand {
		if code, done, err := a.preRunChecks(cio, commandName, isNonAllowedRoot, mayNeedScriptCommand); done || err != nil {
			return code, err
		}
	}

	// record the running command for telemetry purposes (sent via the User-Agent in StreamContextFactory)
	// this runs once all commands (incl. plugin and script commands) are registered so it sees the final command
	if rawCommandName != "" {
		if cmd, err := a.Find(rawCommandName); err == nil {
			a.Runtime().SetRunningCommand(telemetryCommandName(cmd), true)
		} else if !phperr.InstanceOf(err, ClassInvalidArgument) {
			return 0, err
		}
	}

	return a.runCommand(in, out, cio, oldWorkingDir)
}

// noComposerJSONCommands are the commands that can function without
// composer.json (no parent directory prompt).
var noComposerJSONCommands = map[string]bool{
	"": true, "list": true, "init": true, "about": true, "help": true, "diagnose": true,
	"self-update": true, "global": true, "create-project": true, "outdated": true,
}

// fetchingCommands are the commands that talk to the repositories on most
// runs (an install from a lock file revalidates Packagist's filter list):
// connections to them are opened while the project loads
// (Runtime.SetFetching).
var fetchingCommands = map[string]bool{
	"install": true, "update": true, "require": true, "remove": true,
	"reinstall": true, "outdated": true, "audit": true,
}

// promptParentDir is the "No composer.json in current directory" lookup;
// it returns the previous working directory when it changed directory.
func (a *Application) promptParentDir(in console.Input, cio *io.ConsoleIO, commandName string) (string, error) {
	composerFile, err := composer.GetComposerFile()
	if err != nil {
		return "", err
	}
	if php.FileExists(composerFile) {
		return "", nil
	}
	// if use-parent-dir is disabled we should not prompt
	useParentDir, err := a.useParentDirConfigValue()
	if err != nil {
		return "", err
	}
	if b, ok := useParentDir.(bool); ok && !b {
		return "", nil
	}
	// config --file ... should not prompt
	if commandName == "config" && (in.HasParameterOption([]string{"--file"}, true) || in.HasParameterOption([]string{"-f"}, true)) {
		return "", nil
	}
	// calling a command's help should not prompt
	if in.HasParameterOption([]string{"--help"}, true) || in.HasParameterOption([]string{"-h"}, true) {
		return "", nil
	}

	cwd, _ := util.GetCwd(true)
	dir := php.Dirname(cwd)
	homeEnv := os.Getenv("HOME")
	if homeEnv == "" {
		homeEnv = os.Getenv("USERPROFILE")
	}
	if homeEnv == "" {
		homeEnv = "/"
	}
	home, homeOK := php.Realpath(homeEnv)

	// abort when we reach the home dir or top of the filesystem
	for php.Dirname(dir) != dir && (!homeOK || dir != home) {
		if php.FileExists(dir + "/" + composerFile) {
			isTrue := useParentDir == true
			if !isTrue && !cio.IsInteractive() {
				cio.WriteError("<info>No composer.json in current directory, to use the one at "+dir+" run interactively or set config.use-parent-dir to true</info>", true, io.Normal)

				return "", nil
			}
			ok := isTrue
			if !ok {
				if ok, err = cio.AskConfirmation("<info>No composer.json in current directory, do you want to use the one at "+dir+"?</info> [<comment>y,n</comment>]? ", true); err != nil {
					return "", err
				}
			}
			if ok {
				if isTrue {
					cio.WriteError("<info>No composer.json in current directory, changing working directory to "+dir+"</info>", true, io.Normal)
				} else {
					cio.WriteError(`<info>Always want to use the parent dir? Use "composer config --global use-parent-dir true" to change the default.</info>`, true, io.Normal)
				}
				old, _ := util.GetCwd(true)
				if err := os.Chdir(dir); err != nil {
					return "", &util.ErrorException{Message: "chdir(): " + err.Error()}
				}

				return old, nil
			}

			return "", nil
		}
		dir = php.Dirname(dir)
	}

	return "", nil
}

// silentExec is Silencer::call('exec', $command).
func silentExec(command string) {
	_ = exec.Command("/bin/sh", "-c", command).Run() //nolint:gosec // Composer's fixed sudo commands
}

// addPluginCommands adds getPluginCommands() to the application.
func (a *Application) addPluginCommands(cio *io.ConsoleIO) error {
	commands, err := a.pluginCommands()
	if err != nil {
		if pe, ok := errors.AsType[*jsonlint.ParsingError](err); ok {
			file := ""
			if composerFile, ferr := composer.GetComposerFile(); ferr == nil {
				file, _ = php.Realpath(composerFile)
			}
			line := pe.Details.Line
			console.NewGithubActionError(func(m string) { cio.Write(m, true, io.Normal) }).Emit(pe.Message, file, line)

			return err
		}
		if isNoSSL(err) {
			// suppress these as they are not relevant at this point
			return nil
		}

		return err
	}
	for _, command := range commands {
		name := command.Base().Name()
		if a.Has(name) {
			cio.WriteError("<warning>Plugin command "+name+" ("+command.PHPClass()+") would override a Composer command and has been skipped</warning>", true, io.Normal)
		} else if _, err := a.Add(command); err != nil {
			return err
		}
	}

	return nil
}

// isNoSSL is `$e instanceof NoSslException`.
func isNoSSL(err error) bool {
	return phperr.InstanceOf(err, `Composer\Exception\NoSslException`)
}

// pluginCommands ports getPluginCommands(), whose frame is on the stack
// plugin code sees (docs/PLUGINS.md §5.12).
func (a *Application) pluginCommands() ([]console.Commander, error) {
	if rt := a.Runtime(); rt != nil {
		rt.PushFrame(`Composer\Console\Application->getPluginCommands`, a)
		defer rt.PopFrame()
	}

	c, err := a.getComposer(2, false, new(false), nil) // $this->getComposer(false, false)
	if err != nil {
		return nil, err
	}
	if c == nil {
		if c, err = a.factory.CreateGlobal(a.io, a.disablePluginsByDefault, a.disableScriptsByDefault); err != nil {
			return nil, err
		}
	}
	if c == nil {
		return nil, nil
	}
	if p, ok := c.PluginManager().(PluginCommandProvider); ok {
		return p.PluginCommands(c, a.io)
	}

	return nil, nil
}

// preRunChecks is the block doRun runs for commands that are not proxy
// commands: the debug banner, the warnings, the root prompt, the temp
// directory check and the script commands. done reports an early return.
func (a *Application) preRunChecks(cio *io.ConsoleIO, commandName string, isNonAllowedRoot, mayNeedScriptCommand bool) (code int, done bool, err error) {
	phpVersion := ""
	xdebugActive := false
	if snap, _, verr := a.Runtime().ComposerView(); verr == nil {
		phpVersion = snap.Version
		// XdebugHandler::isXdebugActive() in the PHP Composer runs on
		// (after its restart without xdebug, if any)
		xdebugActive = snap.Xdebug.Active
	}
	uname := "Unknown OS"
	if s, r, ok := phpUname(); ok {
		uname = s + " / " + r
	}
	cio.WriteError("Running "+composer.GetVersion()+" ("+composer.ReleaseDate+") with PHP "+phpVersion+" on "+uname, true, io.Debug)

	// The PHP < 7.2.5 warning cannot happen: cmd/maestro refuses such a
	// PHP as bin/composer does.

	if xdebugActive && !util.EnvTruthy("COMPOSER_DISABLE_XDEBUG_WARN") {
		cio.WriteError("<warning>Composer is operating slower than normal because you have Xdebug enabled. See https://getcomposer.org/xdebug</warning>", true, io.Normal)
	}

	// COMPOSER_DEV_WARNING_TIME is only defined in dev builds of Composer's
	// phar; maestro has none.

	if isNonAllowedRoot && commandName != "self-update" && commandName != "selfupdate" && commandName != "_complete" {
		cio.WriteError("<warning>Do not run Composer as root/super user! See https://getcomposer.org/root for details</warning>", true, io.Normal)

		if cio.IsInteractive() {
			ok, err := cio.AskConfirmation("<info>Continue as root/super user</info> [<comment>yes</comment>]? ", true)
			if err != nil {
				return 0, true, err
			}
			if !ok {
				return 1, true, nil
			}
		}
	}

	// Check system temp folder for usability as it can cause weird runtime issues otherwise
	checkTempDir(cio)

	// add non-standard scripts as own commands
	if mayNeedScriptCommand {
		if err := a.addScriptCommands(cio); err != nil {
			return 0, true, err
		}
	}

	return 0, false, nil
}

// applicationPath is __FILE__ inside Composer's phar, which the temp
// directory check writes.
const applicationPath = "phar:///usr/local/bin/composer/src/Composer/Console/Application.php"

// checkTempDir ports the sys_get_temp_dir() usability check.
func checkTempDir(cio *io.ConsoleIO) {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	tmp := php.SysGetTempDir()
	tempfile := tmp + "/temp-" + strconv.Itoa(os.Getpid()) + "-" + hex.EncodeToString(b)
	ok := os.WriteFile(tempfile, []byte(applicationPath), 0o666) == nil
	if ok {
		data, err := os.ReadFile(tempfile)
		ok = err == nil && string(data) == applicationPath
	}
	if ok {
		ok = os.Remove(tempfile) == nil && !php.FileExists(tempfile)
	}
	if !ok {
		cio.WriteError("<error>PHP temp directory ("+tmp+") does not exist or is not writable to Composer. Set sys_temp_dir in your php.ini</error>", true, io.Normal)
	}
}

// scriptEventConstants are the names of the ScriptEvents constants.
var scriptEventConstants = map[string]bool{
	"PRE_INSTALL_CMD": true, "POST_INSTALL_CMD": true, "PRE_UPDATE_CMD": true, "POST_UPDATE_CMD": true,
	"PRE_STATUS_CMD": true, "POST_STATUS_CMD": true, "PRE_AUTOLOAD_DUMP": true, "POST_AUTOLOAD_DUMP": true,
	"POST_ROOT_PACKAGE_INSTALL": true, "POST_CREATE_PROJECT_CMD": true, "PRE_ARCHIVE_CMD": true, "POST_ARCHIVE_CMD": true,
}

// isScriptEvent is defined(ScriptEvents::class.'::'.str_replace('-', '_',
// strtoupper($name))): whether a script name is that of an event.
// strtoupper is ASCII-only, so "post-inſtall-cmd" is no event.
func isScriptEvent(name string) bool {
	return scriptEventConstants[strings.ReplaceAll(php.Strtoupper(name), "-", "_")]
}

// addScriptCommands adds the composer.json scripts that are not events as
// commands.
func (a *Application) addScriptCommands(cio *io.ConsoleIO) error {
	file, err := composer.GetComposerFile()
	if err != nil {
		return err
	}
	if st, serr := os.Stat(file); serr != nil || !st.Mode().IsRegular() || !util.IsReadable(file) {
		return nil
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil //nolint:nilerr // file_get_contents() failing gives json_decode(false): not an array
	}
	decoded, err := php.JSONDecode(string(data), true)
	if err != nil {
		return nil //nolint:nilerr // json_decode's null: not an array
	}
	composerJSON, ok := decoded.(*php.Array)
	if !ok {
		return nil
	}
	scripts, ok := composerJSON.GetArray("scripts")
	if !ok {
		return nil
	}
	descriptions, _ := composerJSON.GetArray("scripts-descriptions")
	scriptAliases, _ := composerJSON.GetArray("scripts-aliases")

	projectLoaderRegistered := false
	for key, value := range scripts.All() {
		if key.IsInt() {
			// strtoupper($script) of a numeric script name, an int key:
			// a TypeError under strict_types, thrown before doRun's try
			return uncaught(&php.EngineError{Class: "TypeError", Message: "strtoupper(): Argument #1 ($string) must be of type string, int given"})
		}
		script := key.String()
		if isScriptEvent(script) {
			continue
		}
		if a.Has(script) {
			cio.WriteError("<warning>A script named "+script+" would override a Composer command and has been skipped</warning>", true, io.Normal)

			continue
		}

		var description any = "Runs the " + script + " script as defined in composer.json"
		if descriptions != nil {
			if d, ok := descriptions.GetKey(key); ok && d != nil {
				description = d
			}
		}
		var aliases any = php.NewArray()
		if scriptAliases != nil {
			if al, ok := scriptAliases.GetKey(key); ok && al != nil {
				aliases = al
			}
		}

		var c *composer.Composer
		if !projectLoaderRegistered {
			projectLoaderRegistered = true
			// Composer registers the project's autoloader here, for
			// command class scripts; only the plugin runtime needs it.
			if c, err = a.getComposer(1, false, nil, nil); err != nil { // $this->getComposer(false)
				return err
			}
			if c != nil {
				if p, ok := c.PluginManager().(ProjectLoaderRegistrar); ok {
					if err := p.RegisterProjectLoader(c); err != nil {
						return err
					}
				}
			}
		}

		var cmd console.Commander
		if class, ok := value.(string); ok {
			if c == nil {
				c = a.composer
			}
			if c != nil {
				if p, ok := c.PluginManager().(ScriptCommandProvider); ok {
					command, isCommand, perr := p.ScriptCommand(c, cio, script, class)
					if perr != nil {
						return perr
					}
					if isCommand {
						cmd = a.adoptScriptCommand(cio, command, script, description)
					}
				}
			}
		}
		if cmd == nil {
			// fallback to usual aliasing behavior
			if scriptAliasConstructor == nil {
				return &util.LogicError{Message: "ScriptAliasCommand is not available"}
			}
			if cmd, err = scriptAliasConstructor(script, description, aliases); err != nil {
				return err
			}
		}

		if _, err := a.Add(cmd); err != nil {
			return err
		}
	}

	return nil
}

// adoptScriptCommand applies the name and description rules to a script
// that is a command class.
func (*Application) adoptScriptCommand(cio *io.ConsoleIO, cmd console.Commander, script string, description any) console.Commander {
	base := cmd.Base()
	// makes sure the command is find()'able by the name defined in composer.json, and the name isn't overridden in its configure()
	if base.Name() != "" && base.Name() != script {
		cio.WriteError("<warning>The script named "+script+" in composer.json has a mismatched name in its class definition. For consistency, either use the same name, or do not define one inside the class.</warning>", true, io.Normal)
		base.SetName(script) // override it with the defined script name
	}
	if d, ok := description.(string); ok && base.Description() == "" {
		base.SetDescription(d)
	}

	return cmd
}

// runCommand is the try block around parent::doRun.
func (a *Application) runCommand(in console.Input, out console.Output, cio *io.ConsoleIO, oldWorkingDir string) (int, error) {
	var startTime time.Time
	profile := in.HasParameterOption([]string{"--profile"}, false)
	if profile {
		startTime = time.Now()
		cio.EnableDebugging(startTime)
	}

	result, err := a.CallOn(`Symfony\Component\Console\Application->doRun`, a, []any{in, out}, func() (int, error) {
		return a.Application.DoRun(in, out)
	})
	if err != nil {
		return a.handleRunError(err, out, cio)
	}

	if in.HasParameterOption([]string{"--version", "-V"}, true) {
		if snap, _, verr := a.Runtime().ComposerView(); verr == nil {
			cio.WriteError("<info>PHP</info> version <comment>"+snap.Version+"</comment> ("+snap.PHPBinary()+")", true, io.Normal)
		}
		cio.WriteError(`Run the "diagnose" command to get more detailed diagnostics output.`, true, io.Normal)
		if a.Runtime().ClientVersion() != "" {
			cio.WriteError(a.maestroVersion(), true, io.Normal)
		}
	}

	// chdir back to $oldWorkingDir if set
	if oldWorkingDir != "" {
		_ = os.Chdir(oldWorkingDir)
	}

	if profile {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		cio.WriteError("<info>Memory usage: "+roundMiB(ms.HeapAlloc)+"MiB (peak: "+roundMiB(ms.Sys)+"MiB), time: "+phpRound2(time.Since(startTime).Seconds())+"s</info>", true, io.Normal)
	}

	return result, nil
}

func roundMiB(b uint64) string {
	return phpRound2(float64(b) / 1024 / 1024)
}

// phpRound2 is (string) round($v, 2).
func phpRound2(v float64) string {
	return php.FloatToString(math.Round(v*100) / 100)
}

// handleRunError is doRun's catch blocks.
func (a *Application) handleRunError(err error, out console.Output, cio *io.ConsoleIO) (int, error) {
	if _, ok := errors.AsType[ExitCoder](err); ok {
		return 0, err
	}
	if se, ok := errors.AsType[*eventdispatcher.ScriptExecutionError](err); ok {
		if a.disablePluginsByDefault && isRunningAsRoot() && !a.io.IsInteractive() {
			cio.WriteError("<error>Plugins have been disabled automatically as you are running as root, this may be the cause of the script failure.</error>", true, io.Quiet)
			cio.WriteError("<error>See also https://getcomposer.org/root</error>", true, io.Quiet)
		}

		return se.Code, nil
	}

	console.NewGithubActionError(func(m string) { cio.Write(m, true, io.Normal) }).Emit(err.Error(), "", 0)

	hints, fatal := a.hintCommonErrors(err, out)
	if fatal != nil {
		return 0, uncaught(fatal)
	}
	a.errorHints = hints

	// override TransportException's code for the purpose of parent::run() using it as process exit code
	// as http error codes are all beyond the 255 range of permitted exit codes
	if phperr.InstanceOf(err, `Composer\Downloader\TransportException`) {
		return 0, withExitCode(err, composer.ErrorTransportException)
	}

	return 0, err
}

// hintCommonErrors ports hintCommonErrors: the hints about the likely
// causes of exception, which RenderThrowable shows with it
// (PresentError). Composer writes them before the exception, as errors;
// their wording is maestro's (#13). fatal is a PHP \Error its
// getComposer() raises, which escapes its catch (\Exception): the run ends
// with that one, uncaught.
func (a *Application) hintCommonErrors(exception error, out console.Output) (hints []string, fatal error) {
	if (phperr.Class(exception) == ClassLogic || phperr.InstanceOf(exception, "Error")) && out.Verbosity() < console.VerbosityVerbose {
		out.SetVerbosity(console.VerbosityVerbose)
	}

	util.SilencerSuppress()
	c, err := a.getComposer(2, false, new(true), nil) // $this->getComposer(false, true)
	util.SilencerRestore()
	if err != nil && phperr.InstanceOf(err, "Error") {
		return nil, err
	}
	if err == nil && c != nil {
		cfg := c.Config()
		const minSpaceFree = 100 * 1024 * 1024
		for _, key := range []string{"home", "vendor-dir", ""} {
			dir := php.SysGetTempDir()
			if key != "" {
				v, gerr := cfg.Get(key, 0)
				if gerr != nil {
					break
				}
				dir = php.ToString(v)
			}
			if df, ok := diskFreeSpace(dir); ok && df < minSpaceFree {
				hints = append(hints, "The disk hosting "+dir+" has less than 100MiB of free space, which may be the cause of this error.")

				break
			}
		}
	}

	_, isTransport := errors.AsType[*util.TransportError](exception)
	message := exception.Error()
	// the text of ProxyManager::getProxyForRequest's exception (proxy.go)
	if isTransport && strings.Contains(message, "Unable to use a proxy") {
		hints = append(hints, "Your proxy seems to be misconfigured, see https://getcomposer.org/doc/faqs/how-to-use-composer-behind-a-proxy.md")
	}

	if te, ok := errors.AsType[*util.TransportError](exception); ok && util.IsWindows() && te.Curl != nil && te.Curl.VerifyResult == util.X509VErrUnableToGetIssuerCertLocally {
		if matches, _ := filepath.Glob(`C:\Program Files\Avast*`); len(matches) != 0 {
			hints = append(hints, "The Avast Firewall may be the cause of this error, see https://getcomposer.org/local-issuer")
		} else {
			hints = append(hints, "A firewall or antivirus may be the cause of this error, see https://getcomposer.org/local-issuer")
		}
	}

	if util.IsWindows() && strings.Contains(message, "The system cannot find the path specified") {
		hints = append(hints, "A stale entry in your cmd.exe AutoRun may be the cause of this error, see https://getcomposer.org/doc/articles/troubleshooting.md#-the-system-cannot-find-the-path-specified-windows-")
	}

	if strings.Contains(message, "fork failed - Cannot allocate memory") {
		hints = append(hints, "This error is caused by a lack of memory or swap, or not having swap configured, see https://getcomposer.org/doc/articles/troubleshooting.md#proc-open-fork-failed-errors")
	}

	if _, ok := errors.AsType[*util.ProcessTimedOutError](exception); ok {
		hints = append(hints, "A process timed out; the limit is the process-timeout setting, see https://getcomposer.org/doc/06-config.md#process-timeout")
	}

	if a.disablePluginsByDefault && isRunningAsRoot() && !a.io.IsInteractive() {
		hints = append(hints, "Plugins have been disabled automatically as you are running as root, which may be the cause of this error, see https://getcomposer.org/root")
	} else if phperr.InstanceOf(exception, console.ClassCommandNotFound) && a.disablePluginsByDefault {
		hints = append(hints, "Plugins have been disabled, which may be why some commands are missing, unless you made a typo.")
	}

	for _, hint := range http.GetExceptionHints(exception) {
		hints = append(hints, exceptionHint(hint))
	}

	if te, ok := errors.AsType[*util.TransportError](exception); ok && a.commandName != "self-update" && (te.IsTimeout() || te.IsResolveFailure()) {
		hints = append(hints, "To run without connecting to the internet, run the command again prefixed with COMPOSER_DISABLE_NETWORK=1 (offline mode).")
	}

	return hints, nil
}

// exceptionHint is one of HttpDownloader::getExceptionHints' lines as a
// hint: its text, without the console tags, about "this error" rather than
// "the following exception".
func exceptionHint(hint string) string {
	hint = console.StripTags(hint)
	if rest, ok := strings.CutPrefix(hint, "The following exception "); ok {
		hint = "This error " + rest
	}

	return hint
}

// PresentError implements console.AppErrorPresenter: the hints
// hintCommonErrors found for the error the run ends with, and the
// presentation of transport errors (transportDiagnostic).
func (a *Application) PresentError(err error, d *ui.Diagnostic) {
	d.Hints = append(d.Hints, a.errorHints...)
	a.errorHints = nil
	transportDiagnostic(err, d)
}

// newWorkingDir ports getNewWorkingDir; nil is null.
func (*Application) newWorkingDir(in console.Input) (*string, error) {
	v := in.ParameterOption([]string{"--working-dir", "-d"}, nil, true)
	if v == nil {
		return nil, nil
	}
	workingDir := php.ToString(v)
	if st, err := os.Stat(workingDir); err != nil || !st.IsDir() {
		return nil, NewError(ClassRuntime, "Invalid working directory specified, "+workingDir+" does not exist.")
	}

	return &workingDir, nil
}

// GetComposer ports getComposer($required, $disablePlugins,
// $disableScripts); nil flags read --no-plugins/--no-scripts. It is
// BaseCommand's call, with its three arguments.
func (a *Application) GetComposer(required bool, disablePlugins, disableScripts *bool) (*composer.Composer, error) {
	return a.getComposer(3, required, disablePlugins, disableScripts)
}

// GetComposerFromPHP is GetComposer called by PHP code, whose stack has
// the call's frame already.
func (a *Application) GetComposerFromPHP(required bool, disablePlugins, disableScripts *bool) (*composer.Composer, error) {
	return a.getComposer(-1, required, disablePlugins, disableScripts)
}

// getComposer is GetComposer called with argc arguments, a frame on the
// stack plugin code sees while the Composer instance is created
// (docs/PLUGINS.md §5.12: plugins are activated there); none for -1.
func (a *Application) getComposer(argc int, required bool, disablePlugins, disableScripts *bool) (*composer.Composer, error) {
	if rt := a.Runtime(); rt != nil && argc >= 0 && a.composer == nil {
		args := []any{required, nullableBoolArg(disablePlugins), nullableBoolArg(disableScripts)}
		rt.PushFrame(`Composer\Console\Application->getComposer`, a, args[:argc]...)
		defer rt.PopFrame()
	}

	dp := a.disablePluginsByDefault
	if disablePlugins != nil {
		dp = *disablePlugins
	}
	ds := a.disableScriptsByDefault
	if disableScripts != nil {
		ds = *disableScripts
	}

	if a.composer == nil {
		out := a.io
		if util.IsInputCompletionProcess() {
			out = io.NewNullIO()
		}
		disable := composer.PluginsEnabled
		if dp {
			disable = composer.PluginsDisabled
		}
		c, err := a.factory.Create(out, nil, disable, ds)
		if err != nil {
			switch {
			case phperr.InstanceOf(err, ClassInvalidArgument):
				if required {
					a.io.WriteError(err.Error(), true, io.Normal)
					if a.AreExceptionsCaught() {
						a.Exit(1)
					}

					return nil, err
				}
			case isJSONValidation(err), phperr.InstanceOf(err, "RuntimeException"):
				if required {
					return nil, err
				}
			default:
				return nil, err
			}
		} else {
			a.composer = c
		}
	}

	return a.composer, nil
}

// nullableBoolArg is a ?bool argument as PHP has it: null or the bool.
func nullableBoolArg(b *bool) any {
	if b == nil {
		return nil
	}

	return *b
}

func isJSONValidation(err error) bool {
	_, ok := errors.AsType[*json.ValidationError](err)

	return ok
}

// SetComposer sets the cached Composer instance (tests, nested runs).
func (a *Application) SetComposer(c *composer.Composer) { a.composer = c }

// ResetComposer ports resetComposer.
func (a *Application) ResetComposer() {
	a.composer = nil
	if r, ok := a.io.(interface{ ResetAuthentications() }); ok {
		r.ResetAuthentications()
	}
}

// IO ports getIO.
func (a *Application) IO() io.IO { return a.io }

// SetIO replaces the IO (what doRun creates); for tests and nested runs.
func (a *Application) SetIO(out io.IO) { a.io = out }

// Help ports getHelp: the banner of `list` and a bare run is maestro's
// logo, its version and the Composer release it reproduces, where Composer
// prints its logo and long version (docs/PORTING.md deviation 8).
func (a *Application) Help() string {
	return logo + a.maestroVersion() + " (Composer <comment>" + a.Version() + "</comment> compatible)"
}

// maestroVersion is "maestro version X", X being cmd/maestro's stamped
// build version, or just "maestro" when the runtime has none.
func (a *Application) maestroVersion() string {
	if v := a.Runtime().ClientVersion(); v != "" {
		return "<info>maestro</info> version <comment>" + v + "</comment>"
	}

	return "<info>maestro</info>"
}

// DefaultCommands ports getDefaultCommands.
func (a *Application) DefaultCommands() []console.Commander {
	commands := a.Application.DefaultCommands()
	for _, ctor := range commandConstructors {
		if ctor != nil {
			commands = append(commands, ctor())
		}
	}

	return commands
}

// commandNameBeforeBinding ports getCommandNameBeforeBinding.
func (a *Application) commandNameBeforeBinding(in console.Input) string {
	in = in.Clone()
	// Makes ArgvInput::getFirstArgument() able to distinguish an option from an argument.
	// Errors must be ignored, full binding/validation happens later when the command is known.
	_ = in.Bind(a.Definition())

	return in.FirstArgument()
}

// telemetryCommandName ports getTelemetryCommandName.
func telemetryCommandName(cmd console.Commander) string {
	class := cmd.PHPClass()
	if class == scriptAliasClass {
		return "script"
	}
	if strings.HasPrefix(class, `Composer\`) || strings.HasPrefix(class, `Symfony\Component\Console\`) {
		return cmd.Base().Name()
	}

	return "plugin"
}

// scriptAliasClass is ScriptAliasCommand's class name.
const scriptAliasClass = `Composer\Command\ScriptAliasCommand`

// LongVersion ports getLongVersion.
func (a *Application) LongVersion() string {
	branchAlias := ""
	if composer.BranchAliasVersion != "" {
		branchAlias = " (" + composer.BranchAliasVersion + ")"
	}

	return "<info>" + a.Name() + "</info> version <comment>" + a.Version() + branchAlias + "</comment> " + composer.ReleaseDate
}

// DefaultInputDefinition ports getDefaultInputDefinition.
func (a *Application) DefaultInputDefinition() *console.InputDefinition {
	definition := a.Application.DefaultInputDefinition()
	for _, o := range []*console.InputOption{
		console.MustOption("--profile", "", console.OptionValueNone, "Display timing and memory usage information", nil),
		console.MustOption("--no-plugins", "", console.OptionValueNone, "Whether to disable plugins.", nil),
		console.MustOption("--no-scripts", "", console.OptionValueNone, "Skips the execution of all scripts defined in composer.json file.", nil),
		console.MustOption("--working-dir", "-d", console.OptionValueRequired, "If specified, use the given directory as working directory.", nil),
		console.MustOption("--no-cache", "", console.OptionValueNone, "Prevent use of the cache", nil),
	} {
		if err := definition.AddOption(o); err != nil {
			panic(err)
		}
	}

	return definition
}

// InitialWorkingDirectory ports getInitialWorkingDirectory ("" for
// false).
func (a *Application) InitialWorkingDirectory() string { return a.initialWorkingDirectory }

// DisablePluginsByDefault ports getDisablePluginsByDefault.
func (a *Application) DisablePluginsByDefault() bool { return a.disablePluginsByDefault }

// DisableScriptsByDefault ports getDisableScriptsByDefault.
func (a *Application) DisableScriptsByDefault() bool { return a.disableScriptsByDefault }

// useParentDirConfigValue ports getUseParentDirConfigValue: true, false or
// "prompt".
func (a *Application) useParentDirConfigValue() (any, error) {
	cfg, err := a.factory.CreateConfig(a.io, "")
	if err != nil {
		return nil, err
	}

	return cfg.Get("use-parent-dir", 0)
}
