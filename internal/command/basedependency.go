// Ports src/Composer/Command/BaseDependencyCommand.php.

package command

import (
	"strings"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/ui"
)

// The argument and option names of the dependency commands.
const (
	ArgumentPackage    = "package"
	ArgumentConstraint = "version"
	OptionRecursive    = "recursive"
	OptionTree         = "tree"
)

// BaseDependencyCommand is Composer\Command\BaseDependencyCommand, the
// base of depends (why) and prohibits (why-not). The embedding command
// defines its arguments and options and calls DoExecute.
type BaseDependencyCommand struct {
	*BaseCommand

	// colors is $colors (set by initStyles).
	colors []string
}

// NewBaseDependencyCommand returns a BaseDependencyCommand named name.
func NewBaseDependencyCommand(name string) *BaseDependencyCommand {
	return &BaseDependencyCommand{BaseCommand: NewBaseCommand(name)}
}

// DoExecute ports doExecute: inverted is why-not behaviour.
func (c *BaseDependencyCommand) DoExecute(in console.Input, out console.Output, inverted bool) (int, error) {
	// Emit command event on startup
	composer, err := c.RequireComposer(nil, nil)
	if err != nil {
		return 0, err
	}
	commandEvent := eventdispatcher.NewCommandEvent(eventdispatcher.PluginCommand, c.Name(), in, out, nil, nil)
	if _, err := composer.EventDispatcher().Dispatch(commandEvent.Name(), commandEvent); err != nil {
		return 0, err
	}

	var repos []repository.RepositoryInterface

	root, _ := pkg.Clone(composer.Package()).(pkg.RootPackageInterface)
	rootRepo, err := repository.NewRootPackageRepository(root)
	if err != nil {
		return 0, err
	}
	repos = append(repos, rootRepo)

	platformOptions, err := composer.Runtime().PlatformOptions(composer.ProcessExecutor())
	if err != nil {
		return 0, err
	}

	if console.BoolOption(in, "locked") {
		locker := composer.Locker()

		locked, err := locker.IsLocked()
		if err != nil {
			return 0, err
		}
		if !locked {
			return 0, NewError(ClassUnexpectedValue, "A valid composer.lock file is required to run this command with --locked")
		}

		lockedRepo, err := locker.LockedRepository(true)
		if err != nil {
			return 0, err
		}
		overrides, err := locker.PlatformOverrides()
		if err != nil {
			return 0, err
		}
		platformRepo, err := repository.NewPlatformRepository(nil, overrides, platformOptions)
		if err != nil {
			return 0, err
		}
		repos = append(repos, lockedRepo, platformRepo)
	} else {
		localRepo := composer.RepositoryManager().LocalRepository()
		rootPkg := composer.Package()

		localPackages, err := localRepo.Packages()
		if err != nil {
			return 0, err
		}
		if len(localPackages) == 0 && (rootPkg.Requires().Len() > 0 || rootPkg.DevRequires().Len() > 0) {
			out.Writeln("<warning>No dependencies installed. Try running composer install or update, or use --locked.</warning>")

			return 1, nil
		}

		repos = append(repos, localRepo)

		platform, err := composer.Config().Get("platform", 0)
		if err != nil {
			return 0, err
		}
		overrides, _ := platform.(*php.Array)
		if !php.ToBool(platform) {
			overrides = php.NewArray()
		}
		platformRepo, err := repository.NewPlatformRepository(nil, overrides, platformOptions)
		if err != nil {
			return 0, err
		}
		repos = append(repos, platformRepo)
	}

	installedRepo, err := repository.NewInstalledRepository(repos)
	if err != nil {
		return 0, err
	}

	// Parse package name and constraint
	needle := console.StringArgument(in, ArgumentPackage)
	textConstraint := "*"
	if in.HasArgument(ArgumentConstraint) {
		textConstraint = console.StringArgument(in, ArgumentConstraint)
	}

	// Find packages that are or provide the requested package first
	packages, err := installedRepo.FindPackagesWithReplacersAndProviders(needle, nil)
	if err != nil {
		return 0, err
	}
	if len(packages) == 0 {
		return 0, NewError(ClassInvalidArgument, `Could not find package "`+needle+`" in your project`)
	}

	// If the version we ask for is not installed then we need to locate it in remote repos and add it.
	// This is needed for why-not to resolve conflicts from an uninstalled version against installed packages.
	findConstraint, err := repository.ParseConstraint(textConstraint)
	if err != nil {
		return 0, err
	}
	matchedPackage, err := installedRepo.FindPackage(needle, findConstraint)
	if err != nil {
		return 0, err
	}
	switch {
	case matchedPackage == nil:
		defaults, err := repository.DefaultRepos(c.IO(), composer.Config(), composer.RepositoryManager())
		if err != nil {
			return 0, err
		}
		var defaultList []repository.RepositoryInterface
		for _, r := range defaults.All() {
			defaultList = append(defaultList, r)
		}
		defaultRepos, err := repository.NewCompositeRepository(defaultList)
		if err != nil {
			return 0, err
		}
		match, err := defaultRepos.FindPackage(needle, findConstraint)
		if err != nil {
			return 0, err
		}
		switch {
		case match != nil:
			extra, err := repository.NewInstalledArrayRepository([]pkg.PackageInterface{pkg.Clone(match)})
			if err != nil {
				return 0, err
			}
			if err := installedRepo.AddRepository(extra); err != nil {
				return 0, err
			}
		case repository.IsPlatformPackage(needle):
			constraint, err := pkg.NewVersionParser().ParseConstraints(textConstraint)
			if err != nil {
				return 0, err
			}
			// `$constraint->getLowerBound() !== Bound::zero()` compares
			// object identity with a fresh instance: it is always true.
			lower := constraint.LowerBound().Version()
			extra, err := repository.NewInstalledArrayRepository([]pkg.PackageInterface{pkg.NewPackage(needle, lower, lower)})
			if err != nil {
				return 0, err
			}
			if err := installedRepo.AddRepository(extra); err != nil {
				return 0, err
			}
		default:
			c.IO().WriteError(`<error>Package "`+needle+`" could not be found with constraint "`+textConstraint+`", results below will most likely be incomplete.</error>`, true, io.Normal)
		}
	case repository.IsPlatformPackage(needle):
		extraNotice := ""
		if v, ok := matchedPackage.Extra().Get("config.platform"); ok && v == true {
			extraNotice = " (version provided by config.platform)"
		}
		c.IO().WriteError(`<info>Package "`+needle+` `+textConstraint+`" found in version "`+matchedPackage.PrettyVersion()+`"`+extraNotice+`.</info>`, true, io.Normal)
	case inverted:
		c.IO().Write(`<comment>Package "`+needle+`" `+matchedPackage.PrettyVersion()+` is already installed! To find out why, run `+"`composer why "+needle+"`"+`</comment>`, true, io.Normal)

		return 0, nil
	}

	// Include replaced packages for inverted lookups as they are then the actual starting point to consider
	needles := []string{needle}
	if inverted {
		for _, p := range packages {
			for link := range p.Replaces().Values() {
				needles = append(needles, link.Target())
			}
		}
	}

	// Parse constraint if one was supplied
	var constraint semver.ConstraintInterface
	if textConstraint != "*" {
		if constraint, err = pkg.NewVersionParser().ParseConstraints(textConstraint); err != nil {
			return 0, err
		}
	}

	// Parse rendering options
	renderTree := console.BoolOption(in, OptionTree)
	recursive := renderTree || console.BoolOption(in, OptionRecursive)

	ret := 0
	if inverted {
		ret = 1
	}

	// Resolve dependencies
	results, err := installedRepo.GetDependents(needles, constraint, inverted, recursive)
	if err != nil {
		return 0, err
	}
	switch {
	case len(results) == 0:
		extra := ""
		if constraint != nil {
			not := ""
			if inverted {
				not = "not "
			}
			extra = " in versions " + not + "matching " + textConstraint
		}
		c.IO().WriteError(`<info>There is no installed package depending on "`+needle+`"`+extra+`</info>`, true, io.Normal)
		if inverted {
			ret = 0
		} else {
			ret = 1
		}
	case renderTree:
		c.initStyles()
		root := packages[0]
		description := ""
		if cp, ok := root.(pkg.CompletePackageInterface); ok {
			description = cp.Description().S
		}
		c.IO().Write("<info>"+root.PrettyName()+"</info> "+root.PrettyVersion()+" "+description, true, io.Normal)
		if err := c.printTree(results, "", 1); err != nil {
			return 0, err
		}
	default:
		if err := c.printTable(out, results); err != nil {
			return 0, err
		}
	}

	if inverted && in.HasArgument(ArgumentConstraint) && !repository.IsPlatformPackage(needle) {
		composerCommand := "update"

		for link := range composer.Package().Requires().Values() {
			if link.Target() == needle {
				composerCommand = "require"

				break
			}
		}

		for link := range composer.Package().DevRequires().Values() {
			if link.Target() == needle {
				composerCommand = "require --dev"

				break
			}
		}

		c.IO().WriteError("Not finding what you were looking for? Try calling `composer "+composerCommand+` "`+needle+":"+textConstraint+`" --dry-run`+"` to get another view on the problem.", true, io.Normal)
	}

	return ret, nil
}

