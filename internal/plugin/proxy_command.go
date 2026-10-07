// Commands and the Application (docs/PLUGINS.md §4.11, §5.7, D10): the
// proxies maestro's Application holds for commands written in PHP (the
// commands of CommandProvider capabilities and composer.json scripts
// naming a Symfony Command class), maestro's commands and Application as
// PHP sees them, and the `app.*` methods.
//
// A PHP command runs in PHP on the vendored Symfony Console: maestro lists,
// describes and completes it from the description PHP gave when it
// crossed (name, aliases, help, definition, ...), and its proxy's Run is
// `command.run`, where Symfony's Command::run binds, validates, initializes
// (the shim's BaseCommand::initialize), interacts and executes.

package plugin

import (
	"slices"
	"strings"
	"sync"

	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
)

// commandTable holds the proxies of PHP command objects and the Go peers of
// Applications created in PHP, by handle, and the class loaders of the
// projects whose script commands were looked for.
type commandTable struct {
	mu       sync.Mutex
	commands map[rpc.Handle]console.Commander
	apps     map[rpc.Handle]*command.Application
	appObjs  map[*command.Application]*rpc.PHPObject
	loaders  map[*composer.Composer]*eventdispatcher.LoaderContents
	// pendingLoaders are project class loaders registered before PHP ran,
	// which it registers when it starts.
	pendingLoaders []*eventdispatcher.LoaderContents
}

func newCommandTable() *commandTable {
	return &commandTable{
		commands: map[rpc.Handle]console.Commander{},
		apps:     map[rpc.Handle]*command.Application{},
		appObjs:  map[*command.Application]*rpc.PHPObject{},
		loaders:  map[*composer.Composer]*eventdispatcher.LoaderContents{},
	}
}

// commandDesc is what PHP reports of a command object
// (Maestro\Shim\Console::describe).
type commandDesc struct {
	obj                    *rpc.PHPObject
	class                  string
	isBase                 bool
	name                   string
	hasName                bool
	aliases                []string
	description, help      string
	hidden, enabled, proxy bool
	ignoreValidationErrors bool
	usages                 []string
	processTitle           string
	definition             *console.InputDefinition
}

func commandDescFrom(v any) (*commandDesc, error) {
	a, ok := v.(*php.Array)
	if !ok {
		return nil, &rpc.ProtocolError{Message: "an invalid command description"}
	}
	obj, _ := a.Get("object")
	o, ok := obj.(*rpc.PHPObject)
	if !ok {
		return nil, &rpc.ProtocolError{Message: "a command description without its object"}
	}
	d := &commandDesc{obj: o}
	d.class, _ = a.GetString("class")
	flag := func(k string) bool { v, _ := a.Get(k); return v == true }
	d.isBase = flag("base")
	d.hidden = flag("hidden")
	d.enabled = flag("enabled")
	d.proxy = flag("proxy")
	d.ignoreValidationErrors = flag("ignoreValidationErrors")
	if name, ok := a.Get("name"); ok && name != nil {
		d.name, d.hasName = php.ToString(name), true
	}
	aliases, _ := a.Get("aliases")
	d.aliases = php.ToStrings(aliases)
	d.description, _ = a.GetString("description")
	d.help, _ = a.GetString("help")
	usages, _ := a.Get("usages")
	d.usages = php.ToStrings(usages)
	if pt, ok := a.Get("processTitle"); ok && pt != nil {
		d.processTitle = php.ToString(pt)
	}
	def, ok := a.GetArray("definition")
	if !ok {
		return nil, &rpc.ProtocolError{Message: "a command description without its definition"}
	}
	var err error
	if d.definition, err = definitionFromValue(def); err != nil {
		return nil, err
	}

	return d, nil
}

// phpCommand is the PHP half of a command proxy.
type phpCommand struct {
	r       *Runtime
	obj     *rpc.PHPObject
	class   string
	enabled bool
	proxy   bool
}

// proxyCommand is a Symfony command written in PHP (a composer.json
// script naming a Command class).
type proxyCommand struct {
	*console.Command
	php *phpCommand
}

