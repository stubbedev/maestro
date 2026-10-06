// Ports src/Composer/Command/AuditCommand.php.

package command

import (
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/filterlist"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/policy"
	"github.com/stubbedev/maestro/internal/repository"
)

func init() {
	registerCommand(OrderAudit, func() console.Commander { return NewAuditCommand() })
}

// AuditCommand is Composer\Command\AuditCommand.
type AuditCommand struct{ *BaseCommand }

// NewAuditCommand ports new AuditCommand() (configure()).
func NewAuditCommand() *AuditCommand {
	c := &AuditCommand{BaseCommand: NewBaseCommand("")}
	c.SetImpl(c)
	c.SetName("audit")
	c.SetDescription("Checks for security vulnerability advisories for installed packages")
	c.SetDefinitionItems(
		console.MustOption("no-dev", "", console.OptionValueNone, "Disables auditing of require-dev packages.", nil),
		optionWithSuggestions("format", "f", console.OptionValueRequired, `Output format. Must be "table", "plain", "json", or "summary".`, advisory.FormatTable, advisory.Formats[:]...),
		console.MustOption("locked", "", console.OptionValueNone, "Audit based on the lock file instead of the installed packages.", nil),
		optionWithSuggestions("abandoned", "", console.OptionValueRequired, `Behavior on abandoned packages. Must be "ignore", "report", or "fail".`, nil, policy.Audits[:]...),
		optionWithSuggestions("ignore-severity", "", console.OptionValueIsArray|console.OptionValueRequired, "Ignore advisories of a certain severity level.", []any{}, "low", "medium", "high", "critical"),
		console.MustOption("ignore-unreachable", "", console.OptionValueNone, "Ignore repositories that are unreachable or return a non-200 status code.", nil),
	)
	c.SetHelp(`The <info>audit</info> command checks for security vulnerability advisories for installed packages.

If you do not want to include dev dependencies in the audit you can omit them with --no-dev

If you want to ignore repositories that are unreachable or return a non-200 status code, use --ignore-unreachable

Read more at https://getcomposer.org/doc/03-cli.md#audit`)

	return c
}

// ClassName implements console.ClassNamer.
func (*AuditCommand) ClassName() string { return `Composer\Command\AuditCommand` }

// Execute ports execute().
func (c *AuditCommand) Execute(in console.Input, _ console.Output) (int, error) {
	comp, err := c.RequireComposer(nil, nil)
	if err != nil {
		return 0, err
	}
	packages, err := c.getPackages(comp, in)
	if err != nil {
		return 0, err
	}

	if len(packages) == 0 {
		if comp.Package().Requires().Len() != 0 ||
			(!console.BoolOption(in, "no-dev") && comp.Package().DevRequires().Len() != 0) {
			c.IO().WriteError(`No installed packages found. Please run "composer install" before running "audit" or pass "--locked" to audit the lock file.`, true, io.Normal)

			return advisory.StatusFailed, nil
		}

		c.IO().WriteError("No packages - skipping audit.", true, io.Normal)

		return advisory.StatusOK, nil
	}

	auditor := advisory.Auditor{}
	repoSet, err := repository.NewRepositorySet("stable", nil, nil, nil, nil, nil)
	if err != nil {
		return 0, err
	}
	for _, repo := range comp.RepositoryManager().Repositories() {
		if err := repoSet.AddRepository(repo); err != nil {
			return 0, err
		}
	}

	abandoned, hasAbandoned := in.Option("abandoned").(string)
	if hasAbandoned && !slices.Contains(policy.Audits[:], abandoned) {
		return 0, NewError(ClassInvalidArgument, "--abandoned must be one of "+strings.Join(policy.Audits[:], ", ")+".")
	}

	policyConfig, err := c.CreatePolicyConfig(comp.Config(), in)
	if err != nil {
		return 0, err
	}
	if hasAbandoned {
		policyConfig = policyConfig.WithAudit(abandoned)
	}

	if ignoreSeverities := console.StringsOption(in, "ignore-severity"); len(ignoreSeverities) > 0 {
		policyConfig = policyConfig.WithIgnoreSeverity(ignoreSeverities)
	}
	if console.BoolOption(in, "ignore-unreachable") {
		if policyConfig, err = policyConfig.WithIgnoreUnreachable("audit"); err != nil {
			return 0, err
		}
	}

	var filterListProviderSet filterlist.ProviderSet
	if policyConfig.Enabled {
		set, err := filterlist.CreateFilterListProviderSet(policyConfig, comp.RepositoryManager().Repositories(), comp.Loop().HttpDownloader())
		if err != nil {
			return 0, err
		}
		filterListProviderSet = set
	}

	format, err := c.AuditFormat(in, "format")
	if err != nil {
		return 0, err
	}
	status, err := auditor.Audit(c.IO(), repoSet, policyConfig, packages, format, false, filterListProviderSet)
	if err != nil {
		return 0, err
	}

	return min(255, status), nil
}

// getPackages ports getPackages.
func (c *AuditCommand) getPackages(comp *composer.Composer, in console.Input) ([]pkg.PackageInterface, error) {
	if console.BoolOption(in, "locked") {
		locked, err := comp.Locker().IsLocked()
		if err != nil {
			return nil, err
		}
		if !locked {
			return nil, NewError(ClassUnexpectedValue, "Valid composer.json and composer.lock files are required to run this command with --locked")
		}
		repo, err := comp.Locker().LockedRepository(!console.BoolOption(in, "no-dev"))
		if err != nil {
			return nil, err
		}

		return repo.Packages()
	}

	rootPkg := comp.Package()
	installedRepo, err := repository.NewInstalledRepository([]repository.RepositoryInterface{comp.RepositoryManager().LocalRepository()})
	if err != nil {
		return nil, err
	}
	packages, err := installedRepo.Packages()
	if err != nil {
		return nil, err
	}

	if console.BoolOption(in, "no-dev") {
		return repository.FilterRequiredPackages(packages, rootPkg, false), nil
	}

	return packages, nil
}