// nameWithLink is the package name, linked to its source or homepage.
func nameWithLink(p pkg.PackageInterface) string {
	if url := pkg.GetViewSourceOrHomepageURL(p); url.Valid {
		return "<href=" + console.Escape(url.S) + ">" + p.PrettyName() + "</>"
	}

	return p.PrettyName()
}

// printTable ports printTable: a bottom-up table of the dependencies.
func (c *BaseDependencyCommand) printTable(out console.Output, results []repository.Dependent) error {
	var table []any
	doubles := map[string]bool{}
	for {
		var queue []repository.Dependent
		var rows []any
		for _, result := range results {
			unique := result.Link.String()
			if doubles[unique] {
				continue
			}
			doubles[unique] = true
			version := result.Package.PrettyVersion()
			if version == pkg.DefaultPrettyVersion {
				version = "-"
			}
			prettyConstraint, err := result.Link.PrettyConstraint()
			if err != nil {
				return err
			}
			rows = append(rows, []any{nameWithLink(result.Package), version, result.Link.Description(), result.Link.Target() + " (" + prettyConstraint + ")"})
			if !result.Cut {
				queue = append(queue, result.Dependents...)
			}
		}
		results = queue
		table = append(rows, table...)
		if len(results) == 0 {
			break
		}
	}

	return c.RenderTable(table, out)
}

