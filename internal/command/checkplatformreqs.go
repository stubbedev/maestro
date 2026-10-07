// Ports src/Composer/Command/CheckPlatformReqsCommand.php.

package command

import (
	"slices"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
)

func init() {
	registerCommand(OrderCheckPlatformReqs, func() console.Commander { return NewCheckPlatformReqsCommand() })
}

// CheckPlatformReqsCommand is Composer\Command\CheckPlatformReqsCommand.
type CheckPlatformReqsCommand struct{ *BaseCommand }

// NewCheckPlatformReqsCommand ports new CheckPlatformReqsCommand()
// (configure()).
func NewCheckPlatformReqsCommand() *CheckPlatformReqsCommand {
	c := &CheckPlatformReqsCommand{BaseCommand: NewBaseCommand("")}
	c.SetImpl(c)
	c.SetName("check-platform-reqs")
	c.SetDescription("Check that platform requirements are satisfied")
	c.SetDefinitionItems(
		console.MustOption("no-dev", "", console.OptionValueNone, "Disables checking of require-dev packages requirements.", nil),
		console.MustOption("lock", "", console.OptionValueNone, "Checks requirements only from the lock file, not from installed packages.", nil),
		optionWithSuggestions("format", "f", console.OptionValueRequired, "Format of the output: text or json", "text", "json", "text"),
	)
	c.SetHelp(`Checks that your PHP and extensions versions match the platform requirements of the installed packages.

Unlike update/install, this command will ignore config.platform settings and check the real platform packages so you can be certain you have the required platform dependencies.

<info>php composer.phar check-platform-reqs</info>
`)

	return c
}

// PHPClass implements php.Classer.
func (*CheckPlatformReqsCommand) PHPClass() string {
	return `Composer\Command\CheckPlatformReqsCommand`
}

// platformReqResult is a row of $results: [name, version, link|null,
// status, provider].
type platformReqResult struct {
	platformPackage string
	version         string
	link            *pkg.Link
	status          string
	provider        string
}

