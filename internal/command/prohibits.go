// Ports src/Composer/Command/ProhibitsCommand.php.

package command

import (
	"github.com/stubbedev/maestro/internal/console"
)

func init() {
	registerCommand(OrderProhibits, func() console.Commander { return NewProhibitsCommand() })
}

// ProhibitsCommand is Composer\Command\ProhibitsCommand.
type ProhibitsCommand struct{ *BaseDependencyCommand }

// NewProhibitsCommand ports new ProhibitsCommand() (configure()).
func NewProhibitsCommand() *ProhibitsCommand {
	c := &ProhibitsCommand{BaseDependencyCommand: NewBaseDependencyCommand("")}
	c.SetImpl(c)
	c.SetName("prohibits")
	c.SetAliases("why-not")
	c.SetDescription("Shows which packages prevent the given package from being installed")
	c.SetDefinitionItems(
		console.MustArgument(ArgumentPackage, console.ArgumentRequired, "Package to inspect", nil).WithSuggestFunc(c.SuggestAvailablePackage(99)),
		console.MustArgument(ArgumentConstraint, console.ArgumentRequired, "Version constraint, which version you expected to be installed", nil),
		console.MustOption(OptionRecursive, "r", console.OptionValueNone, "Recursively resolves up to the root package", nil),
		console.MustOption(OptionTree, "t", console.OptionValueNone, "Prints the results as a nested tree", nil),
		console.MustOption("locked", "", console.OptionValueNone, "Read dependency information from composer.lock", nil),
	)
	c.SetHelp(`Displays detailed information about why a package cannot be installed.

<info>php composer.phar prohibits composer/composer</info>

Read more at https://getcomposer.org/doc/03-cli.md#prohibits-why-not`)

	return c
}

// PHPClass implements php.Classer.
func (*ProhibitsCommand) PHPClass() string { return `Composer\Command\ProhibitsCommand` }

// Execute ports execute().
func (c *ProhibitsCommand) Execute(in console.Input, out console.Output) (int, error) {
	code, err := c.DoExecute(in, out, true)

	return code, err
}
