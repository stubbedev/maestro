// Ports src/Composer/Command/DependsCommand.php.

package command

import (
	"github.com/stubbedev/maestro/internal/console"
)

func init() {
	registerCommand(OrderDepends, func() console.Commander { return NewDependsCommand() })
}

// DependsCommand is Composer\Command\DependsCommand.
type DependsCommand struct{ *BaseDependencyCommand }

// NewDependsCommand ports new DependsCommand() (configure()).
func NewDependsCommand() *DependsCommand {
	c := &DependsCommand{BaseDependencyCommand: NewBaseDependencyCommand("")}
	c.SetImpl(c)
	c.SetName("depends")
	c.SetAliases("why")
	c.SetDescription("Shows which packages cause the given package to be installed")
	c.SetDefinitionItems(
		console.MustArgument(ArgumentPackage, console.ArgumentRequired, "Package to inspect", nil).WithSuggestFunc(c.SuggestInstalledPackage(true, true)),
		console.MustOption(OptionRecursive, "r", console.OptionValueNone, "Recursively resolves up to the root package", nil),
		console.MustOption(OptionTree, "t", console.OptionValueNone, "Prints the results as a nested tree", nil),
		console.MustOption("locked", "", console.OptionValueNone, "Read dependency information from composer.lock", nil),
	)
	c.SetHelp(`Displays detailed information about where a package is referenced.

<info>php composer.phar depends composer/composer</info>

Read more at https://getcomposer.org/doc/03-cli.md#depends-why`)

	return c
}

// ClassName implements console.ClassNamer.
func (*DependsCommand) ClassName() string { return `Composer\Command\DependsCommand` }

// Execute ports execute().
func (c *DependsCommand) Execute(in console.Input, out console.Output) (int, error) {
	code, err := c.DoExecute(in, out, false)

	return code, err
}