// proxyBaseCommand is a Composer BaseCommand written in PHP (a plugin
// command): `instanceof BaseCommand` for maestro's Application.
type proxyBaseCommand struct {
	*command.BaseCommand
	php *phpCommand
}

// newCommandProxy builds the proxy of a PHP command from its description.
func (r *Runtime) newCommandProxy(d *commandDesc) (console.Commander, error) {
	pc := &phpCommand{r: r, obj: d.obj, class: d.class, enabled: d.enabled, proxy: d.proxy}

	var (
		cmd  console.Commander
		base *console.Command
	)
	if d.isBase {
		c := &proxyBaseCommand{BaseCommand: command.NewBaseCommand(""), php: pc}
		c.SetImpl(c)
		cmd, base = c, c.Command
	} else {
		c := &proxyCommand{Command: console.NewCommand(""), php: pc}
		c.SetImpl(c)
		cmd, base = c, c.Command
	}

	if d.hasName {
		base.SetName(d.name)
	}
	if len(d.aliases) > 0 {
		base.SetAliases(d.aliases...)
	}
	base.SetDescription(d.description)
	base.SetHelp(d.help)
	base.SetHidden(d.hidden)
	if d.processTitle != "" {
		base.SetProcessTitle(d.processTitle)
	}
	base.SetDefinition(d.definition)
	for _, u := range d.usages {
		base.AddUsage(u)
	}
	if d.ignoreValidationErrors {
		base.IgnoreValidationErrors()
	}

	r.commands.mu.Lock()
	r.commands.commands[d.obj.H] = cmd
	r.commands.mu.Unlock()

	return cmd, nil
}

// commandProxy returns the proxy of the PHP command a description is of,
// building it the first time.
func (r *Runtime) commandProxy(v any) (console.Commander, error) {
	d, err := commandDescFrom(v)
	if err != nil {
		return nil, err
	}
	r.commands.mu.Lock()
	cmd, ok := r.commands.commands[d.obj.H]
	r.commands.mu.Unlock()
	if ok {
		return cmd, nil
	}

	return r.newCommandProxy(d)
}

// Run implements console.Commander.
func (c *proxyCommand) Run(in console.Input, out console.Output) (int, error) {
	return c.php.run(c.Command, in, out)
}

// Complete implements console.Commander.
func (c *proxyCommand) Complete(in *console.CompletionInput, s *console.CompletionSuggestions) {
	c.php.complete(c.Command, in, s)
}

// IsEnabled implements console.Enabler.
func (c *proxyCommand) IsEnabled() bool { return c.php.enabled }

// ClassName implements console.ClassNamer.
func (c *proxyCommand) ClassName() string { return c.php.class }

func (c *proxyCommand) phpObject() *rpc.PHPObject { return c.php.obj }

// Run implements console.Commander.
func (c *proxyBaseCommand) Run(in console.Input, out console.Output) (int, error) {
	return c.php.run(c.Command, in, out)
}

// Complete implements console.Commander.
func (c *proxyBaseCommand) Complete(in *console.CompletionInput, s *console.CompletionSuggestions) {
	c.php.complete(c.Command, in, s)
}

// IsEnabled implements console.Enabler.
func (c *proxyBaseCommand) IsEnabled() bool { return c.php.enabled }

// IsProxyCommand implements command.ProxyCommander.
func (c *proxyBaseCommand) IsProxyCommand() bool { return c.php.proxy }

// ClassName implements console.ClassNamer.
func (c *proxyBaseCommand) ClassName() string { return c.php.class }

func (c *proxyBaseCommand) phpObject() *rpc.PHPObject { return c.php.obj }

// commandPHPObject is implemented by the proxies.
type commandPHPObject interface{ phpObject() *rpc.PHPObject }

// applicationOf is the Composer Application a command is attached to.
func applicationOf(base *console.Command) *command.Application {
	if app := base.Application(); app != nil {
		a, _ := app.Impl().(*command.Application)

		return a
	}

	return nil
}