// Execute ports execute().
func (c *CheckPlatformReqsCommand) Execute(in console.Input, out console.Output) (int, error) {
	comp, err := c.RequireComposer(nil, nil)
	if err != nil {
		return 0, err
	}

	noDev := console.BoolOption(in, "no-dev")
	nonDev := ""
	if noDev {
		nonDev = "non-dev "
	}

	// name => index into requireLinks ($requires: name => list of links;
	// a *php.Array holds only PHP values)
	requires := php.NewArray()
	var requireLinks [][]*pkg.Link
	addLink := func(name string, link *pkg.Link) {
		if i, ok := requires.Get(name); ok {
			idx, _ := i.(int64)
			requireLinks[idx] = append(requireLinks[idx], link)

			return
		}
		requires.Set(name, int64(len(requireLinks)))
		requireLinks = append(requireLinks, []*pkg.Link{link})
	}
	var removePackages []string
	var installedRepo repository.RepositoryInterface
	if console.BoolOption(in, "lock") {
		c.IO().WriteError("<info>Checking "+nonDev+"platform requirements using the lock file</info>", true, io.Normal)
		if installedRepo, err = comp.Locker().LockedRepository(!noDev); err != nil {
			return 0, err
		}
	} else {
		localRepo := comp.RepositoryManager().LocalRepository()
		packages, err := localRepo.Packages()
		if err != nil {
			return 0, err
		}
		// fallback to lockfile if installed repo is empty
		if len(packages) == 0 {
			c.IO().WriteError("<warning>No vendor dir present, checking "+nonDev+"platform requirements from the lock file</warning>", true, io.Normal)
			if installedRepo, err = comp.Locker().LockedRepository(!noDev); err != nil {
				return 0, err
			}
		} else {
			if noDev {
				removePackages = localRepo.DevPackageNames()
			}

			c.IO().WriteError("<info>Checking "+nonDev+"platform requirements for packages in the vendor dir</info>", true, io.Normal)
			installedRepo = localRepo
		}
	}
	if !noDev {
		for require, link := range comp.Package().DevRequires().All() {
			addLink(require, link)
		}
	}

	root, _ := pkg.Clone(comp.Package()).(pkg.RootPackageInterface)
	rootRepo, err := repository.NewRootPackageRepository(root)
	if err != nil {
		return 0, err
	}
	installed, err := repository.NewInstalledRepository([]repository.RepositoryInterface{installedRepo, rootRepo})
	if err != nil {
		return 0, err
	}
	packages, err := installed.Packages()
	if err != nil {
		return 0, err
	}
	for _, p := range packages {
		if slices.Contains(removePackages, p.Name()) {
			continue
		}
		for require, link := range p.Requires().All() {
			addLink(require, link)
		}
	}

	php.Ksort(requires, 0)

	platformRepo, err := c.newPlatformRepository(nil)
	if err != nil {
		return 0, err
	}
	if err := installed.AddRepository(platformRepo); err != nil {
		return 0, err
	}

	var results []platformReqResult
	exitCode := 0

	for key, v := range requires.All() {
		require := key.String()
		idx, _ := v.(int64)
		links := requireLinks[idx]
		if !repository.IsPlatformPackage(require) {
			continue
		}
		candidates, err := installed.FindPackagesWithReplacersAndProviders(require, nil)
		if err != nil {
			return 0, err
		}
		if len(candidates) == 0 {
			results = append(results, platformReqResult{require, "n/a", links[0], "<error>missing</error>", ""})

			exitCode = max(exitCode, 2)

			continue
		}

		var reqResults []platformReqResult
		matched := false
	candidates:
		for _, candidate := range candidates {
			var candidateConstraint semver.ConstraintInterface
			if candidate.Name() == require {
				constraint := semver.NewConstraintOp(semver.OpEQ, candidate.Version())
				constraint.SetPrettyString(candidate.PrettyVersion())
				candidateConstraint = constraint
			} else {
			links:
				for _, ls := range []pkg.Links{candidate.Provides(), candidate.Replaces()} {
					for link := range ls.Values() {
						if link.Target() == require {
							candidateConstraint = link.Constraint()

							break links
						}
					}
				}
			}

			// safety check for phpstan, but it should not be possible to get a candidate out of findPackagesWithReplacersAndProviders without a constraint matching $require
			if candidateConstraint == nil {
				continue
			}

			name, provider := require, "<comment>provided by "+candidate.PrettyName()+"</comment>"
			if candidate.Name() == require {
				name, provider = candidate.PrettyName(), ""
			}

			for _, link := range links {
				if !link.Constraint().Matches(candidateConstraint) {
					reqResults = append(reqResults, platformReqResult{name, candidateConstraint.PrettyString(), link, "<error>failed</error>", provider})

					// skip to next candidate
					continue candidates
				}
			}

			results = append(results, platformReqResult{name, candidateConstraint.PrettyString(), nil, "<info>success</info>", provider})

			// candidate matched, skip to next requirement
			matched = true

			break
		}
		if matched {
			continue
		}

		// show the first error from every failed candidate
		results = append(results, reqResults...)
		exitCode = max(exitCode, 1)
	}

	if err := c.printTable(out, results, console.StringOption(in, "format")); err != nil {
		return 0, err
	}

	return exitCode, nil
}

// printTable ports printTable.
func (c *CheckPlatformReqsCommand) printTable(out console.Output, results []platformReqResult, format string) error {
	if format == "json" {
		rows := php.NewArray()
		for _, r := range results {
			var failed any
			if r.link != nil {
				constraint, err := r.link.PrettyConstraint()
				if err != nil {
					return err
				}
				failed = php.ArrayOf(
					"source", r.link.Source(),
					"type", r.link.Description(),
					"target", r.link.Target(),
					"constraint", constraint,
				)
			}
			var provider any
			if r.provider != "" {
				provider = php.StripTags(r.provider)
			}
			rows.Append(php.ArrayOf(
				"name", r.platformPackage,
				"version", r.version,
				"status", php.StripTags(r.status),
				"failed_requirement", failed,
				"provider", provider,
			))
		}

		encoded, err := json.EncodeDefault(rows)
		if err != nil {
			return err
		}
		c.IO().Write(encoded, true, io.Normal)

		return nil
	}

	rows := make([]any, 0, len(results))
	for _, r := range results {
		var link any
		description := ""
		if r.link != nil {
			link = r.link
			constraint, err := r.link.PrettyConstraint()
			if err != nil {
				return err
			}
			description = r.link.Source() + " " + r.link.Description() + " " + r.link.Target() + " (" + constraint + ")"
		}
		rows = append(rows, []any{
			r.platformPackage,
			r.version,
			link,
			description,
			php.Rtrim(r.status + " " + r.provider),
		})
	}

	return c.RenderTable(rows, out)
}