// initStyles ports initStyles.
func (c *BaseDependencyCommand) initStyles() {
	c.colors = ui.TreeTags()
}

// printTree ports printTree: a tree of results at level, each line
// prefixed with prefix.
func (c *BaseDependencyCommand) printTree(results []repository.Dependent, prefix string, level int) error {
	count := len(results)
	for idx, result := range results {
		color := c.colors[level%len(c.colors)]
		prevColor := c.colors[(level-1)%len(c.colors)]
		isLast := idx+1 == count
		versionText := result.Package.PrettyVersion()
		if versionText == pkg.DefaultPrettyVersion {
			versionText = ""
		}
		packageText := php.Rtrim("<" + color + ">" + nameWithLink(result.Package) + "</" + color + "> " + versionText)
		prettyConstraint, err := result.Link.PrettyConstraint()
		if err != nil {
			return err
		}
		linkText := result.Link.Description() + " <" + prevColor + ">" + result.Link.Target() + "</" + prevColor + "> " + prettyConstraint
		circularWarn := ""
		if result.Cut {
			circularWarn = "(circular dependency aborted here)"
		}
		branch := "├──"
		if isLast {
			branch = "└──"
		}
		c.writeTreeLine(php.Rtrim(prefix + branch + packageText + " (" + linkText + ") " + circularWarn))
		if !result.Cut {
			childPrefix := "│  "
			if isLast {
				childPrefix = "   "
			}
			if err := c.printTree(result.Dependents, prefix+childPrefix, level+1); err != nil {
				return err
			}
		}
	}

	return nil
}

var treeReplacer = strings.NewReplacer("└", "`-", "├", "|-", "──", "-", "│", "|")

// writeTreeLine ports writeTreeLine.
func (c *BaseDependencyCommand) writeTreeLine(line string) {
	out := c.IO()
	if !out.IsDecorated() {
		line = treeReplacer.Replace(line)
	}

	out.Write(line, true, io.Normal)
}
