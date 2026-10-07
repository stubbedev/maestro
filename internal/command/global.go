// Ports src/Composer/Command/GlobalCommand.php.

package command

import (
	"os"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

func init() {
	registerCommand(OrderGlobal, func() console.Commander { return NewGlobalCommand() })
}

// GlobalCommand is Composer\Command\GlobalCommand.
type GlobalCommand struct{ *BaseCommand }

// NewGlobalCommand ports new GlobalCommand() (configure()).
func NewGlobalCommand() *GlobalCommand {
	c := &GlobalCommand{BaseCommand: NewBaseCommand("")}
	c.SetImpl(c)
	c.SetName("global")
	c.SetDescription("Allows running commands in the global composer dir ($COMPOSER_HOME)")
	c.SetDefinitionItems(
		console.MustArgument("command-name", console.ArgumentRequired, "", nil),
		console.MustArgument("args", console.ArgumentIsArray|console.ArgumentOptional, "", nil),
	)
	c.SetHelp(`Use this command as a wrapper to run other Composer commands
within the global context of COMPOSER_HOME.

You can use this to install CLI utilities globally, all you need
is to add the COMPOSER_HOME/vendor/bin dir to your PATH env var.

COMPOSER_HOME is c:\Users\<user>\AppData\Roaming\Composer on Windows
and /home/<user>/.composer on unix systems.

If your system uses freedesktop.org standards, then it will first check
XDG_CONFIG_HOME or default to /home/<user>/.config/composer

Note: This path may vary depending on customizations to bin-dir in
composer.json or the environmental variable COMPOSER_BIN_DIR.

Read more at https://getcomposer.org/doc/03-cli.md#global`)

	return c
}

// PHPClass implements php.Classer.
func (*GlobalCommand) PHPClass() string { return `Composer\Command\GlobalCommand` }

// IsProxyCommand ports isProxyCommand.
func (*GlobalCommand) IsProxyCommand() bool { return true }

// Complete ports complete().
func (c *GlobalCommand) Complete(in *console.CompletionInput, suggestions *console.CompletionSuggestions) {
	app, err := c.App()
	if err != nil {
		return
	}
	if in.MustSuggestArgumentValuesFor("command-name") {
		var names []string
		for _, nc := range app.All("") {
			if !nc.Command.Base().IsHidden() {
				names = append(names, nc.Command.Base().Name())
			}
		}
		suggestions.SuggestStrings(names...)

		return
	}

	commandName := console.StringArgument(in, "command-name")
	if !app.Has(commandName) {
		return
	}
	sub, err := c.prepareSubcommandInput(in, true)
	if err != nil {
		return
	}
	input := console.CompletionInputFromString(sub.String(), 2)
	command, err := app.Find(commandName)
	if err != nil {
		return
	}
	if err := command.Base().MergeApplicationDefinition(true); err != nil {
		return
	}

	if err := input.Bind(command.Base().Definition()); err != nil {
		return
	}
	command.Complete(input, suggestions)
}

// Run ports run(): it runs the command named after "global" in
// COMPOSER_HOME, or shows this command's help when there is none.
func (c *GlobalCommand) Run(in console.Input, out console.Output) (int, error) {
	// extract real command name
	// Splitting on whitespace cannot fail: \s+ does bounded work per start
	// position.
	tokens, _ := php.PregSplit(`{\s+}`, in.String(), -1, 0)
	var args []string
	for _, token := range tokens {
		if php.Truthy(token) && token[0] != '-' {
			args = append(args, token)
			if len(args) >= 2 {
				break
			}
		}
	}

	// show help for this command if no command was found
	if len(args) < 2 {
		return c.Command.Run(in, out)
	}

	sub, err := c.prepareSubcommandInput(in, false)
	if err != nil {
		return 0, err
	}

	app, err := c.App()
	if err != nil {
		return 0, err
	}

	return app.RunFrom("GlobalCommand.php", 120, sub, out)
}

// prepareSubcommandInput ports prepareSubcommandInput: it changes to the
// global directory and returns the input without the "global" token.
func (c *GlobalCommand) prepareSubcommandInput(in console.Input, quiet bool) (*console.StringInput, error) {
	// The COMPOSER env var should not apply to the global execution scope
	if v, ok := util.GetEnv("COMPOSER"); ok && php.Truthy(v) {
		util.ClearEnv("COMPOSER")
	}

	app, err := c.App()
	if err != nil {
		return nil, err
	}

	// change to global dir
	cfg, err := app.Factory().CreateConfig(nil, "")
	if err != nil {
		return nil, err
	}
	homeValue, err := cfg.Get("home", 0)
	if err != nil {
		return nil, err
	}
	home := php.ToString(homeValue)

	if !php.IsDir(home) {
		if err := util.EnsureDirectoryExists(home); err != nil {
			return nil, err
		}
		if !php.IsDir(home) {
			return nil, NewError(ClassRuntime, "Could not create home directory")
		}
	}

	if err := os.Chdir(home); err != nil {
		e := NewError(ClassRuntime, `Could not switch to home directory "`+home+`"`)
		e.Prev = &util.ErrorException{Message: "chdir(): " + chdirWarning(err)}

		return nil, e
	}
	if !quiet {
		c.IO().WriteError("<info>Changed current directory to "+home+"</info>", true, io.Normal)
	}

	// create new input without "global" command prefix
	// The pattern cannot fail: it matches a bounded literal prefix at each
	// start position.
	stripped, _, _ := php.PregReplace(`{\bg(?:l(?:o(?:b(?:a(?:l)?)?)?)?)?\b}`, "", in.String(), 1)
	input, err := console.NewStringInput(stripped)
	if err != nil {
		return nil, err
	}
	app.ResetComposer()

	return input, nil
}