// run is `$command->run($input, $output)` in PHP (`command.run`), with
// the command attached to its Application and named as maestro names it
// (a script command takes its script's name).
func (p *phpCommand) run(base *console.Command, in console.Input, out console.Output) (int, error) {
	r := p.r
	var name any
	if base.Name() != "" {
		name = base.Name()
	}
	params := php.ArrayOf(
		"command", p.obj,
		"app", r.appValue(applicationOf(base)),
		"input", r.inputObject(in),
		"output", r.outputObject(out),
		"name", name,
		"description", base.Description(),
	)
	// The command's own run() is a real frame in PHP: the frame
	// maestro's command runner pushed for it is left out.
	if frames := r.framesValue(); frames != nil {
		if last, ok := frames.Values()[frames.Len()-1].(*php.Array); ok {
			if o, _ := last.Get(int64(0)); o == p.obj {
				frames = php.ListOf(frames.Values()[:frames.Len()-1]...)
			}
		}
		if frames.Len() > 0 {
			params.Set("frames", frames)
		}
	}
	v, err := r.Call("command.run", params)
	if err != nil {
		return 0, err
	}

	return php.ToNativeInt(v), nil
}

// complete is `$command->complete($input, $suggestions)` in PHP
// (`command.complete`); a failure panics, as maestro's CompleteCommand
// recovers what PHP throws.
func (p *phpCommand) complete(base *console.Command, in *console.CompletionInput, s *console.CompletionSuggestions) {
	r := p.r
	v, err := r.Call("command.complete", php.ArrayOf(
		"command", p.obj,
		"app", r.appValue(applicationOf(base)),
		"tokens", php.StringList(in.CompletionTokens()),
		"index", int64(in.CurrentIndex()),
	))
	if err != nil {
		panic(err)
	}
	res, _ := v.(*php.Array)
	if res == nil {
		return
	}
	if names, ok := res.Get("options"); ok {
		for _, name := range php.ToStrings(names) {
			if o, err := base.Definition().Option(name); err == nil {
				s.SuggestOptions(o)
			}
		}
	}
	if values, ok := res.Get("values"); ok {
		s.SuggestStrings(php.ToStrings(values)...)
	}
}

// commandMirror is one of maestro's commands as PHP sees it: an instance
// of its class (a Composer command class, a Symfony one) with the
// properties of Symfony's Command (name, aliases, description, help,
// definition, ...), as $application->find() and all() return it.
// Running one from PHP is not supported yet (its class's execute() is a
// stub).
type commandMirror struct{ cmd console.Commander }

// PHPOpaque implements php.Opaque.
func (*commandMirror) PHPOpaque() {}

// PHPClass implements rpc.Object.
func (m *commandMirror) PHPClass() string {
	if n, ok := m.cmd.(console.ClassNamer); ok {
		return n.ClassName()
	}
	switch m.cmd.(type) {
	case *console.CompleteCommand:
		return `Symfony\Component\Console\Command\CompleteCommand`
	case *console.DumpCompletionCommand:
		return `Symfony\Component\Console\Command\DumpCompletionCommand`
	}

	return classSymfonyCmd
}

// MirrorBase implements rpc.Mirror.
func (*commandMirror) MirrorBase() string { return commandBase }

// Rev implements rpc.Mirror: maestro's commands do not change once
// registered.
func (*commandMirror) Rev() uint64 { return 0 }

// MirrorSnapshot implements rpc.Mirror.
func (m *commandMirror) MirrorSnapshot() (*php.Array, error) {
	base := m.cmd.Base()
	var name any
	if base.Name() != "" {
		name = base.Name()
	}

	return php.ArrayOf(
		"name", name,
		"aliases", php.StringList(base.Aliases()),
		"description", base.Description(),
		"help", base.Help(),
		"hidden", base.IsHidden(),
		"usages", php.StringList(base.Usages()),
		"definition", definitionValue(base.NativeDefinition()),
	), nil
}

// ApplyMirror implements rpc.Mirror.
func (*commandMirror) ApplyMirror(*php.Array) error {
	return &rpc.ProtocolError{Message: "commands are not synced from PHP"}
}

