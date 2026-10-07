// Ports src/Composer/Command/LicensesCommand.php.

package command

import (
	"strings"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/spdx"
	"github.com/stubbedev/maestro/internal/ui"
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
		formatOption("Format of the output: text, json or summary", "text", licensesFormats),
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

// PHPClass implements php.Classer.
func (*LicensesCommand) PHPClass() string { return `Composer\Command\LicensesCommand` }

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

// styleLicenses is the licenses' identifiers joined by ", ", or "none"
// (implodeComma's): on the free surface an OSI-approved licence in the
// success role, any other in the notice role and "none" in the danger
// role.
func styleLicenses(surface ui.Surface, spdxLicenses *spdx.SpdxLicenses, licenses *php.Array) string {
	if licenses == nil || licenses.Len() == 0 {
		return surface.Style(ui.RoleDanger, "none")
	}
	parts := make([]string, 0, licenses.Len())
	for _, v := range licenses.All() {
		id := php.ToString(v)
		role := ui.RoleNotice
		if spdxLicenses.IsOsiApprovedByIdentifier(id) {
			role = ui.RoleSuccess
		}
		parts = append(parts, surface.Style(role, id))
	}

	return strings.Join(parts, ", ")
}

// Execute ports execute().
func (c *LicensesCommand) Execute(in console.Input, out console.Output) (int, error) {
	comp, err := c.RequireComposer(nil, nil)
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
			return 0, NewError(ClassUnexpectedValue, "Valid composer.json and composer.lock files are required to run this command with --locked")
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

	format := in.Option("format")
	surface := licensesFormats.Surface(format)
	switch format {
	case "text":
		spdxLicenses := spdx.New()
		label := func(s string) string { return surface.Style(ui.RoleMuted, s) }
		cio.Write(label("Name:")+" <comment>"+root.PrettyName()+"</comment>", true, io.Normal)
		cio.Write(label("Version:")+" <comment>"+root.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev)+"</comment>", true, io.Normal)
		// the root's licences keep Composer's <comment> look
		cio.Write(label("Licenses:")+" <comment>"+styleLicenses(ui.Frozen, spdxLicenses, root.License())+"</comment>", true, io.Normal)
		cio.Write(label("Dependencies:"), true, io.Normal)
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

			if err := table.AddRow([]any{
				name,
				p.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev),
				styleLicenses(surface, spdxLicenses, licensesOf(p)),
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
		return 0, NewError(ClassRuntime, `Unsupported format "`+php.ToString(format)+`".  See help for supported formats.`)
	}

	return 0, nil
}
