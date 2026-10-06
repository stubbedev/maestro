// Ports src/Composer/Command/ScriptAliasCommand.php.

package command

import (
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
)

const scriptAliasCommandFile = "ScriptAliasCommand.php"

func init() {
	registerScriptAliasCommand(func(script string, description, aliases any) (console.Commander, error) {
		return NewScriptAliasCommand(script, description, aliases)
	})
}

// ScriptAliasCommand is Composer\Command\ScriptAliasCommand.
type ScriptAliasCommand struct {
	*BaseCommand

	script string
}

// NewScriptAliasCommand ports new ScriptAliasCommand($script, $description,
// $aliases): description is a ?string and aliases an array (anything else
// is the TypeError PHP's strict types raise); the aliases must all be
// strings.
func NewScriptAliasCommand(script string, description, aliases any) (cmd *ScriptAliasCommand, err error) {
	const ctor = `Composer\Command\ScriptAliasCommand::__construct`
	var desc string
	switch d := description.(type) {
	case nil:
		desc = "Runs the " + script + " script as defined in composer.json"
	case string:
		desc = d
	default:
		return nil, pkg.ArgumentTypeError(ctor, 2, "description", "?string", description).
			Called(`Composer\Command\ScriptAliasCommand->__construct`, phperr.At("ScriptAliasCommand.php", 37), "src/Composer/Console/Application.php", 428)
	}
	aliasArray, ok := aliases.(*php.Array)
	if !ok {
		return nil, pkg.ArgumentTypeError(ctor, 3, "aliases", "array", aliases).
			Called(`Composer\Command\ScriptAliasCommand->__construct`, phperr.At("ScriptAliasCommand.php", 37), "src/Composer/Console/Application.php", 428)
	}
	aliasList := make([]string, 0, aliasArray.Len())
	for _, alias := range aliasArray.Values() {
		s, ok := alias.(string)
		if !ok {
			return nil, NewError(ClassInvalidArgument, scriptAliasCommandFile, 45, `"scripts-aliases" element array values should contain only strings`)
		}
		aliasList = append(aliasList, s)
	}

	c := &ScriptAliasCommand{BaseCommand: NewBaseCommand(""), script: script}
	c.SetImpl(c)
	c.IgnoreValidationErrors()

	// setName/setAliases throw an InvalidArgumentException for an invalid
	// command name.
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(error)
			if !ok {
				panic(r)
			}
			cmd, err = nil, e
		}
	}()

	c.SetName(script)
	c.SetDescription(desc)
	c.SetAliases(aliasList...)
	c.SetDefinitionItems(
		console.MustOption("dev", "", console.OptionValueNone, "Sets the dev mode.", nil),
		console.MustOption("no-dev", "", console.OptionValueNone, "Disables the dev mode.", nil),
		console.MustArgument("args", console.ArgumentIsArray|console.ArgumentOptional, "", nil),
	)
	c.SetHelp(`The <info>run-script</info> command runs scripts defined in composer.json:

<info>php composer.phar run-script post-update-cmd</info>

Read more at https://getcomposer.org/doc/03-cli.md#run-script-run`)

	return c, nil
}

// ClassName implements console.ClassNamer.
func (*ScriptAliasCommand) ClassName() string { return `Composer\Command\ScriptAliasCommand` }

// Execute ports execute().
func (c *ScriptAliasCommand) Execute(in console.Input, _ console.Output) (int, error) {
	cmp, err := c.requireComposerAt("ScriptAliasCommand.php", 79)
	if err != nil {
		return 0, err
	}

	args := console.StringsArgument(in, "args")

	devMode := console.BoolOption(in, "dev") || !console.BoolOption(in, "no-dev")

	util.PutEnv("COMPOSER_DEV_MODE", devModeEnv(devMode))

	// Removing the first token (the command name) cannot fail: \S+ at the
	// anchored start does bounded work.
	scriptAliasInput, _, _ := php.PregReplace(`{^\S+ ?}`, "", in.String(), 1)
	flags := php.NewArray()
	flags.Set("script-alias-input", scriptAliasInput)

	return cmp.EventDispatcher().DispatchScript(c.script, devMode, args, flags)
}