// commandObject returns the object a command crosses to PHP as: a PHP
// command's own object, a mirror for maestro's.
func (r *Runtime) commandObject(cmd console.Commander) any {
	if cmd == nil || !hashable(cmd) {
		return nil
	}
	if p, ok := cmd.(commandPHPObject); ok {
		return p.phpObject()
	}

	return r.bridge.object(cmd, func() rpc.Object { return &commandMirror{cmd: cmd} })
}

// appMirror is maestro's running Application as PHP sees it: an instance of
// Composer\Console\Application whose Symfony state is its own and whose
// Composer methods are maestro's (`app.*`).
type appMirror struct{ app *command.Application }

// PHPOpaque implements php.Opaque.
func (*appMirror) PHPOpaque() {}

// PHPClass implements rpc.Object.
func (*appMirror) PHPClass() string { return classApplication }

// MirrorBase implements rpc.Mirror.
func (*appMirror) MirrorBase() string { return appBase }

// Rev implements rpc.Mirror.
func (*appMirror) Rev() uint64 { return 0 }

// MirrorSnapshot implements rpc.Mirror.
func (m *appMirror) MirrorSnapshot() (*php.Array, error) {
	return php.ArrayOf("name", m.app.Name(), "version", m.app.Version()), nil
}

// ApplyMirror implements rpc.Mirror.
func (*appMirror) ApplyMirror(*php.Array) error {
	return &rpc.ProtocolError{Message: "applications are not synced from PHP"}
}

// appValue returns the object an Application crosses to PHP as: the PHP
// object of one created in PHP, a mirror of maestro's own.
func (r *Runtime) appValue(app *command.Application) any {
	if app == nil {
		return nil
	}
	r.commands.mu.Lock()
	obj, ok := r.commands.appObjs[app]
	r.commands.mu.Unlock()
	if ok {
		return obj
	}

	return r.bridge.object(app, func() rpc.Object { return &appMirror{app: app} })
}

// appParam returns param i as an Application.
func (r *Runtime) appParam(a args, i int) (*command.Application, error) {
	switch v := a.at(i).(type) {
	case *appMirror:
		return v.app, nil
	case *rpc.PHPObject:
		r.commands.mu.Lock()
		app, ok := r.commands.apps[v.H]
		r.commands.mu.Unlock()
		if ok {
			return app, nil
		}
	}

	return nil, a.errorf("param %d is not an Application maestro knows", i)
}

// nullableBool is a ?bool param.
func nullableBool(a args, i int) *bool {
	if !a.has(i) {
		return nil
	}
	b := a.boolean(i)

	return &b
}

// addPHPCommands adds the commands PHP added to an Application with
// Symfony's add() (they are PHP's until it runs).
func (r *Runtime) addPHPCommands(app *command.Application, added any) error {
	list, _ := added.(*php.Array)
	if list == nil {
		return nil
	}
	for _, v := range list.Values() {
		cmd, err := r.commandProxy(v)
		if err != nil {
			return err
		}
		if _, err := app.Add(cmd); err != nil {
			return err
		}
	}

	return nil
}

