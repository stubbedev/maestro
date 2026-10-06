// Ports src/Composer/Command/LicensesCommand.php.

package command

import (
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
)

func init() {
	registerCommand(OrderLicenses, func() console.Commander { return NewLicensesCommand() })
}

// LicensesCommand is Composer\Command\LicensesCommand.
type LicensesCommand struct{ *BaseCommand }

// NewLicensesCommand ports new LicensesCommand() (configure()).
func NewLicensesCommand() *LicensesCommand {
	c := &LicensesCommand{BaseCommand: NewBaseCommand("")}
	c.SetImpl(c)
	c.SetName("licenses")
	c.SetDescription("Shows information about licenses of dependencies")
	c.SetDefinitionItems(
		optionWithSuggestions("format", "f", console.OptionValueRequired, "Format of the output: text, json or summary", "text", "text", "json", "summary"),
		console.MustOption("no-dev", "", console.OptionValueNone, "Disables search in require-dev packages.", nil),
		console.MustOption("locked", "", console.OptionValueNone, "Shows licenses from the lock file instead of installed packages.", nil),
	)
	c.SetHelp(`The license command displays detailed information about the licenses of
the installed dependencies.

Use --locked to show licenses from composer.lock instead of what's currently
installed in the vendor directory.

Read more at https://getcomposer.org/doc/03-cli.md#licenses`)

	return c
}

// ClassName implements console.ClassNamer.
func (*LicensesCommand) ClassName() string { return `Composer\Command\LicensesCommand` }

// licensesOf is `$package instanceof CompletePackageInterface ?
// $package->getLicense() : []`.
func licensesOf(p pkg.PackageInterface) *php.Array {
	if cp, ok := p.(pkg.CompletePackageInterface); ok {
		if l := cp.License(); l != nil {
			return l
		}
	}

	return php.NewArray()
}

// Execute ports execute().
func (c *LicensesCommand) Execute(in console.Input, out console.Output) (int, error) {
	comp, err := c.requireComposerAt("LicensesCommand.php", 60)
	if err != nil {
		return 0, err
	}

	commandEvent := eventdispatcher.NewCommandEvent(eventdispatcher.PluginCommand, "licenses", in, out, nil, nil)
	if _, err := comp.EventDispatcher().Dispatch(commandEvent.Name(), commandEvent); err != nil {
		return 0, err
	}

	root := comp.Package()

	var packages []pkg.PackageInterface
	if console.BoolOption(in, "locked") {
		locked, err := comp.Locker().IsLocked()
		if err != nil {
			return 0, err
		}
		if !locked {
			return 0, NewError(ClassUnexpectedValue, "LicensesCommand.php", 69, "Valid composer.json and composer.lock files are required to run this command with --locked")
		}
		repo, err := comp.Locker().LockedRepository(!console.BoolOption(in, "no-dev"))
		if err != nil {
			return 0, err
		}
		if packages, err = repo.Packages(); err != nil {
			return 0, err
		}
	} else {
		repo := comp.RepositoryManager().LocalRepository()
		if packages, err = repo.Packages(); err != nil {
			return 0, err
		}
		if console.BoolOption(in, "no-dev") {
			packages = repository.FilterRequiredPackages(packages, root, false)
		}
	}

	packages = pkg.SortPackagesAlphabetically(packages)
	cio := c.IO()

	switch format := in.Option("format"); format {
	case "text":
		rootLicenses := implodeComma(root.License())
		if rootLicenses == "" {
			rootLicenses = "none"
		}
		cio.Write("Name: <comment>"+root.PrettyName()+"</comment>", true, io.Normal)
		cio.Write("Version: <comment>"+root.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev)+"</comment>", true, io.Normal)
		cio.Write("Licenses: <comment>"+rootLicenses+"</comment>", true, io.Normal)
		cio.Write("Dependencies:", true, io.Normal)
		cio.Write("", true, io.Normal)

		table := console.NewTable(out)
		if err := table.SetStyle("compact"); err != nil {
			return 0, err
		}
		table.SetHeaders([]any{"Name", "Version", "Licenses"})
		for _, p := range packages {
			name := p.PrettyName()
			if link := pkg.GetViewSourceOrHomepageURL(p); link.Valid {
				name = "<href=" + console.Escape(link.S) + ">" + p.PrettyName() + "</>"
			}

			licenses := implodeComma(licensesOf(p))
			if licenses == "" {
				licenses = "none"
			}
			if err := table.AddRow([]any{
				name,
				p.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev),
				licenses,
			}); err != nil {
				return 0, err
			}
		}
		if err := table.Render(); err != nil {
			return 0, err
		}

	case "json":
		dependencies := php.NewArray()
		for _, p := range packages {
			dependencies.Set(p.PrettyName(), php.ArrayOf(
				"version", p.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev),
				"license", licensesOf(p),
			))
		}

		rootLicense := root.License()
		if rootLicense == nil {
			rootLicense = php.NewArray()
		}
		encoded, err := json.EncodeDefault(php.ArrayOf(
			"name", root.PrettyName(),
			"version", root.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev),
			"license", rootLicense,
			"dependencies", dependencies,
		))
		if err != nil {
			return 0, err
		}
		cio.Write(encoded, true, io.Normal)

	case "summary":
		usedLicenses := php.NewArray()
		for _, p := range packages {
			licenses := licensesOf(p).Values()
			if len(licenses) == 0 {
				licenses = append(licenses, "none")
			}
			for _, licenseName := range licenses {
				key := php.ToKey(licenseName)
				n, _ := usedLicenses.GetKey(key)
				count, _ := n.(int64)
				usedLicenses.SetKey(key, count+1)
			}
		}

		// Sort licenses so that the most used license will appear first
		php.Arsort(usedLicenses, php.SortNumeric)

		rows := make([]any, 0, usedLicenses.Len())
		for usedLicense, numberOfDependencies := range usedLicenses.All() {
			rows = append(rows, []any{usedLicense.Value(), numberOfDependencies})
		}

		symfonyIo := console.NewSymfonyStyle(in, out)
		if err := symfonyIo.Table([]any{"License", "Number of dependencies"}, rows); err != nil {
			return 0, err
		}

	default:
		return 0, NewError(ClassRuntime, "LicensesCommand.php", 162, `Unsupported format "`+php.ToString(format)+`".  See help for supported formats.`)
	}

	return 0, nil
}
