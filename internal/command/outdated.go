// Ports src/Composer/Command/OutdatedCommand.php.

package command

import (
	"github.com/stubbedev/maestro/internal/console"
)

func init() {
	registerCommand(OrderOutdated, func() console.Commander { return NewOutdatedCommand() })
}

// OutdatedCommand is Composer\Command\OutdatedCommand.
type OutdatedCommand struct{ *BaseCommand }

// NewOutdatedCommand ports new OutdatedCommand() (configure()).
func NewOutdatedCommand() *OutdatedCommand {
	c := &OutdatedCommand{BaseCommand: NewBaseCommand("")}
	c.SetImpl(c)
	c.SetName("outdated")
	c.SetDescription("Shows a list of installed packages that have updates available, including their latest version")
	c.SetDefinitionItems(
		console.MustArgument("package", console.ArgumentOptional, "Package to inspect. Or a name including a wildcard (*) to filter lists of packages instead.", nil).WithSuggestFunc(c.SuggestInstalledPackage(false, false)),
		console.MustOption("outdated", "o", console.OptionValueNone, "Show only packages that are outdated (this is the default, but present here for compat with `show`", nil),
		console.MustOption("all", "a", console.OptionValueNone, "Show all installed packages with their latest versions", nil),
		console.MustOption("locked", "", console.OptionValueNone, "Shows updates for packages from the lock file, regardless of what is currently in vendor dir", nil),
		console.MustOption("direct", "D", console.OptionValueNone, "Shows only packages that are directly required by the root package", nil),
		console.MustOption("strict", "", console.OptionValueNone, "Return a non-zero exit code when there are outdated packages", nil),
		console.MustOption("major-only", "M", console.OptionValueNone, "Show only packages that have major SemVer-compatible updates.", nil),
		console.MustOption("minor-only", "m", console.OptionValueNone, "Show only packages that have minor SemVer-compatible updates.", nil),
		console.MustOption("patch-only", "p", console.OptionValueNone, "Show only packages that have patch SemVer-compatible updates.", nil),
		console.MustOption("sort-by-age", "A", console.OptionValueNone, "Displays the installed version's age, and sorts packages oldest first.", nil),
		optionWithSuggestions("format", "f", console.OptionValueRequired, "Format of the output: text or json", "text", "json", "text"),
		optionWithSuggestFunc("ignore", "", console.OptionValueRequired|console.OptionValueIsArray, "Ignore specified package(s). Can contain wildcards (*). Use it if you don't want to be informed about new versions of some packages.", nil, c.SuggestInstalledPackage(false, false)),
		console.MustOption("no-dev", "", console.OptionValueNone, "Disables search in require-dev packages.", nil),
		console.MustOption("ignore-platform-req", "", console.OptionValueRequired|console.OptionValueIsArray, "Ignore a specific platform requirement (php & ext- packages). Use with the --outdated option", nil),
		console.MustOption("ignore-platform-reqs", "", console.OptionValueNone, "Ignore all platform requirements (php & ext- packages). Use with the --outdated option", nil),
	)
	c.SetHelp("The outdated command is just a proxy for `composer show -l`" + `

The color coding (or signage if you have ANSI colors disabled) for dependency versions is as such:

- <info>green</info> (=): Dependency is in the latest version and is up to date.
- <comment>yellow</comment> (~): Dependency has a new version available that includes backwards
  compatibility breaks according to semver, so upgrade when you can but it
  may involve work.
- <highlight>red</highlight> (!): Dependency has a new version that is semver-compatible and you should upgrade it.

Read more at https://getcomposer.org/doc/03-cli.md#outdated`)

	return c
}

// ClassName implements console.ClassNamer.
func (*OutdatedCommand) ClassName() string { return `Composer\Command\OutdatedCommand` }

// IsProxyCommand ports isProxyCommand.
func (*OutdatedCommand) IsProxyCommand() bool { return true }

// Execute ports execute().
func (c *OutdatedCommand) Execute(in console.Input, out console.Output) (int, error) {
	args := []console.Param{
		console.P("command", "show"),
		console.P("--latest", true),
	}
	flag := func(option, arg string) {
		if console.BoolOption(in, option) {
			args = append(args, console.P(arg, true))
		}
	}
	flag("no-interaction", "--no-interaction")
	flag("no-plugins", "--no-plugins")
	flag("no-scripts", "--no-scripts")
	flag("no-cache", "--no-cache")
	if !console.BoolOption(in, "all") {
		args = append(args, console.P("--outdated", true))
	}
	flag("direct", "--direct")
	if p := in.Argument("package"); p != nil {
		args = append(args, console.P("package", p))
	}
	flag("strict", "--strict")
	flag("major-only", "--major-only")
	flag("minor-only", "--minor-only")
	flag("patch-only", "--patch-only")
	flag("locked", "--locked")
	flag("no-dev", "--no-dev")
	flag("sort-by-age", "--sort-by-age")
	args = append(args, console.P("--ignore-platform-req", in.Option("ignore-platform-req")))
	flag("ignore-platform-reqs", "--ignore-platform-reqs")
	args = append(args, console.P("--format", in.Option("format")))
	args = append(args, console.P("--ignore", in.Option("ignore")))

	sub, err := console.NewArrayInput(args, nil)
	if err != nil {
		return 0, err
	}
	app, err := c.App()
	if err != nil {
		return 0, err
	}

	return app.Run(sub, out)
}