func (r *Runtime) registerApplications() {
	method := func(name string, fn func(app *command.Application, a args) (any, error)) {
		r.Handle("app."+name, func(v any) (any, error) {
			a := argsOf("app."+name, v)
			app, err := r.appParam(a, 0)
			if err != nil {
				return nil, err
			}

			return fn(app, a)
		})
	}

	r.Handle("app.new", func(v any) (any, error) {
		a := argsOf("app.new", v)
		obj, ok := a.at(0).(*rpc.PHPObject)
		if !ok {
			return nil, a.errorf("param 0 is not a PHP object")
		}
		app := command.NewNamedApplication(r.factory(), a.str(1), a.str(2))
		r.commands.mu.Lock()
		r.commands.apps[obj.H] = app
		r.commands.appObjs[app] = obj
		r.commands.mu.Unlock()

		return nil, nil
	})
	method("run", func(app *command.Application, a args) (any, error) {
		in, err := inputParam(a, 1)
		if err != nil {
			return nil, err
		}
		out, err := r.outputParam(a, 2)
		if err != nil {
			return nil, err
		}
		if err := r.addPHPCommands(app, a.at(5)); err != nil {
			return nil, err
		}
		// PHP exits itself when its auto-exit is on.
		app.SetAutoExit(false)
		app.SetCatchExceptions(a.boolean(4))
		code, err := app.Run(in, out)

		return int64(code), err
	})
	method("doRun", func(app *command.Application, a args) (any, error) {
		in, err := inputParam(a, 1)
		if err != nil {
			return nil, err
		}
		out, err := r.outputParam(a, 2)
		if err != nil {
			return nil, err
		}
		if err := r.addPHPCommands(app, a.at(3)); err != nil {
			return nil, err
		}
		code, err := app.DoRun(in, out)

		return int64(code), err
	})
	// Commands plugin code added with Symfony's add() to the running
	// Application it found on the stack (Maestro\Shim\Frames).
	method("added", func(app *command.Application, a args) (any, error) {
		return nil, r.addPHPCommands(app, a.at(1))
	})
	method("commands", func(app *command.Application, _ args) (any, error) {
		var seen []console.Commander
		list := php.NewArray()
		for _, nc := range app.All("") {
			if slices.Contains(seen, nc.Command) {
				continue
			}
			seen = append(seen, nc.Command)
			list.Append(r.commandObject(nc.Command))
		}

		return list, nil
	})
	method("getComposer", func(app *command.Application, a args) (any, error) {
		required := true
		if a.has(1) {
			required = a.boolean(1)
		}
		c, err := app.GetComposerFromPHP(required, nullableBool(a, 2), nullableBool(a, 3))
		if err != nil || c == nil {
			return nil, err
		}

		return r.value(c), nil
	})
	method("resetComposer", func(app *command.Application, _ args) (any, error) { app.ResetComposer(); return nil, nil })
	method("getIO", func(app *command.Application, _ args) (any, error) { return r.value(app.IO()), nil })
	method("getDisablePluginsByDefault", func(app *command.Application, _ args) (any, error) {
		return app.DisablePluginsByDefault(), nil
	})
	method("getDisableScriptsByDefault", func(app *command.Application, _ args) (any, error) {
		return app.DisableScriptsByDefault(), nil
	})
	method("getInitialWorkingDirectory", func(app *command.Application, _ args) (any, error) {
		if dir := app.InitialWorkingDirectory(); dir != "" {
			return dir, nil
		}

		return false, nil
	})
}

// pluginCommands is Application::getPluginCommands() for a Composer whose
// plugins run in PHP (`capability.commands`): the commands of every
// CommandProvider capability, with Composer's checks.
func (r *Runtime) pluginCommands(c *composer.Composer, out io.IO) ([]console.Commander, error) {
	if !r.Started() {
		// No plugin was instantiated: no capability.
		return nil, nil
	}
	v, err := r.Call("capability.commands", php.ArrayOf("composer", r.value(c), "io", r.value(out)))
	if err != nil {
		return nil, err
	}
	list, _ := v.(*php.Array)
	if list == nil {
		return nil, nil
	}
	commands := make([]console.Commander, 0, list.Len())
	for _, d := range list.Values() {
		cmd, err := r.commandProxy(d)
		if err != nil {
			return nil, err
		}
		commands = append(commands, cmd)
	}

	return commands, nil
}

// PluginCommands implements internal/command's PluginCommandProvider.
func (m *Manager) PluginCommands(c *composer.Composer, out io.IO) ([]console.Commander, error) {
	return m.r.pluginCommands(c, out)
}

// RegisterProjectLoader implements internal/command's
// ProjectLoaderRegistrar: the project's class loader Composer's
// Application registers before looking for script commands (the root
// package's autoload, without its dependencies).
func (m *Manager) RegisterProjectLoader(c *composer.Composer) error {
	return m.r.registerProjectLoader(c)
}

// ScriptCommand implements internal/command's ScriptCommandProvider.
func (m *Manager) ScriptCommand(c *composer.Composer, out io.IO, script, class string) (console.Commander, bool, error) {
	return m.r.scriptCommand(c, out, script, class)
}

