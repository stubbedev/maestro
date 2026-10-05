// Ports src/Composer/Command/AboutCommand.php.

package command

import (
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
)

func init() {
	registerCommand(OrderAbout, func() console.Commander { return NewAboutCommand() })
}

// AboutCommand is Composer\Command\AboutCommand.
type AboutCommand struct{ *BaseCommand }

// NewAboutCommand ports new AboutCommand().
func NewAboutCommand() *AboutCommand {
	c := &AboutCommand{BaseCommand: NewBaseCommand("")}
	c.SetImpl(c)
	c.SetName("about").
		SetDescription("Shows a short information about Composer").
		SetHelp("<info>php composer.phar about</info>")

	return c
}

// ClassName implements console.ClassNamer.
func (*AboutCommand) ClassName() string { return `Composer\Command\AboutCommand` }

// Execute implements console.Executor.
func (c *AboutCommand) Execute(console.Input, console.Output) (int, error) {
	composerVersion := composer.GetVersion()

	c.IO().Write(`<info>Composer - Dependency Manager for PHP - version `+composerVersion+`</info>
<comment>Composer is a dependency manager tracking local dependencies of your projects and libraries.
See https://getcomposer.org/ for more information.</comment>`, true, io.Normal)

	return 0, nil
}
