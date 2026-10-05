// Ports src/Composer/Command/FundCommand.php.

package command

import (
	"strings"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
)

func init() {
	registerCommand(OrderFund, func() console.Commander { return NewFundCommand() })
}

// FundCommand is Composer\Command\FundCommand.
type FundCommand struct{ *BaseCommand }

// NewFundCommand ports new FundCommand() (configure()).
func NewFundCommand() *FundCommand {
	c := &FundCommand{BaseCommand: NewBaseCommand("")}
	c.SetImpl(c)
	c.SetName("fund")
	c.SetDescription("Discover how to help fund the maintenance of your dependencies")
	c.SetDefinitionItems(
		optionWithSuggestions("format", "f", console.OptionValueRequired, "Format of the output: text or json", "text", "text", "json"),
	)

	return c
}

// ClassName implements console.ClassNamer.
func (*FundCommand) ClassName() string { return `Composer\Command\FundCommand` }

// Execute ports execute().
func (c *FundCommand) Execute(in console.Input, _ console.Output) (int, error) {
	comp, err := c.RequireComposer(nil, nil)
	if err != nil {
		return 0, err
	}

	repo := comp.RepositoryManager().LocalRepository()
	remoteRepos, err := repository.NewCompositeRepository(comp.RepositoryManager().Repositories())
	if err != nil {
		return 0, err
	}
	fundings := php.NewArray()

	installed, err := repo.Packages()
	if err != nil {
		return 0, err
	}
	packagesToLoad := &repository.ConstraintMap{}
	for _, p := range installed {
		if _, ok := p.(pkg.Alias); ok {
			continue
		}
		packagesToLoad.Set(p.Name(), semver.NewMatchAllConstraint())
	}

	// load all packages dev versions in parallel
	result, err := remoteRepos.LoadPackages(packagesToLoad, php.ArrayOf("dev", pkg.StabilityDev), php.NewArray(), nil)
	if err != nil {
		return 0, err
	}

	// collect funding data from default branches
	for _, p := range result.Packages {
		if _, ok := p.(pkg.Alias); ok {
			continue
		}
		cp, ok := p.(pkg.CompletePackageInterface)
		if ok && cp.IsDefaultBranch() && cp.Funding().Len() > 0 && packagesToLoad.Has(cp.Name()) {
			if err := insertFundingData(fundings, cp); err != nil {
				return 0, err
			}
			packagesToLoad.Delete(cp.Name())
		}
	}

	// collect funding from installed packages if none was found in the default branch above
	for _, p := range installed {
		if _, ok := p.(pkg.Alias); ok || !packagesToLoad.Has(p.Name()) {
			continue
		}

		if cp, ok := p.(pkg.CompletePackageInterface); ok && cp.Funding().Len() > 0 {
			if err := insertFundingData(fundings, cp); err != nil {
				return 0, err
			}
		}
	}

	php.Ksort(fundings, php.SortRegular)

	cio := c.IO()

	format := in.Option("format")
	if format != "text" && format != "json" {
		cio.WriteError(`Unsupported format "`+php.ToString(format)+`". See help for supported formats.`, true, io.Normal)

		return 1, nil
	}

	switch {
	case fundings.Len() > 0 && format == "text":
		prev := ""
		hasPrev := false

		cio.Write("The following packages were found in your dependencies which publish funding information:", true, io.Normal)

		for vendor, links := range fundings.All() {
			cio.Write("", true, io.Normal)
			cio.Write("<comment>"+vendor.String()+"</comment>", true, io.Normal)
			byURL, _ := links.(*php.Array)
			for url, packages := range byURL.All() {
				names, _ := packages.(*php.Array)
				line := "  <info>" + implodeComma(names) + "</info>"

				if !hasPrev || prev != line {
					cio.Write(line, true, io.Normal)
					prev, hasPrev = line, true
				}

				cio.Write("    <href="+console.Escape(url.String())+">"+url.String()+"</>", true, io.Normal)
			}
		}

		cio.Write("", true, io.Normal)
		cio.Write("Please consider following these links and sponsoring the work of package authors!", true, io.Normal)
		cio.Write("Thank you!", true, io.Normal)
	case format == "json":
		encoded, err := json.EncodeDefault(fundings)
		if err != nil {
			return 0, err
		}
		cio.Write(encoded, true, io.Normal)
	default:
		cio.Write("No funding links were found in your package dependencies. This doesn't mean they don't need your support!", true, io.Normal)
	}

	return 0, nil
}

var fundGithubURL = php.MustCompile(`{^https://github.com/([^/]+)$}`)

// insertFundingData ports FundCommand::insertFundingData (fundings is
// modified in place).
func insertFundingData(fundings *php.Array, p pkg.CompletePackageInterface) error {
	for _, option := range p.Funding().All() {
		fundingOption, _ := option.(*php.Array)
		if fundingOption == nil {
			continue
		}
		vendor, packageName, _ := strings.Cut(p.PrettyName(), "/")
		// ignore malformed funding entries
		urlValue, _ := fundingOption.Get("url")
		if !php.ToBool(urlValue) {
			continue
		}
		url := php.ToString(urlValue)
		if typ, _ := fundingOption.Get("type"); php.ToBool(typ) && typ == "github" {
			m, err := fundGithubURL.Match(url)
			if err != nil {
				return err
			}
			if m != nil {
				url = "https://github.com/sponsors/" + m.Get(1)
			}
		}
		byVendor, _ := fundings.GetArray(vendor)
		if byVendor == nil {
			byVendor = php.NewArray()
		}
		byURL, _ := byVendor.GetArray(url)
		if byURL == nil {
			byURL = php.NewArray()
		}
		byURL.Append(packageName)
		byVendor.Set(url, byURL)
		fundings.Set(vendor, byVendor)
	}

	return nil
}