// registerProjectLoader computes the project's class loader (which may
// write createLoader's warnings) and registers it in PHP, now or when PHP
// starts.
func (r *Runtime) registerProjectLoader(c *composer.Composer) error {
	loader, err := c.ScriptAutoloader().CreateLoader(nil)
	if err != nil {
		return err
	}
	r.commands.mu.Lock()
	r.commands.loaders[c] = loader
	r.commands.mu.Unlock()

	if !r.Started() {
		r.commands.mu.Lock()
		r.commands.pendingLoaders = append(r.commands.pendingLoaders, loader)
		r.commands.mu.Unlock()

		return nil
	}

	return r.sendProjectLoader(loader)
}

func (r *Runtime) sendProjectLoader(loader *eventdispatcher.LoaderContents) error {
	_, err := r.Call("autoload.register", loaderValue(loader))

	return err
}

// loaderValue is a class loader's contents as the shim rebuilds them.
func loaderValue(loader *eventdispatcher.LoaderContents) *php.Array {
	a := php.ArrayOf("vendorDir", loader.VendorDir)
	if loader.Psr0 != nil {
		a.Set("psr0", loader.Psr0)
	}
	if loader.Psr4 != nil {
		a.Set("psr4", loader.Psr4)
	}
	if loader.ClassMap != nil {
		a.Set("classmap", loader.ClassMap)
	}

	return a
}

// scriptCommand is the command of a composer.json script naming a
// Symfony Command class (Application::doRun: `class_exists($dummy) &&
// is_subclass_of($dummy, SymfonyCommand::class)`, then `new
// $dummy($script)`); ok is false for any other script. PHP starts only when
// it runs already or the project's class loader can find a file for the
// class.
func (r *Runtime) scriptCommand(c *composer.Composer, out io.IO, script, class string) (console.Commander, bool, error) {
	if !r.Started() {
		r.commands.mu.Lock()
		loader := r.commands.loaders[c]
		r.commands.mu.Unlock()
		if loader == nil || !loaderMayDefine(loader, class) {
			return nil, false, nil
		}
	}
	if err := r.startFor("script "+class, out); err != nil {
		return nil, false, err
	}
	v, err := r.Call("command.script", php.ArrayOf("class", class, "script", script, "io", r.value(out)))
	if err != nil || v == nil {
		return nil, false, err
	}
	d, err := commandDescFrom(v)
	if err != nil {
		return nil, false, err
	}
	cmd, err := r.newCommandProxy(d)
	if err != nil {
		return nil, false, err
	}

	return cmd, true, nil
}

// loaderMayDefine reports whether a class loader finds a file for a class
// (ClassLoader::findFile, without APCu and the include path): the only way
// a class can exist before PHP runs.
func loaderMayDefine(l *eventdispatcher.LoaderContents, class string) bool {
	class = strings.TrimPrefix(class, `\`)
	if l.ClassMap != nil {
		if _, ok := l.ClassMap.Get(class); ok {
			return true
		}
	}

	logicalPathPsr4 := strings.ReplaceAll(class, `\`, "/") + ".php"
	if l.Psr4 != nil {
		for k, paths := range l.Psr4.All() {
			prefix := k.String()
			if !strings.HasPrefix(class, prefix) {
				continue
			}
			for _, dir := range loaderPaths(paths) {
				if php.FileExists(dir + "/" + logicalPathPsr4[len(prefix):]) {
					return true
				}
			}
		}
	}

	var logicalPathPsr0 string
	if pos := strings.LastIndexByte(class, '\\'); pos >= 0 {
		logicalPathPsr0 = logicalPathPsr4[:pos+1] + strings.ReplaceAll(logicalPathPsr4[pos+1:], "_", "/")
	} else {
		logicalPathPsr0 = strings.ReplaceAll(class, "_", "/") + ".php"
	}
	if l.Psr0 != nil {
		for k, paths := range l.Psr0.All() {
			if !strings.HasPrefix(class, k.String()) {
				continue
			}
			for _, dir := range loaderPaths(paths) {
				if php.FileExists(dir + "/" + logicalPathPsr0) {
					return true
				}
			}
		}
	}

	return false
}

// loaderPaths is a prefix's path or list of paths.
func loaderPaths(v any) []string {
	if a, ok := v.(*php.Array); ok {
		return php.ToStrings(a)
	}

	return []string{php.ToString(v)}
}
