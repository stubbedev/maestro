// Ports src/Composer/Command/BaseCommand.php.

package command

import (
	"strings"

	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/filter"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/policy"
	"github.com/stubbedev/maestro/internal/util"
)

const baseCommandFile = "BaseCommand.php"

// ProxyCommander is isProxyCommand(): a command meant to call another
// command (no duplicated warnings).
type ProxyCommander interface {
	IsProxyCommand() bool
}

// composerCommand is implemented by every type embedding *BaseCommand
// (`instanceof BaseCommand`).
type composerCommand interface {
	baseCommand() *BaseCommand
}

func isBaseCommand(c console.Commander) bool {
	_, ok := c.(composerCommand)

	return ok
}

// AsBaseCommand returns the BaseCommand of c (nil when c is not a
// Composer command).
func AsBaseCommand(c console.Commander) *BaseCommand {
	if b, ok := c.(composerCommand); ok {
		return b.baseCommand()
	}

	return nil
}

// BaseCommand is Composer\Command\BaseCommand. A command embeds it and
// calls SetImpl(itself) in its constructor:
//
//	type AboutCommand struct{ *BaseCommand }
//
//	func NewAboutCommand() *AboutCommand {
//		c := &AboutCommand{BaseCommand: NewBaseCommand("about")}
//		c.SetImpl(c)
//		c.SetDescription(...)
//		return c
//	}
//
// The concrete command implements Execute (console.Executor) and may
// override Initialize (calling c.BaseCommand.Initialize as
// parent::initialize), Interact, Complete, IsProxyCommand and IsEnabled.
type BaseCommand struct {
	*console.Command

	impl     console.Commander
	composer *composer.Composer
	io       io.IO
	// alwaysDisablePlugins is `$this instanceof SelfUpdateCommand`.
	alwaysDisablePlugins bool
}

// NewBaseCommand ports the constructor (Command::__construct($name)); an
// empty name leaves it unset (configure() sets it).
func NewBaseCommand(name string) *BaseCommand {
	return &BaseCommand{Command: console.NewCommand(name)}
}

func (c *BaseCommand) baseCommand() *BaseCommand { return c }

// SetImpl records the command embedding c (its overrides).
func (c *BaseCommand) SetImpl(impl console.Commander) {
	c.impl = impl
	c.Command.SetImpl(impl)
}

// Impl returns the concrete command (c itself when SetImpl was not
// called).
func (c *BaseCommand) Impl() console.Commander {
	if c.impl != nil {
		return c.impl
	}

	return c
}

