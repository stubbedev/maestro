// Ports src/Composer/Command/RunScriptCommand.php.

package command

import (
	"errors"
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/script"
	"github.com/stubbedev/maestro/internal/util"
)

const runScriptCommandFile = "RunScriptCommand.php"

func init() {
	registerCommand(OrderRunScript, func() console.Commander { return NewRunScriptCommand() })
}

// runScriptEvents is RunScriptCommand::$scriptEvents.
var runScriptEvents = []string{
	script.PreInstallCmd,
	script.PostInstallCmd,
	script.PreUpdateCmd,
	script.PostUpdateCmd,
	script.PreStatusCmd,
	script.PostStatusCmd,
	script.PostRootPackageInstall,
	script.PostCreateProjectCmd,
	script.PreArchiveCmd,
	script.PostArchiveCmd,
	script.PreAutoloadDump,
	script.PostAutoloadDump,
}

// RunScriptCommand is Composer\Command\RunScriptCommand.
type RunScriptCommand struct{ *BaseCommand }

// NewRunScriptCommand ports new RunScriptCommand() (configure()).
func NewRunScriptCommand() *RunScriptCommand {
	c := &RunScriptCommand{BaseCommand: NewBaseCommand("")}
	c.SetImpl(c)
	c.SetName("run-script")
	c.SetAliases("run")
	c.SetDescription("Runs the scripts defined in composer.json")
	c.SetDefinitionItems(
		console.MustArgument("script", console.ArgumentOptional, "Script name to run.", nil).WithSuggestFunc(func(*console.CompletionInput, *console.CompletionSuggestions) []console.Suggestion {
			scripts, err := c.scripts()
			if err != nil {
				return nil
			}
			suggestions := make([]console.Suggestion, len(scripts))
			for i, s := range scripts {
				suggestions[i] = console.Suggestion{Value: s.name}
			}

			return suggestions
		}),
		console.MustArgument("args", console.ArgumentIsArray|console.ArgumentOptional, "", nil),
		console.MustOption("timeout", "", console.OptionValueRequired, "Sets script timeout in seconds, or 0 for never.", nil),
		console.MustOption("dev", "", console.OptionValueNone, "Sets the dev mode.", nil),
		console.MustOption("no-dev", "", console.OptionValueNone, "Disables the dev mode.", nil),
		console.MustOption("list", "l", console.OptionValueNone, "List scripts.", nil),
	)
	c.SetHelp(`The <info>run-script</info> command runs scripts defined in composer.json:

<info>php composer.phar run-script post-update-cmd</info>

Read more at https://getcomposer.org/doc/03-cli.md#run-script-run`)

	return c
}

// ClassName implements console.ClassNamer.
func (*RunScriptCommand) ClassName() string { return `Composer\Command\RunScriptCommand` }

// Interact ports interact().
func (c *RunScriptCommand) Interact(in console.Input, _ console.Output) error {
	scripts, err := c.scripts()
	if err != nil {
		return err
	}
	if len(scripts) == 0 {
		return nil
	}

	if in.Argument("script") != nil || console.BoolOption(in, "list") {
		return nil
	}

	options := php.NewArray()
	for _, s := range scripts {
		options.Set(s.name, s.description)
	}
	selected, err := c.IO().Select("Script to run: ", options, "", 1, `Invalid script name "%s"`, false)
	if err != nil {
		return err
	}

	in.SetArgument("script", selected)

	return nil
}

// Execute ports execute().
func (c *RunScriptCommand) Execute(in console.Input, out console.Output) (int, error) {
	if console.BoolOption(in, "list") {
		return c.listScripts(out)
	}

	scriptArg := in.Argument("script")
	if scriptArg == nil {
		return 0, NewError(ClassRuntime, runScriptCommandFile, 111, `Missing required argument "script"`)
	}
	scriptName := php.ToString(scriptArg)

	if !slices.Contains(runScriptEvents, scriptName) {
		if scriptEventConstants[strings.ReplaceAll(strings.ToUpper(scriptName), "-", "_")] {
			return 0, NewError(ClassInvalidArgument, runScriptCommandFile, 116, `Script "`+scriptName+`" cannot be run with this command`)
		}
	}

	c2, err := c.RequireComposer(nil, nil)
	if err != nil {
		return 0, err
	}
	devMode := console.BoolOption(in, "dev") || !console.BoolOption(in, "no-dev")
	event := eventdispatcher.NewScriptEvent(scriptName, c2, c.IO(), devMode, nil, nil)
	if !c2.EventDispatcher().HasEventListeners(event) {
		return 0, NewError(ClassInvalidArgument, runScriptCommandFile, 125, `Script "`+scriptName+`" is not defined in this package`)
	}

	args := console.StringsArgument(in, "args")

	if timeout := in.Option("timeout"); timeout != nil {
		t := php.ToString(timeout)
		if !ctypeDigit(t) {
			return 0, NewError(ClassRuntime, runScriptCommandFile, 132, "Timeout value must be numeric and positive if defined, or 0 for forever")
		}
		// Override global timeout set before in Composer by environment or config
		util.SetProcessTimeout(int(php.ToInt(t)))
	}

	util.PutEnv("COMPOSER_DEV_MODE", devModeEnv(devMode))

	return c2.EventDispatcher().DispatchScript(scriptName, devMode, args, nil)
}

// devModeEnv is `$devMode ? '1' : '0'`.
func devModeEnv(devMode bool) string {
	if devMode {
		return "1"
	}

	return "0"
}

// ctypeDigit is ctype_digit() of a string: non-empty, only 0-9.
func ctypeDigit(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}

	return true
}

// listScripts ports listScripts.
func (c *RunScriptCommand) listScripts(out console.Output) (int, error) {
	scripts, err := c.scripts()
	if err != nil {
		return 0, err
	}
	if len(scripts) == 0 {
		return 0, nil
	}

	c.IO().WriteError("<info>scripts:</info>", true, io.Normal)
	table := make([]any, 0, len(scripts))
	for _, s := range scripts {
		table = append(table, []any{"  " + s.name, s.description})
	}

	if err := c.RenderTable(table, out); err != nil {
		return 0, err
	}

	return 0, nil
}

type scriptInfo struct {
	name        string
	description string
}

// scripts ports getScripts.
func (c *RunScriptCommand) scripts() ([]scriptInfo, error) {
	cmp, err := c.RequireComposer(nil, nil)
	if err != nil {
		return nil, err
	}
	scripts := cmp.Package().Scripts()
	if scripts.Len() == 0 {
		return nil, nil
	}

	app, err := c.App()
	if err != nil {
		return nil, err
	}
	var result []scriptInfo
	for key := range scripts.All() {
		name := key.String()
		description := ""
		cmd, ferr := app.Find(name)
		switch {
		case ferr == nil:
			description = cmd.Base().Description()
		case errors.Is(ferr, console.ErrCommandNotFound):
			// ignore scripts that have no command associated, like native Composer script listeners
		default:
			return nil, ferr
		}
		result = append(result, scriptInfo{name: name, description: description})
	}

	return result, nil
}