// MethodClass implements console.MethodClasser: the class declaring the
// method of Composer's command class (src/Composer/Command of Composer
// 2.10.3), as an exception's trace frames name it. initialize() is
// BaseCommand's, except where BaseConfigCommand, ConfigCommand and
// InitCommand override it; interact() is Symfony's no-op except in
// ExecCommand, InitCommand, RequireCommand and RunScriptCommand; run() is
// Symfony's except in GlobalCommand; execute() is every command's own.
func (*BaseCommand) MethodClass(class, method string) string {
	const ns = `Composer\Command\`
	switch method {
	case "initialize":
		switch class {
		case ns + "ConfigCommand", ns + "InitCommand":
			return class
		case ns + "RepositoryCommand", ns + "PolicyCommand":
			return ns + "BaseConfigCommand"
		}

		return ns + "BaseCommand"
	case "interact":
		switch class {
		case ns + "ExecCommand", ns + "InitCommand", ns + "RequireCommand", ns + "RunScriptCommand":
			return class
		}

		return `Symfony\Component\Console\Command\Command`
	case "run":
		if class == ns+"GlobalCommand" {
			return class
		}

		return `Symfony\Component\Console\Command\Command`
	}

	return class
}

// App ports getApplication(): the Composer Application, or the
// RuntimeException PHP throws when the command is not attached to one.
func (c *BaseCommand) App() (*Application, error) {
	if app := c.application(); app != nil {
		return app, nil
	}

	return nil, NewError(ClassRuntime, baseCommandFile, 66, `Composer commands can only work with an Composer\Console\Application instance set`)
}

// application is parent::getApplication() when it is a Composer
// Application, else nil.
func (c *BaseCommand) application() *Application {
	base := c.Application()
	if base == nil {
		return nil
	}
	app, _ := base.Impl().(*Application)

	return app
}

// GetComposer ports the deprecated getComposer($required, ...).
func (c *BaseCommand) GetComposer(required bool, disablePlugins, disableScripts *bool) (*composer.Composer, error) {
	if required {
		return c.RequireComposer(disablePlugins, disableScripts)
	}

	return c.TryComposer(disablePlugins, disableScripts)
}

// RequireComposer ports requireComposer: nil flags read --no-plugins /
// --no-scripts.
func (c *BaseCommand) RequireComposer(disablePlugins, disableScripts *bool) (*composer.Composer, error) {
	if c.composer == nil {
		app := c.application()
		if app == nil {
			return nil, NewError(ClassRuntime, baseCommandFile, 106, `Could not create a Composer\Composer instance, you must inject one if this command is not used with a Composer\Console\Application instance`)
		}
		leave := phperr.Enter(`Composer\Console\Application->getComposer`, baseCommandFile, 103)
		composer, err := app.GetComposer(true, disablePlugins, disableScripts)
		leave()
		if err != nil {
			return nil, phperr.Call(err, `Composer\Console\Application->getComposer`, baseCommandFile, 103)
		}
		c.composer = composer
	}

	return c.composer, nil
}

// commandClass is get_class($this): the class trait methods' frames name.
func (c *BaseCommand) commandClass() string {
	if n, ok := c.impl.(console.ClassNamer); ok {
		return n.ClassName()
	}

	return `Composer\Command\BaseCommand`
}

// requireComposerAt is $this->requireComposer() called at file:line of
// Composer's sources (the frame it adds to an exception's trace).
func (c *BaseCommand) requireComposerAt(file string, line int) (*composer.Composer, error) {
	leave := phperr.Enter(`Composer\Command\BaseCommand->requireComposer`, file, line)
	comp, err := c.RequireComposer(nil, nil)
	leave()

	return comp, phperr.Call(err, `Composer\Command\BaseCommand->requireComposer`, file, line)
}

// TryComposer ports tryComposer: nil when there is no composer.json (or
// it is invalid in a way getComposer(false) tolerates).
func (c *BaseCommand) TryComposer(disablePlugins, disableScripts *bool) (*composer.Composer, error) {
	if c.composer == nil {
		if app := c.application(); app != nil {
			leave := phperr.Enter(`Composer\Console\Application->getComposer`, baseCommandFile, 129)
			composer, err := app.GetComposer(false, disablePlugins, disableScripts)
			leave()
			if err != nil {
				return nil, phperr.Call(err, `Composer\Console\Application->getComposer`, baseCommandFile, 129)
			}
			c.composer = composer
		}
	}

	return c.composer, nil
}

// SetComposer ports setComposer.
func (c *BaseCommand) SetComposer(composer *composer.Composer) { c.composer = composer }

// ResetComposer ports resetComposer.
func (c *BaseCommand) ResetComposer() error {
	c.composer = nil
	app, err := c.App()
	if err != nil {
		return err
	}
	app.ResetComposer()

	return nil
}

// IsProxyCommand ports isProxyCommand (false; commands override it).
func (*BaseCommand) IsProxyCommand() bool { return false }

// IO ports getIO.
func (c *BaseCommand) IO() io.IO {
	if c.io == nil {
		if app := c.application(); app != nil {
			c.io = app.IO()
		} else {
			c.io = io.NewNullIO()
		}
	}

	return c.io
}

// SetIO ports setIO.
func (c *BaseCommand) SetIO(out io.IO) { c.io = out }

// Complete ports complete(): the suggested values of the option or
// argument being completed.
func (c *BaseCommand) Complete(in *console.CompletionInput, suggestions *console.CompletionSuggestions) {
	definition := c.Definition()
	name := in.CompletionName()
	switch in.CompletionType() {
	case console.CompletionTypeOptionValue:
		if definition.HasOption(name) {
			if o, err := definition.Option(name); err == nil {
				o.Complete(in, suggestions)

				return
			}
		}
	case console.CompletionTypeArgumentValue:
		if definition.HasArgument(name) {
			if a, err := definition.Argument(name); err == nil {
				a.Complete(in, suggestions)

				return
			}
		}
	}
}

// envOptions are the COMPOSER_* variables initialize() maps to options.
var envOptions = []struct {
	env     string
	options []string
}{
	{"COMPOSER_NO_AUDIT", []string{"no-audit"}},
	{"COMPOSER_NO_DEV", []string{"no-dev", "update-no-dev"}},
	{"COMPOSER_PREFER_STABLE", []string{"prefer-stable"}},
	{"COMPOSER_PREFER_LOWEST", []string{"prefer-lowest"}},
	{"COMPOSER_MINIMAL_CHANGES", []string{"minimal-changes"}},
	{"COMPOSER_WITH_DEPENDENCIES", []string{"with-dependencies"}},
	{"COMPOSER_WITH_ALL_DEPENDENCIES", []string{"with-all-dependencies"}},
	{"COMPOSER_NO_SECURITY_BLOCKING", []string{"no-security-blocking"}},
	{"COMPOSER_NO_BLOCKING", []string{"no-blocking"}},
}

// Initialize ports initialize(): it loads a plugin-enabled Composer
// (local or global), dispatches PRE_COMMAND_RUN and applies the COMPOSER_*
// environment options.
func (c *BaseCommand) Initialize(in console.Input, _ console.Output) error {
	// initialize a plugin-enabled Composer instance, either local or global
	disablePlugins := in.HasParameterOption([]string{"--no-plugins"}, false)
	disableScripts := in.HasParameterOption([]string{"--no-scripts"}, false)

	app := c.application()
	if app != nil && app.DisablePluginsByDefault() {
		disablePlugins = true
	}
	if app != nil && app.DisableScriptsByDefault() {
		disableScripts = true
	}

	if c.alwaysDisablePlugins {
		disablePlugins = true
		disableScripts = true
	}

	leave := phperr.Enter(`Composer\Command\BaseCommand->tryComposer`, baseCommandFile, 240)
	composer, err := c.TryComposer(&disablePlugins, &disableScripts)
	leave()
	if err != nil {
		return phperr.Call(err, `Composer\Command\BaseCommand->tryComposer`, baseCommandFile, 240)
	}
	out := c.IO()

	if composer == nil && app != nil {
		if composer, err = app.Factory().CreateGlobal(c.IO(), disablePlugins, disableScripts); err != nil {
			return phperr.Call(err, `Composer\Factory::createGlobal`, baseCommandFile, 244)
		}
	}
	if composer != nil {
		event := eventdispatcher.NewPreCommandRunEvent(eventdispatcher.PreCommandRun, in, c.Name())
		if _, err := composer.EventDispatcher().Dispatch(event.Name(), event); err != nil {
			return phperr.Call(err, `Composer\EventDispatcher\EventDispatcher->dispatch`, baseCommandFile, 248)
		}
	}

	if in.HasParameterOption([]string{"--no-ansi"}, false) && in.HasOption("no-progress") {
		in.SetOption("no-progress", true)
	}

	for _, e := range envOptions {
		for _, name := range e.options {
			if in.HasOption(name) {
				if in.Option(name) == false && envTruthy(e.env) {
					in.SetOption(name, true)
				}
			}
		}
	}

	if in.HasOption("ignore-platform-reqs") {
		if !console.BoolOption(in, "ignore-platform-reqs") && envTruthy("COMPOSER_IGNORE_PLATFORM_REQS") {
			in.SetOption("ignore-platform-reqs", true)

			out.WriteError("<warning>COMPOSER_IGNORE_PLATFORM_REQS is set. You may experience unexpected errors.</warning>", true, io.Normal)
		}
	}

	if in.HasOption("ignore-platform-req") && (!in.HasOption("ignore-platform-reqs") || !console.BoolOption(in, "ignore-platform-reqs")) {
		env, ok := util.GetEnv("COMPOSER_IGNORE_PLATFORM_REQ")
		if len(console.StringsOption(in, "ignore-platform-req")) == 0 && ok && env != "" {
			values := strings.Split(env, ",")
			list := make([]any, len(values))
			for i, v := range values {
				list[i] = v
			}
			in.SetOption("ignore-platform-req", list)

			out.WriteError("<warning>COMPOSER_IGNORE_PLATFORM_REQ is set to ignore "+env+". You may experience unexpected errors.</warning>", true, io.Normal)
		}
	}

	return nil
}

// CreateComposerInstance ports createComposerInstance: Factory::create
// honouring --no-plugins/--no-scripts and the Application defaults.
// config is nil, a file name or a *php.Array.
func (c *BaseCommand) CreateComposerInstance(in console.Input, out io.IO, cfg any, disablePlugins, disableScripts bool) (*composer.Composer, error) {
	disablePlugins = disablePlugins || in.HasParameterOption([]string{"--no-plugins"}, false)
	disableScripts = disableScripts || in.HasParameterOption([]string{"--no-scripts"}, false)

	factory := &composer.Factory{}
	if app := c.application(); app != nil {
		if app.DisablePluginsByDefault() {
			disablePlugins = true
		}
		if app.DisableScriptsByDefault() {
			disableScripts = true
		}
		factory = app.Factory()
	}

	disable := composer.PluginsEnabled
	if disablePlugins {
		disable = composer.PluginsDisabled
	}

	leave := phperr.Enter(`Composer\Factory::create`, baseCommandFile, 315)
	comp, err := factory.Create(out, cfg, disable, disableScripts)
	leave()

	return comp, phperr.Call(err, `Composer\Factory::create`, baseCommandFile, 315)
}

// PreferredInstallOptions ports getPreferredInstallOptions.
func (*BaseCommand) PreferredInstallOptions(cfg *config.Config, in console.Input, keepVcsRequiresPreferSource bool) (preferSource, preferDist bool, err error) {
	v, err := cfg.Get("preferred-install", 0)
	if err != nil {
		return false, false, err
	}
	switch v {
	case "source":
		preferSource = true
	case "dist":
		preferDist = true
	}

	if !in.HasOption("prefer-dist") || !in.HasOption("prefer-source") {
		return preferSource, preferDist, nil
	}

	if in.HasOption("prefer-install") {
		if pi, ok := in.Option("prefer-install").(string); ok {
			if console.BoolOption(in, "prefer-source") {
				return false, false, NewError(ClassInvalidArgument, baseCommandFile, 347, "--prefer-source can not be used together with --prefer-install")
			}
			if console.BoolOption(in, "prefer-dist") {
				return false, false, NewError(ClassInvalidArgument, baseCommandFile, 350, "--prefer-dist can not be used together with --prefer-install")
			}
			switch pi {
			case "dist":
				in.SetOption("prefer-dist", true)
			case "source":
				in.SetOption("prefer-source", true)
			case "auto":
				preferDist = false
				preferSource = false
			default:
				return false, false, NewError(ClassUnexpectedValue, baseCommandFile, 364, `--prefer-install accepts one of "dist", "source" or "auto", got `+pi)
			}
		}
	}

	keepVcs := keepVcsRequiresPreferSource && in.HasOption("keep-vcs") && console.BoolOption(in, "keep-vcs")
	if console.BoolOption(in, "prefer-source") || console.BoolOption(in, "prefer-dist") || keepVcs {
		preferSource = console.BoolOption(in, "prefer-source") || keepVcs
		preferDist = console.BoolOption(in, "prefer-dist")
	}

	return preferSource, preferDist, nil
}

// PlatformRequirementFilter ports getPlatformRequirementFilter.
func (*BaseCommand) PlatformRequirementFilter(in console.Input) (filter.PlatformRequirementFilter, error) {
	if !in.HasOption("ignore-platform-reqs") || !in.HasOption("ignore-platform-req") {
		return nil, NewError(ClassLogic, baseCommandFile, 379, "Calling getPlatformRequirementFilter from a command which does not define the --ignore-platform-req[s] flags is not permitted.")
	}

	if in.Option("ignore-platform-reqs") == true {
		return filter.IgnoreAllFilter(), nil
	}

	if ignores := console.StringsOption(in, "ignore-platform-req"); len(ignores) > 0 {
		return filter.FromBoolOrList(ignores)
	}

	return filter.IgnoreNothingFilter(), nil
}

// FormatRequirements ports formatRequirements: name => version, in input
// order.
func (c *BaseCommand) FormatRequirements(requirements []string) (*php.Array, error) {
	requires := php.NewArray()
	for _, requirement := range c.NormalizeRequirements(requirements) {
		if !requirement.Version.Valid {
			return nil, NewError(ClassUnexpectedValue, baseCommandFile, 405, "Option "+requirement.Name+" is missing a version constraint, use e.g. "+requirement.Name+":^1.0")
		}
		requires.Set(requirement.Name, requirement.Version.S)
	}

	return requires, nil
}

// NormalizeRequirements ports normalizeRequirements.
func (*BaseCommand) NormalizeRequirements(requirements []string) []pkg.NameVersionPair {
	return pkg.NewVersionParser().ParseNameVersionPairs(requirements)
}

// RenderTable ports renderTable: a compact-style table of rows (each a
// []any of cells or a *console.TableSeparator).
func (*BaseCommand) RenderTable(rows []any, out console.Output) error {
	renderer := console.NewTable(out)
	if err := renderer.SetStyle("compact"); err != nil {
		return err
	}
	if err := renderer.SetRows(rows); err != nil {
		return err
	}

	return renderer.Render()
}

// TerminalWidth ports getTerminalWidth.
func (*BaseCommand) TerminalWidth() int {
	width := console.Terminal{}.Width()

	if util.IsWindows() {
		width--
	} else {
		width = max(80, width)
	}

	return width
}

// AuditFormat ports getAuditFormat; optName is "format" or "audit-format".
func (*BaseCommand) AuditFormat(in console.Input, optName string) (string, error) {
	if !in.HasOption(optName) {
		return "", NewError(ClassLogic, baseCommandFile, 462, "This should not be called on a Command which has no "+optName+" option defined.")
	}

	val, _ := in.Option(optName).(string)
	for _, f := range advisory.Formats {
		if in.Option(optName) == f {
			return val, nil
		}
	}

	return "", NewError(ClassInvalidArgument, baseCommandFile, 467, "--"+optName+" must be one of "+joinFormats()+".")
}

func joinFormats() string { return strings.Join(advisory.Formats[:], ", ") }

// CreatePolicyConfig ports createPolicyConfig; in may be nil.
func (*BaseCommand) CreatePolicyConfig(cfg *config.Config, in console.Input) (*policy.PolicyConfig, error) {
	policyConfig, err := policy.FromConfig(cfg)
	if err != nil {
		return nil, phperr.Call(err, `Composer\Policy\PolicyConfig::fromConfig`, baseCommandFile, 480)
	}

	// --no-blocking / --no-security-blocking: disable ALL blocking (advisories + malware + abandoned + custom)
	noBlocking, _, err := util.GetBoolEnv("COMPOSER_NO_BLOCKING")
	if err != nil {
		return nil, err
	}
	if !noBlocking {
		if noBlocking, _, err = util.GetBoolEnv("COMPOSER_NO_SECURITY_BLOCKING"); err != nil {
			return nil, err
		}
	}
	noBlocking = noBlocking ||
		(in != nil && in.HasOption("no-security-blocking") && console.BoolOption(in, "no-security-blocking")) ||
		(in != nil && in.HasOption("no-blocking") && console.BoolOption(in, "no-blocking"))

	if noBlocking {
		policyConfig = policyConfig.WithBlockingDisabled()
	}

	return policyConfig, nil
}

// CreateAuditConfig ports createAuditConfig.
func (c *BaseCommand) CreateAuditConfig(in console.Input) (advisory.AuditConfig, error) {
	// Handle both --audit and --no-audit flags
	var audit bool
	if in.HasOption("audit") {
		audit = console.BoolOption(in, "audit")
	} else {
		audit = !in.HasOption("no-audit") || !console.BoolOption(in, "no-audit")
	}
	auditFormat := advisory.FormatSummary
	if in.HasOption("audit-format") {
		f, err := c.AuditFormat(in, "audit-format")
		if err != nil {
			return advisory.AuditConfig{}, err
		}
		auditFormat = f
	}

	return advisory.AuditConfig{Audit: audit, AuditFormat: auditFormat}, nil
}
