// Ports src/Composer/Command/ShowCommand.php.

package command

import (
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/repository/composerrepo"
	"github.com/stubbedev/maestro/internal/resolver"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/spdx"
)

func init() {
	registerCommand(OrderShow, func() console.Commander { return NewShowCommand() })
}

// ShowCommand is Composer\Command\ShowCommand.
type ShowCommand struct {
	*BaseCommand

	versionParser *pkg.VersionParser
	colors        []string
	repositorySet *repository.RepositorySet
}

// NewShowCommand creates the show command.
func NewShowCommand() *ShowCommand {
	c := &ShowCommand{BaseCommand: NewBaseCommand("")}
	c.SetImpl(c)
	c.SetName("show")
	c.SetAliases("info")
	c.SetDescription("Shows information about packages")
	c.SetDefinitionItems(
		console.MustArgument("package", console.ArgumentOptional, "Package to inspect. Or a name including a wildcard (*) to filter lists of packages instead.", nil).WithSuggestFunc(c.suggestPackageBasedOnMode()),
		console.MustArgument("version", console.ArgumentOptional, "Version or version constraint to inspect", nil),
		console.MustOption("all", "", console.OptionValueNone, "List all packages", nil),
		console.MustOption("locked", "", console.OptionValueNone, "List all locked packages", nil),
		console.MustOption("installed", "i", console.OptionValueNone, "List installed packages only (enabled by default, only present for BC).", nil),
		console.MustOption("platform", "p", console.OptionValueNone, "List platform packages only", nil),
		console.MustOption("available", "a", console.OptionValueNone, "List available packages only", nil),
		console.MustOption("self", "s", console.OptionValueNone, "Show the root package information", nil),
		console.MustOption("name-only", "N", console.OptionValueNone, "List package names only", nil),
		console.MustOption("path", "P", console.OptionValueNone, "Show package paths", nil),
		console.MustOption("tree", "t", console.OptionValueNone, "List the dependencies as a tree", nil),
		console.MustOption("latest", "l", console.OptionValueNone, "Show the latest version", nil),
		console.MustOption("outdated", "o", console.OptionValueNone, "Show the latest version but only for packages that are outdated", nil),
		optionWithSuggestFunc("ignore", "", console.OptionValueRequired|console.OptionValueIsArray, "Ignore specified package(s). Can contain wildcards (*). Use it with the --outdated option if you don't want to be informed about new versions of some packages.", nil, c.SuggestInstalledPackage(false, false)),
		console.MustOption("major-only", "M", console.OptionValueNone, "Show only packages that have major SemVer-compatible updates. Use with the --latest or --outdated option.", nil),
		console.MustOption("minor-only", "m", console.OptionValueNone, "Show only packages that have minor SemVer-compatible updates. Use with the --latest or --outdated option.", nil),
		console.MustOption("patch-only", "", console.OptionValueNone, "Show only packages that have patch SemVer-compatible updates. Use with the --latest or --outdated option.", nil),
		console.MustOption("sort-by-age", "A", console.OptionValueNone, "Displays the installed version's age, and sorts packages oldest first. Use with the --latest or --outdated option.", nil),
		console.MustOption("direct", "D", console.OptionValueNone, "Shows only packages that are directly required by the root package", nil),
		console.MustOption("strict", "", console.OptionValueNone, "Return a non-zero exit code when there are outdated packages", nil),
		optionWithSuggestions("format", "f", console.OptionValueRequired, "Format of the output: text or json", "text", "json", "text"),
		console.MustOption("no-dev", "", console.OptionValueNone, "Disables search in require-dev packages.", nil),
		console.MustOption("ignore-platform-req", "", console.OptionValueRequired|console.OptionValueIsArray, "Ignore a specific platform requirement (php & ext- packages). Use with the --outdated option", nil),
		console.MustOption("ignore-platform-reqs", "", console.OptionValueNone, "Ignore all platform requirements (php & ext- packages). Use with the --outdated option", nil),
	)
	c.SetHelp(`The show command displays detailed information about a package, or
lists all packages available.

Read more at https://getcomposer.org/doc/03-cli.md#show-info`)

	return c
}

// ClassName implements console.ClassNamer.
func (*ShowCommand) ClassName() string { return `Composer\Command\ShowCommand` }

func (c *ShowCommand) suggestPackageBasedOnMode() console.SuggestFunc {
	return func(input *console.CompletionInput, suggestions *console.CompletionSuggestions) []console.Suggestion {
		if console.BoolOption(input, "available") || console.BoolOption(input, "all") {
			return c.SuggestAvailablePackageInclPlatform()(input, suggestions)
		}
		if console.BoolOption(input, "platform") {
			return c.SuggestPlatformPackage()(input, suggestions)
		}

		return c.SuggestInstalledPackage(false, false)(input, suggestions)
	}
}

// showEntry is a value of $packages[$type]: a package, or a bare name
// (ComposerRepository::getPackageNames).
type showEntry struct {
	pkg  pkg.PackageInterface
	name string
}

// showList is $packages[$type]: name => entry, in PHP array order.
type showList struct {
	order   *php.Array
	entries map[string]showEntry
}

func newShowList() *showList {
	return &showList{order: php.NewArray(), entries: map[string]showEntry{}}
}

func (l *showList) get(name string) (showEntry, bool) {
	e, ok := l.entries[name]

	return e, ok
}

func (l *showList) set(name string, e showEntry) {
	l.order.Set(name, true)
	l.entries[name] = e
}

func (l *showList) ksort() { php.Ksort(l.order, 0) }

func (l *showList) values() []showEntry {
	out := make([]showEntry, 0, l.order.Len())
	for k := range l.order.All() {
		out = append(out, l.entries[k.String()])
	}

	return out
}

// showLists is $packages: type => showList. As in PHP, a type exists only
// once an entry is set in it, so a repository that contributes nothing
// leaves no (empty) type behind.
type showLists map[string]*showList

// get reads $packages[$type][$name], absent when the type is.
func (ls showLists) get(typ, name string) (showEntry, bool) {
	if l := ls[typ]; l != nil {
		return l.get(name)
	}

	return showEntry{}, false
}

// set is $packages[$type][$name] = $entry, creating the type on its first
// entry.
func (ls showLists) set(typ, name string, e showEntry) {
	if ls[typ] == nil {
		ls[typ] = newShowList()
	}
	ls[typ].set(name, e)
}

// Execute ports ShowCommand::execute.
func (c *ShowCommand) Execute(input console.Input, output console.Output) (int, error) {
	c.versionParser = pkg.NewVersionParser()
	if console.BoolOption(input, "tree") {
		if err := c.initStyles(output); err != nil {
			return 0, err
		}
	}

	comp, err := c.TryComposer(nil, nil)
	if err != nil {
		return 0, err
	}
	io := c.IO()

	if console.BoolOption(input, "installed") && !console.BoolOption(input, "self") {
		io.WriteError(`<warning>You are using the deprecated option "installed". Only installed packages are shown by default now. The --all option can be used to show all packages.</warning>`, true, mio.Normal)
	}

	if console.BoolOption(input, "outdated") {
		input.SetOption("latest", true)
	} else if len(console.StringsOption(input, "ignore")) > 0 {
		io.WriteError(`<warning>You are using the option "ignore" for action other than "outdated", it will be ignored.</warning>`, true, mio.Normal)
	}

	if console.BoolOption(input, "direct") && (console.BoolOption(input, "all") || console.BoolOption(input, "available") || console.BoolOption(input, "platform")) {
		io.WriteError("The --direct (-D) option is not usable in combination with --all, --platform (-p) or --available (-a)", true, mio.Normal)

		return 1, nil
	}

	if console.BoolOption(input, "tree") && (console.BoolOption(input, "all") || console.BoolOption(input, "available")) {
		io.WriteError("The --tree (-t) option is not usable in combination with --all or --available (-a)", true, mio.Normal)

		return 1, nil
	}

	only := 0
	for _, o := range []string{"patch-only", "minor-only", "major-only"} {
		if console.BoolOption(input, o) {
			only++
		}
	}
	if only > 1 {
		io.WriteError("Only one of --major-only, --minor-only or --patch-only can be used at once", true, mio.Normal)

		return 1, nil
	}

	if console.BoolOption(input, "tree") && console.BoolOption(input, "latest") {
		io.WriteError("The --tree (-t) option is not usable in combination with --latest (-l)", true, mio.Normal)

		return 1, nil
	}

	if console.BoolOption(input, "tree") && console.BoolOption(input, "path") {
		io.WriteError("The --tree (-t) option is not usable in combination with --path (-P)", true, mio.Normal)

		return 1, nil
	}

	format := console.StringOption(input, "format")
	if format != "text" && format != "json" {
		io.WriteError(`Unsupported format "`+format+`". See help for supported formats.`, true, mio.Normal)

		return 1, nil
	}

	platformReqFilter, err := c.PlatformRequirementFilter(input)
	if err != nil {
		return 0, err
	}

	// init repos
	var platformOverrides *php.Array
	if comp != nil {
		v, err := comp.Config().Get("platform", 0)
		if err != nil {
			return 0, err
		}
		platformOverrides, _ = v.(*php.Array)
	}
	platformRepo, err := c.newPlatformRepository(platformOverrides)
	if err != nil {
		return 0, err
	}
	var lockedRepo repository.RepositoryInterface
	var repos repository.RepositoryInterface
	var installedRepo *repository.InstalledRepository
	var selfPackage pkg.PackageInterface

	switch {
	case console.BoolOption(input, "self") && !console.BoolOption(input, "installed") && !console.BoolOption(input, "locked"):
		required, err := c.RequireComposer(nil, nil)
		if err != nil {
			return 0, err
		}
		selfPackage = pkg.Clone(required.Package())
		if console.BoolOption(input, "name-only") {
			io.Write(selfPackage.Name(), true, mio.Normal)

			return 0, nil
		}
		if php.ToBool(input.Argument("package")) {
			return 0, NewError(ClassInvalidArgument, "You cannot use --self together with a package name")
		}
		selfRoot, _ := selfPackage.(pkg.RootPackageInterface)
		rootRepo, err := repository.NewRootPackageRepository(selfRoot)
		if err != nil {
			return 0, err
		}
		if installedRepo, err = repository.NewInstalledRepository([]repository.RepositoryInterface{rootRepo}); err != nil {
			return 0, err
		}
		repos = installedRepo
	case console.BoolOption(input, "platform"):
		if installedRepo, err = repository.NewInstalledRepository([]repository.RepositoryInterface{platformRepo}); err != nil {
			return 0, err
		}
		repos = installedRepo
	case console.BoolOption(input, "available"):
		if installedRepo, err = repository.NewInstalledRepository([]repository.RepositoryInterface{platformRepo}); err != nil {
			return 0, err
		}
		if comp != nil {
			if repos, err = repository.NewCompositeRepository(comp.RepositoryManager().Repositories()); err != nil {
				return 0, err
			}
			if err := installedRepo.AddRepository(comp.RepositoryManager().LocalRepository()); err != nil {
				return 0, err
			}
		} else {
			defaultRepos, err := defaultReposWithDefaultManager(io, c.processFactory())
			if err != nil {
				return 0, err
			}
			if repos, err = repository.NewCompositeRepository(repoMapValues(defaultRepos)); err != nil {
				return 0, err
			}
			io.WriteError("No composer.json found in the current directory, showing available packages from "+strings.Join(repoMapNames(defaultRepos), ", "), true, mio.Normal)
		}
	case console.BoolOption(input, "all") && comp != nil:
		localRepo := comp.RepositoryManager().LocalRepository()
		locker := comp.Locker()
		locked, err := locker.IsLocked()
		if err != nil {
			return 0, err
		}
		if locked {
			lr, err := locker.LockedRepository(true)
			if err != nil {
				return 0, err
			}
			lockedRepo = lr
			installedRepo, err = repository.NewInstalledRepository([]repository.RepositoryInterface{lr, localRepo, platformRepo})
			if err != nil {
				return 0, err
			}
		} else {
			installedRepo, err = repository.NewInstalledRepository([]repository.RepositoryInterface{localRepo, platformRepo})
			if err != nil {
				return 0, err
			}
		}
		filtered, err := repository.NewFilterRepository(installedRepo, php.ArrayOf("canonical", false))
		if err != nil {
			return 0, err
		}
		if repos, err = repository.NewCompositeRepository(append([]repository.RepositoryInterface{filtered}, comp.RepositoryManager().Repositories()...)); err != nil {
			return 0, err
		}
	case console.BoolOption(input, "all"):
		defaultRepos, err := defaultReposWithDefaultManager(io, c.processFactory())
		if err != nil {
			return 0, err
		}
		io.WriteError("No composer.json found in the current directory, showing available packages from "+strings.Join(repoMapNames(defaultRepos), ", "), true, mio.Normal)
		if installedRepo, err = repository.NewInstalledRepository([]repository.RepositoryInterface{platformRepo}); err != nil {
			return 0, err
		}
		if repos, err = repository.NewCompositeRepository(append([]repository.RepositoryInterface{installedRepo}, repoMapValues(defaultRepos)...)); err != nil {
			return 0, err
		}
	case console.BoolOption(input, "locked"):
		locked := false
		if comp != nil {
			if locked, err = comp.Locker().IsLocked(); err != nil {
				return 0, err
			}
		}
		if !locked {
			return 0, NewError(ClassUnexpectedValue, "A valid composer.json and composer.lock files is required to run this command with --locked")
		}
		lr, err := comp.Locker().LockedRepository(!console.BoolOption(input, "no-dev"))
		if err != nil {
			return 0, err
		}
		lockedRepo = lr
		if console.BoolOption(input, "self") {
			if err := lr.AddPackage(pkg.Clone(comp.Package())); err != nil {
				return 0, err
			}
		}
		if installedRepo, err = repository.NewInstalledRepository([]repository.RepositoryInterface{lr}); err != nil {
			return 0, err
		}
		repos = installedRepo
	default:
		// --installed / default case
		if comp == nil {
			if comp, err = c.RequireComposer(nil, nil); err != nil {
				return 0, err
			}
		}
		rootPkg := comp.Package()

		var rootRepo repository.RepositoryInterface
		if console.BoolOption(input, "self") {
			clonedRoot, _ := pkg.Clone(rootPkg).(pkg.RootPackageInterface)
			if rootRepo, err = repository.NewRootPackageRepository(clonedRoot); err != nil {
				return 0, err
			}
		} else if rootRepo, err = repository.NewInstalledArrayRepository(nil); err != nil {
			return 0, err
		}
		if console.BoolOption(input, "no-dev") {
			localPackages, err := comp.RepositoryManager().LocalRepository().Packages()
			if err != nil {
				return 0, err
			}
			packages := repository.FilterRequiredPackages(localPackages, rootPkg, false)
			cloned := make([]pkg.PackageInterface, len(packages))
			for i, p := range packages {
				cloned[i] = pkg.Clone(p)
			}
			arrayRepo, err := repository.NewInstalledArrayRepository(cloned)
			if err != nil {
				return 0, err
			}
			if installedRepo, err = repository.NewInstalledRepository([]repository.RepositoryInterface{rootRepo, arrayRepo}); err != nil {
				return 0, err
			}
		} else if installedRepo, err = repository.NewInstalledRepository([]repository.RepositoryInterface{rootRepo, comp.RepositoryManager().LocalRepository()}); err != nil {
			return 0, err
		}
		repos = installedRepo

		installedPackages, err := installedRepo.Packages()
		if err != nil {
			return 0, err
		}
		if len(installedPackages) == 0 {
			hasNonPlatformReqs := func(reqs pkg.Links) bool {
				for name := range reqs.All() {
					if !repository.IsPlatformPackage(name) {
						return true
					}
				}

				return false
			}

			if hasNonPlatformReqs(rootPkg.Requires()) || hasNonPlatformReqs(rootPkg.DevRequires()) {
				io.WriteError("<warning>No dependencies installed. Try running composer install or update.</warning>", true, mio.Normal)
			}
		}
	}

	if comp != nil {
		if _, err := comp.EventDispatcher().Dispatch(eventdispatcher.PluginCommand, eventdispatcher.NewCommandEvent(eventdispatcher.PluginCommand, "show", input, output, nil, nil)); err != nil {
			return 0, err
		}
	}

	if console.BoolOption(input, "latest") && comp == nil {
		io.WriteError(`No composer.json found in the current directory, disabling "latest" option`, true, mio.Normal)
		input.SetOption("latest", false)
	}

	packageFilterValue := input.Argument("package")
	packageFilter, hasPackageFilter := packageFilterValue.(string)

	// show single package or single version
	var single pkg.CompletePackageInterface
	var versions *php.Array
	if selfPackage != nil {
		single, _ = selfPackage.(pkg.CompletePackageInterface)
		versions = php.ArrayOf(selfPackage.PrettyVersion(), selfPackage.Version())
	} else if hasPackageFilter && !strings.Contains(packageFilter, "*") {
		var versionArg any
		if v, ok := input.Argument("version").(string); ok {
			versionArg = v
		}
		single, versions, err = c.getPackage(installedRepo, repos, packageFilter, versionArg)
		if err != nil {
			return 0, err
		}

		if single != nil && console.BoolOption(input, "direct") {
			rootRequires, err := c.getRootRequires()
			if err != nil {
				return 0, err
			}
			if !slices.Contains(rootRequires, single.Name()) {
				return 0, NewError(ClassInvalidArgument, `Package "`+single.Name()+`" is installed but not a direct dependent of the root package.`)
			}
		}

		if single == nil {
			hint := ""
			if console.BoolOption(input, "locked") {
				hint += " in lock file"
			}
			if input.HasOption("working-dir") {
				if wd := input.Option("working-dir"); wd != nil {
					hint += " in " + php.ToString(wd) + "/composer.json"
				}
			}
			if repository.IsPlatformPackage(packageFilter) && !console.BoolOption(input, "platform") {
				hint += ", try using --platform (-p) to show platform packages"
			}
			if !console.BoolOption(input, "all") && !console.BoolOption(input, "available") {
				hint += ", try using --available (-a) to show all available packages"
			}

			return 0, NewError(ClassInvalidArgument, `Package "`+packageFilter+`" not found`+hint+".")
		}
	}

	if single != nil {
		exitCode := 0
		if console.BoolOption(input, "tree") {
			arrayTree, err := c.generatePackageTree(single, installedRepo, repos)
			if err != nil {
				return 0, err
			}

			if format == "json" {
				encoded, err := json.EncodeDefault(php.ArrayOf("installed", php.ListOf(arrayTree.toArray())))
				if err != nil {
					return 0, err
				}
				io.Write(encoded, true, mio.Normal)
			} else {
				c.displayPackageTree([]*treeNode{arrayTree})
			}

			return exitCode, nil
		}

		var latestPackage pkg.PackageInterface
		if console.BoolOption(input, "latest") {
			latestPackage, err = c.findLatestPackage(single, comp, platformRepo, console.BoolOption(input, "major-only"), console.BoolOption(input, "minor-only"), console.BoolOption(input, "patch-only"), platformReqFilter)
			if err != nil {
				return 0, err
			}
		}
		if console.BoolOption(input, "outdated") &&
			console.BoolOption(input, "strict") &&
			latestPackage != nil &&
			latestPackage.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev) != single.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev) &&
			!isAbandonedComplete(latestPackage) {
			exitCode = 1
		}
		if console.BoolOption(input, "path") {
			io.Write(single.Name(), false, mio.Normal)
			path, ok, err := comp.InstallationManager().InstallPath(single)
			if err != nil {
				return 0, err
			}
			if ok {
				tok, _ := strtok(php.RealpathString(path))
				io.Write(" "+tok, true, mio.Normal)
			} else {
				io.Write(" null", true, mio.Normal)
			}

			return exitCode, nil
		}

		if format == "json" {
			err = c.printPackageInfoAsJSON(single, versions, installedRepo, latestPackage)
		} else {
			err = c.printPackageInfo(single, versions, installedRepo, latestPackage)
		}

		return exitCode, err
	}

	// show tree view if requested
	if console.BoolOption(input, "tree") {
		rootRequires, err := c.getRootRequires()
		if err != nil {
			return 0, err
		}
		packages, err := installedRepo.Packages()
		if err != nil {
			return 0, err
		}
		packages = slices.Clone(packages)
		php.SortSlice(packages, func(a, b pkg.PackageInterface) int {
			return php.Strcmp(a.String(), b.String())
		})
		var arrayTree []*treeNode
		for _, p := range packages {
			if slices.Contains(rootRequires, p.Name()) {
				node, err := c.generatePackageTree(p, installedRepo, repos)
				if err != nil {
					return 0, err
				}
				arrayTree = append(arrayTree, node)
			}
		}

		if format == "json" {
			list := php.NewArray()
			for _, n := range arrayTree {
				list.Append(n.toArray())
			}
			encoded, err := json.EncodeDefault(php.ArrayOf("installed", list))
			if err != nil {
				return 0, err
			}
			io.Write(encoded, true, mio.Normal)
		} else {
			c.displayPackageTree(arrayTree)
		}

		return 0, nil
	}

	// list packages
	packages := showLists{}
	var packageFilterRegex *php.Regexp
	if hasPackageFilter {
		if packageFilterRegex, err = php.Compile("{^" + strings.ReplaceAll(php.PregQuote(packageFilter, ""), `\*`, ".*?") + "$}i"); err != nil {
			return 0, err
		}
	}

	var packageListFilter []string
	hasListFilter := false
	if console.BoolOption(input, "direct") {
		if packageListFilter, err = c.getRootRequires(); err != nil {
			return 0, err
		}
		hasListFilter = true
	}

	if console.BoolOption(input, "path") && comp == nil {
		io.WriteError(`No composer.json found in the current directory, disabling "path" option`, true, mio.Normal)
		input.SetOption("path", false)
	}

	installedRepos := installedRepo.Repositories()
	for _, repo := range repository.FlattenRepositories(repos, true) {
		var typ string
		switch {
		case repo == repository.RepositoryInterface(platformRepo):
			typ = "platform"
		case lockedRepo != nil && repo == lockedRepo:
			typ = "locked"
		case repo == repository.RepositoryInterface(installedRepo) || slices.Contains(installedRepos, repo):
			typ = "installed"
		default:
			typ = "available"
		}
		if cr, ok := repo.(*composerrepo.ComposerRepository); ok {
			names, err := cr.PackageNames(packageFilter)
			if err != nil {
				return 0, err
			}
			for _, name := range names {
				packages.set(typ, name, showEntry{name: name})
			}

			continue
		}
		repoPackages, err := repo.Packages()
		if err != nil {
			return 0, err
		}
		for _, p := range repoPackages {
			entry, ok := packages.get(typ, p.Name())
			if !ok || entry.pkg == nil || semver.VersionCompare(entry.pkg.Version(), p.Version()) < 0 {
				for {
					alias, ok := p.(pkg.Alias)
					if !ok {
						break
					}
					p = alias.AliasOf()
				}
				matches := true
				if packageFilterRegex != nil {
					if matches, err = packageFilterRegex.IsMatch(p.Name()); err != nil {
						return 0, err
					}
				}
				if matches && (!hasListFilter || slices.Contains(packageListFilter, p.Name())) {
					packages.set(typ, p.Name(), showEntry{pkg: p})
				}
			}
		}
		if repo == repository.RepositoryInterface(platformRepo) {
			for name, p := range platformRepo.DisabledPackages().All() {
				packages.set(typ, name, showEntry{pkg: p})
			}
		}
	}

	showAllTypes := console.BoolOption(input, "all")
	showLatest := console.BoolOption(input, "latest")
	showMajorOnly := console.BoolOption(input, "major-only")
	showMinorOnly := console.BoolOption(input, "minor-only")
	showPatchOnly := console.BoolOption(input, "patch-only")
	ignores := console.StringsOption(input, "ignore")
	for i, s := range ignores {
		ignores[i] = php.Strtolower(s)
	}
	ignoredPackagesRegex, err := php.Compile(pkg.PackageNamesToRegexp(ignores, "{^(?:%s)$}iD"))
	if err != nil {
		return 0, err
	}
	indent := ""
	if showAllTypes {
		indent = "  "
	}
	latestPackages := map[string]pkg.PackageInterface{}
	exitCode := 0
	viewData := php.NewArray()
	viewMetaData := map[string]showMeta{}

	writeVersion := false
	writeDescription := false

	rootRequires, err := c.getRootRequires()
	if err != nil {
		return 0, err
	}

	for _, t := range []struct {
		typ         string
		showVersion bool
	}{{"platform", true}, {"locked", true}, {"available", false}, {"installed", true}} {
		typ := t.typ
		list := packages[typ]
		if list == nil {
			continue
		}
		list.ksort()

		nameLength, versionLength, latestLength, releaseDateLength := 0, 0, 0, 0

		if showLatest && t.showVersion {
			for _, entry := range list.values() {
				if entry.pkg == nil {
					continue
				}
				ignored, err := ignoredPackagesRegex.IsMatch(entry.pkg.PrettyName())
				if err != nil {
					return 0, err
				}
				if ignored {
					continue
				}
				latestPackage, err := c.findLatestPackage(entry.pkg, comp, platformRepo, showMajorOnly, showMinorOnly, showPatchOnly, platformReqFilter)
				if err != nil {
					return 0, err
				}
				if latestPackage == nil {
					continue
				}
				latestPackages[entry.pkg.PrettyName()] = latestPackage
			}
		}

		writePath := !console.BoolOption(input, "name-only") && console.BoolOption(input, "path")
		writeVersion = !console.BoolOption(input, "name-only") && !console.BoolOption(input, "path") && t.showVersion
		writeLatest := writeVersion && showLatest
		writeDescription = !console.BoolOption(input, "name-only") && !console.BoolOption(input, "path")
		writeReleaseDate := writeLatest && (console.BoolOption(input, "sort-by-age") || format == "json")

		hasOutdatedPackages := false

		entries := list.values()
		if console.BoolOption(input, "sort-by-age") {
			php.SortSlice(entries, func(a, b showEntry) int {
				if a.pkg != nil && b.pkg != nil {
					return compareReleaseDates(a.pkg, b.pkg)
				}

				return 0
			})
		}

		typeData := php.NewArray()
		for _, entry := range entries {
			packageViewData := php.NewArray()
			if entry.pkg != nil {
				p := entry.pkg
				var latestPackage pkg.PackageInterface
				if showLatest {
					latestPackage = latestPackages[p.PrettyName()]
				}

				// Determine if Composer is checking outdated dependencies and if current package should trigger non-default exit code
				packageIsUpToDate := latestPackage != nil && latestPackage.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev) == p.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev) && !isAbandonedComplete(latestPackage)
				// When using --major-only, and no bigger version than current major is found then it is considered up to date
				packageIsUpToDate = packageIsUpToDate || (latestPackage == nil && showMajorOnly)
				packageIsIgnored, err := ignoredPackagesRegex.IsMatch(p.PrettyName())
				if err != nil {
					return 0, err
				}
				if console.BoolOption(input, "outdated") && (packageIsUpToDate || packageIsIgnored) {
					continue
				}

				if console.BoolOption(input, "outdated") || console.BoolOption(input, "strict") {
					hasOutdatedPackages = true
				}

				packageViewData.Set("name", p.PrettyName())
				packageViewData.Set("direct-dependency", slices.Contains(rootRequires, p.Name()))
				if format != "json" || !console.BoolOption(input, "name-only") {
					var homepage any
					if cp, ok := p.(pkg.CompletePackageInterface); ok {
						homepage = nullable(cp.Homepage())
					}
					packageViewData.Set("homepage", homepage)
					packageViewData.Set("source", nullable(pkg.GetViewSourceURL(p)))
				}
				nameLength = max(nameLength, len(p.PrettyName()))
				if writeVersion {
					v := p.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev)
					if format == "text" {
						v = php.LtrimSet(v, "v")
					}
					packageViewData.Set("version", v)
					versionLength = max(versionLength, len(v))
				}
				if writeReleaseDate {
					if releaseDate, ok := p.ReleaseDate(); ok {
						age := strings.ReplaceAll(c.getRelativeTime(releaseDate), " ago", " old")
						if !strings.Contains(age, " old") {
							age = "from " + age
						}
						packageViewData.Set("release-age", age)
						releaseDateLength = max(releaseDateLength, len(age))
						packageViewData.Set("release-date", releaseDate.Format(atomFormat))
					} else {
						packageViewData.Set("release-age", "")
						packageViewData.Set("release-date", "")
					}
				}
				if writeLatest && latestPackage != nil {
					latest := latestPackage.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev)
					if format == "text" {
						latest = php.LtrimSet(latest, "v")
					}
					packageViewData.Set("latest", latest)
					status, err := getUpdateStatus(latestPackage, p)
					if err != nil {
						return 0, err
					}
					packageViewData.Set("latest-status", status)
					latestLength = max(latestLength, len(latest))

					if releaseDate, ok := latestPackage.ReleaseDate(); ok {
						packageViewData.Set("latest-release-date", releaseDate.Format(atomFormat))
					} else {
						packageViewData.Set("latest-release-date", "")
					}
				} else if writeLatest {
					packageViewData.Set("latest", "[none matched]")
					packageViewData.Set("latest-status", "up-to-date")
					latestLength = max(latestLength, len("[none matched]"))
				}
				if cp, ok := p.(pkg.CompletePackageInterface); ok && writeDescription {
					packageViewData.Set("description", nullable(cp.Description()))
				}
				if writePath {
					path, ok, err := comp.InstallationManager().InstallPath(p)
					if err != nil {
						return 0, err
					}
					if ok {
						if tok, found := strtok(php.RealpathString(path)); found {
							packageViewData.Set("path", tok)
						} else {
							packageViewData.Set("path", false)
						}
					} else {
						packageViewData.Set("path", nil)
					}
				}

				var packageIsAbandoned any = false
				if lp, ok := latestPackage.(pkg.CompletePackageInterface); ok && lp.IsAbandoned() {
					replacementPackageName := lp.ReplacementPackage()
					replacement := "No replacement was suggested"
					if replacementPackageName.Valid {
						replacement = "Use " + replacementPackageName.S + " instead"
					}
					packageViewData.Set("warning", "Package "+p.PrettyName()+" is abandoned, you should avoid using it. "+replacement+".")
					if replacementPackageName.Valid {
						packageIsAbandoned = replacementPackageName.S
					} else {
						packageIsAbandoned = true
					}
				}

				packageViewData.Set("abandoned", packageIsAbandoned)
			} else {
				packageViewData.Set("name", entry.name)
				nameLength = max(nameLength, len(entry.name))
			}
			typeData.Append(packageViewData)
		}
		viewData.Set(typ, typeData)
		viewMetaData[typ] = showMeta{
			nameLength:        nameLength,
			versionLength:     versionLength,
			latestLength:      latestLength,
			releaseDateLength: releaseDateLength,
			writeLatest:       writeLatest,
			writeReleaseDate:  writeReleaseDate,
		}
		if console.BoolOption(input, "strict") && hasOutdatedPackages {
			exitCode = 1

			break
		}
	}

	if format == "json" {
		encoded, err := json.EncodeDefault(viewData)
		if err != nil {
			return 0, err
		}
		io.Write(encoded, true, mio.Normal)

		return exitCode, nil
	}

	anyData := false
	for _, v := range viewData.All() {
		if a, ok := v.(*php.Array); ok && a.Len() > 0 {
			anyData = true
		}
	}
	if console.BoolOption(input, "latest") && anyData {
		if !io.IsDecorated() {
			io.WriteError("Legend:", true, mio.Normal)
			io.WriteError("! patch or minor release available - update recommended", true, mio.Normal)
			io.WriteError("~ major release available - update possible", true, mio.Normal)
			if !console.BoolOption(input, "outdated") {
				io.WriteError("= up to date version", true, mio.Normal)
			}
		} else {
			io.WriteError("<info>Color legend:</info>", true, mio.Normal)
			io.WriteError("- <highlight>patch or minor</highlight> release available - update recommended", true, mio.Normal)
			io.WriteError("- <comment>major</comment> release available - update possible", true, mio.Normal)
			if !console.BoolOption(input, "outdated") {
				io.WriteError("- <info>up to date</info> version", true, mio.Normal)
			}
		}
	}

	width := c.TerminalWidth()

	for k, v := range viewData.All() {
		typ := k.String()
		list, _ := v.(*php.Array)
		meta := viewMetaData[typ]
		nameLength := meta.nameLength
		versionLength := meta.versionLength
		latestLength := meta.latestLength
		releaseDateLength := meta.releaseDateLength
		writeLatest := meta.writeLatest
		writeReleaseDate := meta.writeReleaseDate

		versionFits := nameLength+versionLength+3 <= width
		latestFits := nameLength+versionLength+latestLength+3 <= width
		releaseDateFits := nameLength+versionLength+latestLength+releaseDateLength+3 <= width
		descriptionFits := nameLength+versionLength+latestLength+releaseDateLength+24 <= width

		if latestFits && !io.IsDecorated() {
			latestLength += 2
		}

		if showAllTypes {
			if typ == "available" {
				io.Write("<comment>"+typ+"</comment>:", true, mio.Normal)
			} else {
				io.Write("<info>"+typ+"</info>:", true, mio.Normal)
			}
		}

		var pkgs []*php.Array
		for _, p := range list.All() {
			pa, _ := p.(*php.Array)
			pkgs = append(pkgs, pa)
		}

		pp := printOptions{
			indent:            indent,
			writeVersion:      writeVersion && versionFits,
			writeDescription:  writeDescription && descriptionFits,
			width:             width,
			versionLength:     versionLength,
			nameLength:        nameLength,
			latestLength:      latestLength,
			writeReleaseDate:  writeReleaseDate && releaseDateFits,
			releaseDateLength: releaseDateLength,
		}
		if writeLatest && !console.BoolOption(input, "direct") {
			var directDeps, transitiveDeps []*php.Array
			for _, p := range pkgs {
				if d, _ := p.Get("direct-dependency"); d == true {
					directDeps = append(directDeps, p)
				} else {
					transitiveDeps = append(transitiveDeps, p)
				}
			}

			pp.writeLatest = latestFits
			io.WriteError("", true, mio.Normal)
			io.WriteError("<info>Direct dependencies required in composer.json:</>", true, mio.Normal)
			if len(directDeps) > 0 {
				c.printPackages(io, directDeps, pp)
			} else {
				io.WriteError("Everything up to date", true, mio.Normal)
			}
			io.WriteError("", true, mio.Normal)
			io.WriteError("<info>Transitive dependencies not required in composer.json:</>", true, mio.Normal)
			if len(transitiveDeps) > 0 {
				c.printPackages(io, transitiveDeps, pp)
			} else {
				io.WriteError("Everything up to date", true, mio.Normal)
			}
		} else if writeLatest && len(pkgs) == 0 {
			io.WriteError("All your direct dependencies are up to date", true, mio.Normal)
		} else {
			pp.writeLatest = writeLatest && latestFits
			c.printPackages(io, pkgs, pp)
		}

		if showAllTypes {
			io.Write("", true, mio.Normal)
		}
	}

	return exitCode, nil
}

// atomFormat is DateTimeInterface::ATOM.
const atomFormat = "2006-01-02T15:04:05-07:00"

type showMeta struct {
	nameLength, versionLength, latestLength, releaseDateLength int
	writeLatest, writeReleaseDate                              bool
}

type printOptions struct {
	indent                                                        string
	writeVersion, writeLatest, writeDescription, writeReleaseDate bool
	width, versionLength, nameLength, latestLength                int
	releaseDateLength                                             int
}

// nullable is a NullString as a PHP value.
func nullable(s pkg.NullString) any {
	if !s.Valid {
		return nil
	}

	return s.S
}

// isAbandonedComplete is `$p instanceof CompletePackageInterface &&
// $p->isAbandoned()`.
func isAbandonedComplete(p pkg.PackageInterface) bool {
	cp, ok := p.(pkg.CompletePackageInterface)

	return ok && cp.IsAbandoned()
}

// compareReleaseDates is `$a->getReleaseDate() <=> $b->getReleaseDate()`:
// null sorts before any date.
func compareReleaseDates(a, b pkg.PackageInterface) int {
	da, oka := a.ReleaseDate()
	db, okb := b.ReleaseDate()
	switch {
	case !oka && !okb:
		return 0
	case !oka:
		return -1
	case !okb:
		return 1
	}

	return da.Compare(db)
}

// strtok ports strtok($s, "\r\n") for a first call: the first token, or
// false when s holds only delimiters.
func strtok(s string) (string, bool) {
	const delims = "\r\n"
	s = strings.TrimLeft(s, delims)
	if s == "" {
		return "", false
	}
	if i := strings.IndexAny(s, delims); i >= 0 {
		return s[:i], true
	}

	return s, true
}

func (c *ShowCommand) printPackages(io mio.IO, packages []*php.Array, o printOptions) {
	padName := o.writeVersion || o.writeLatest || o.writeReleaseDate || o.writeDescription
	padVersion := o.writeLatest || o.writeReleaseDate || o.writeDescription
	padLatest := o.writeDescription || o.writeReleaseDate
	padReleaseDate := o.writeDescription
	pad := func(s string, cond bool, length int) string {
		if !cond {
			return s
		}

		return php.StrPad(s, length, " ", php.StrPadRight)
	}
	str := func(p *php.Array, k string) (string, bool) {
		v, ok := p.Get(k)
		if !ok || v == nil {
			return "", false
		}

		return php.ToString(v), true
	}
	for _, p := range packages {
		name, _ := str(p, "name")
		link, ok := str(p, "source")
		if !ok {
			link, _ = str(p, "homepage")
		}
		if link != "" {
			n := 0
			if padName {
				n = o.nameLength - len(name)
			}
			io.Write(o.indent+"<href="+console.Escape(link)+">"+name+"</>"+strings.Repeat(" ", max(n, 0)), false, mio.Normal)
		} else {
			io.Write(o.indent+pad(name, padName, o.nameLength), false, mio.Normal)
		}
		if v, ok := str(p, "version"); ok && o.writeVersion {
			io.Write(" "+pad(v, padVersion, o.versionLength), false, mio.Normal)
		}
		latestVersion, ok1 := str(p, "latest")
		updateStatus, ok2 := str(p, "latest-status")
		if ok1 && ok2 && o.writeLatest {
			style := updateStatusToVersionStyle(updateStatus)
			if !io.IsDecorated() {
				latestVersion = strings.NewReplacer("up-to-date", "=", "semver-safe-update", "!", "update-possible", "~").Replace(updateStatus) + " " + latestVersion
			}
			io.Write(" <"+style+">"+pad(latestVersion, padLatest, o.latestLength)+"</"+style+">", false, mio.Normal)
			if age, ok := str(p, "release-age"); o.writeReleaseDate && ok {
				io.Write(" "+pad(age, padReleaseDate, o.releaseDateLength), false, mio.Normal)
			}
		}
		if description, ok := str(p, "description"); ok && o.writeDescription {
			description, _ = strtok(description)

			// Compute remaining width available for the description.
			remaining := o.width - o.nameLength - o.versionLength - o.releaseDateLength - 4
			if o.writeLatest {
				remaining -= o.latestLength
			}

			// If nothing fits, clear the description.
			switch {
			case remaining <= 0:
				description = ""
			case c.extensionLoaded("mbstring"):
				// Use mb_strwidth/mb_strimwidth to measure and trim by display width
				// (CJK characters count as width 2). mb_strimwidth counts the trim
				// marker ('...') in the width parameter, so pass $remaining directly.
				if mbStrwidth(description) > remaining {
					description = mbStrimwidth(description, remaining, "...")
				}
			default:
				// Fallback when mbstring is not available: do a conservative byte-based cut.
				// Ensure cut length is non-negative and leave room for the ellipsis.
				cut := max(0, remaining-3)
				if len(description) > cut {
					description = description[:cut] + "..."
				}
			}

			io.Write(" "+description, false, mio.Normal)
		}
		if v, ok := p.Get("path"); ok {
			if s, isString := v.(string); isString {
				io.Write(" "+s, false, mio.Normal)
			} else {
				io.Write(" null", false, mio.Normal)
			}
		}
		io.Write("", true, mio.Normal)
		if warning, ok := str(p, "warning"); ok {
			io.Write("<warning>"+warning+"</warning>", true, mio.Normal)
		}
	}
}

// extensionLoaded is extension_loaded() of the PHP Composer would run on.
func (c *ShowCommand) extensionLoaded(name string) bool {
	return c.processRuntime().Environment().ExtensionLoaded(name)
}

// getRootRequires ports ShowCommand::getRootRequires.
func (c *ShowCommand) getRootRequires() ([]string, error) {
	comp, err := c.TryComposer(nil, nil)
	if err != nil || comp == nil {
		return []string{}, err
	}

	rootPackage := comp.Package()
	merged := php.NewArray()
	for name := range rootPackage.Requires().All() {
		merged.Set(name, true)
	}
	for name := range rootPackage.DevRequires().All() {
		merged.Set(name, true)
	}
	names := make([]string, 0, merged.Len())
	for k := range merged.All() {
		names = append(names, php.Strtolower(k.String()))
	}

	return names, nil
}

// getPackage ports ShowCommand::getPackage: version is nil, a string or a
// semver.ConstraintInterface.
func (c *ShowCommand) getPackage(installedRepo *repository.InstalledRepository, repos repository.RepositoryInterface, name string, ver any) (pkg.CompletePackageInterface, *php.Array, error) {
	name = php.Strtolower(name)
	var constraint semver.ConstraintInterface
	switch v := ver.(type) {
	case string:
		var err error
		if constraint, err = c.versionParser.ParseConstraints(v); err != nil {
			return nil, nil, err
		}
	case semver.ConstraintInterface:
		constraint = v
	}

	policy := resolver.NewDefaultPolicy(false, false, nil)
	repositorySet, err := repository.NewRepositorySet("dev", nil, nil, nil, nil, nil)
	if err != nil {
		return nil, nil, err
	}
	repositorySet.AllowInstalledRepositories(true)
	if err := repositorySet.AddRepository(repos); err != nil {
		return nil, nil, err
	}

	var matchedPackage pkg.PackageInterface
	versions := php.NewArray()
	var pool *resolver.Pool
	if repository.IsPlatformPackage(name) {
		pool, err = resolver.CreatePoolWithAllPackages(repositorySet)
	} else {
		pool, err = resolver.CreatePoolForPackage(repositorySet, name, nil)
	}
	if err != nil {
		return nil, nil, err
	}
	matches := pool.WhatProvides(name, constraint)
	var literals []int32
	for _, p := range matches {
		// avoid showing the 9999999-dev alias if the default branch has no branch-alias set
		if alias, ok := p.(pkg.Alias); ok && p.Version() == pkg.DefaultBranchAlias {
			p = alias.AliasOf()
		}

		// select an exact match if it is in the installed repo and no specific version was required
		if ver == nil {
			has, err := installedRepo.HasPackage(p)
			if err != nil {
				return nil, nil, err
			}
			if has {
				matchedPackage = p
			}
		}

		versions.Set(p.PrettyVersion(), p.Version())
		literals = append(literals, int32(p.ID())) //nolint:gosec // pool ids are int32 literals
	}

	// select preferred package according to policy rules
	if matchedPackage == nil && len(literals) > 0 {
		preferred := policy.SelectPreferredPackages(pool, literals, "")
		matchedPackage = pool.LiteralToPackage(preferred[0])
	}

	if matchedPackage == nil {
		return nil, versions, nil
	}
	cp, ok := matchedPackage.(pkg.CompletePackageInterface)
	if !ok {
		return nil, nil, NewError(ClassLogic, "ShowCommand::getPackage can only work with CompletePackageInterface, but got "+matchedPackage.Class())
	}

	return cp, versions, nil
}

// printPackageInfo ports ShowCommand::printPackageInfo.
func (c *ShowCommand) printPackageInfo(p pkg.CompletePackageInterface, versions *php.Array, installedRepo *repository.InstalledRepository, latestPackage pkg.PackageInterface) error {
	io := c.IO()

	if err := c.printMeta(p, versions, installedRepo, latestPackage); err != nil {
		return err
	}
	if err := c.printLinks(p, pkg.TypeRequire, ""); err != nil {
		return err
	}
	if err := c.printLinks(p, pkg.TypeDevRequire, "requires (dev)"); err != nil {
		return err
	}

	if suggests := p.Suggests(); suggests.Len() > 0 {
		io.Write("\n<info>suggests</info>", true, mio.Normal)
		for suggested, reason := range suggests.All() {
			io.Write(suggested.String()+" <comment>"+php.ToString(reason)+"</comment>", true, mio.Normal)
		}
	}

	for _, t := range []string{pkg.TypeProvide, pkg.TypeConflict, pkg.TypeReplace} {
		if err := c.printLinks(p, t, ""); err != nil {
			return err
		}
	}

	return nil
}

// printMeta ports ShowCommand::printMeta.
func (c *ShowCommand) printMeta(p pkg.CompletePackageInterface, versions *php.Array, installedRepo *repository.InstalledRepository, latestPackage pkg.PackageInterface) error {
	isInstalledPackage := false
	if !repository.IsPlatformPackage(p.Name()) {
		has, err := installedRepo.HasPackage(p)
		if err != nil {
			return err
		}
		isInstalledPackage = has
	}

	io := c.IO()
	io.Write("<info>name</info>     : "+p.PrettyName(), true, mio.Normal)
	io.Write("<info>descrip.</info> : "+p.Description().S, true, mio.Normal)
	io.Write("<info>keywords</info> : "+implodeComma(p.Keywords()), true, mio.Normal)
	if err := c.printVersions(p, versions, installedRepo); err != nil {
		return err
	}
	if releaseDate, ok := p.ReleaseDate(); isInstalledPackage && ok {
		io.Write("<info>released</info> : "+releaseDate.Format("2006-01-02")+", "+c.getRelativeTime(releaseDate), true, mio.Normal)
	}
	if latestPackage != nil {
		style, err := getVersionStyle(latestPackage, p)
		if err != nil {
			return err
		}
		releasedTime := ""
		if releaseDate, ok := latestPackage.ReleaseDate(); ok {
			releasedTime = " released " + releaseDate.Format("2006-01-02") + ", " + c.getRelativeTime(releaseDate)
		}
		io.Write("<info>latest</info>   : <"+style+">"+latestPackage.PrettyVersion()+"</"+style+">"+releasedTime, true, mio.Normal)
	} else {
		latestPackage = p
	}
	io.Write("<info>type</info>     : "+p.Type(), true, mio.Normal)
	c.printLicenses(p)
	io.Write("<info>homepage</info> : "+p.Homepage().S, true, mio.Normal)
	io.Write("<info>source</info>   : ["+p.SourceType().S+"] <comment>"+p.SourceURL().S+"</comment> "+p.SourceReference().S, true, mio.Normal)
	io.Write("<info>dist</info>     : ["+p.DistType().S+"] <comment>"+p.DistURL().S+"</comment> "+p.DistReference().S, true, mio.Normal)
	if isInstalledPackage {
		comp, err := c.RequireComposer(nil, nil)
		if err != nil {
			return err
		}
		path, ok, err := comp.InstallationManager().InstallPath(p)
		if err != nil {
			return err
		}
		if ok {
			io.Write("<info>path</info>     : "+php.RealpathString(path), true, mio.Normal)
		} else {
			io.Write("<info>path</info>     : null", true, mio.Normal)
		}
	}
	io.Write("<info>names</info>    : "+strings.Join(p.Names(true), ", "), true, mio.Normal)

	if lp, ok := latestPackage.(pkg.CompletePackageInterface); ok && lp.IsAbandoned() {
		replacement := ""
		if r := lp.ReplacementPackage(); r.Valid {
			replacement = " The author suggests using the " + r.S + " package instead."
		}

		io.WriteError("<warning>Attention: This package is abandoned and no longer maintained."+replacement+"</warning>", true, mio.Normal)
	}

	if support := p.Support(); support.Len() > 0 {
		io.Write("\n<info>support</info>", true, mio.Normal)
		for typ, value := range support.All() {
			io.Write("<comment>"+typ.String()+"</comment> : "+php.ToString(value), true, mio.Normal)
		}
	}

	if autoloadConfig := p.Autoload(); autoloadConfig.Len() > 0 {
		io.Write("\n<info>autoload</info>", true, mio.Normal)
		for typ, autoloads := range autoloadConfig.All() {
			io.Write("<comment>"+typ.String()+"</comment>", true, mio.Normal)

			switch typ.String() {
			case "psr-0", "psr-4":
				a, _ := autoloads.(*php.Array)
				if a == nil {
					break
				}
				for name, path := range a.All() {
					n := name.String()
					if !php.ToBool(name.Value()) {
						n = "*"
					}
					var shown string
					if arr, ok := path.(*php.Array); ok {
						shown = implodeComma(arr)
					} else if php.ToBool(path) {
						shown = php.ToString(path)
					} else {
						shown = "."
					}
					io.Write(n+" => "+shown, true, mio.Normal)
				}
			case "classmap":
				a, _ := autoloads.(*php.Array)
				io.Write(implodeComma(a), true, mio.Normal)
			}
		}
		if includePaths := p.IncludePaths(); includePaths.Len() > 0 {
			io.Write("<comment>include-path</comment>", true, mio.Normal)
			io.Write(implodeComma(includePaths), true, mio.Normal)
		}
	}

	return nil
}

// implodeComma is implode(", ", $array) (nil is an empty array).
func implodeComma(a *php.Array) string {
	if a == nil {
		return ""
	}
	parts := make([]string, 0, a.Len())
	for _, v := range a.All() {
		parts = append(parts, php.ToString(v))
	}

	return strings.Join(parts, ", ")
}

// printVersions ports ShowCommand::printVersions.
func (c *ShowCommand) printVersions(p pkg.CompletePackageInterface, versions *php.Array, installedRepo *repository.InstalledRepository) error {
	keys := make([]string, 0, versions.Len())
	for k := range versions.All() {
		keys = append(keys, k.String())
	}
	sorted, err := semver.Semver.Rsort(keys)
	if err != nil {
		return err
	}

	// highlight installed version
	installedPackages, err := installedRepo.FindPackages(p.Name(), nil)
	if err != nil {
		return err
	}
	for _, installedPackage := range installedPackages {
		installedVersion := installedPackage.PrettyVersion()
		if i := slices.Index(sorted, installedVersion); i >= 0 {
			sorted[i] = "<info>* " + installedVersion + "</info>"
		}
	}

	c.IO().Write("<info>versions</info> : "+strings.Join(sorted, ", "), true, mio.Normal)

	return nil
}

func linksOf(p pkg.PackageInterface, linkType string) pkg.Links {
	switch linkType {
	case pkg.TypeRequire:
		return p.Requires()
	case pkg.TypeDevRequire:
		return p.DevRequires()
	case pkg.TypeProvide:
		return p.Provides()
	case pkg.TypeConflict:
		return p.Conflicts()
	case pkg.TypeReplace:
		return p.Replaces()
	}

	return pkg.Links{}
}

// printLinks ports ShowCommand::printLinks.
func (c *ShowCommand) printLinks(p pkg.CompletePackageInterface, linkType, title string) error {
	if title == "" {
		title = linkType
	}
	io := c.IO()
	links := linksOf(p, linkType)
	if links.Len() == 0 {
		return nil
	}
	io.Write("\n<info>"+title+"</info>", true, mio.Normal)

	for link := range links.Values() {
		constraint, err := link.PrettyConstraint()
		if err != nil {
			return err
		}
		io.Write(link.Target()+" <comment>"+constraint+"</comment>", true, mio.Normal)
	}

	return nil
}

// printLicenses ports ShowCommand::printLicenses.
func (c *ShowCommand) printLicenses(p pkg.CompletePackageInterface) {
	spdxLicenses := spdx.New()

	io := c.IO()
	for _, v := range p.License().All() {
		licenseID := php.ToString(v)
		license, ok := spdxLicenses.GetLicenseByIdentifier(licenseID)

		var out string
		switch {
		case !ok:
			out = licenseID
		case license.OSIApproved:
			// is license OSI approved?
			out = license.Name + " (" + licenseID + ") (OSI approved) " + license.URL
		default:
			out = license.Name + " (" + licenseID + ") " + license.URL
		}

		io.Write("<info>license</info>  : "+out, true, mio.Normal)
	}
}

// printPackageInfoAsJSON ports ShowCommand::printPackageInfoAsJson.
func (c *ShowCommand) printPackageInfoAsJSON(p pkg.CompletePackageInterface, versions *php.Array, installedRepo *repository.InstalledRepository, latestPackage pkg.PackageInterface) error {
	keywords := p.Keywords()
	if keywords == nil {
		keywords = php.NewArray()
	}
	j := php.ArrayOf(
		"name", p.PrettyName(),
		"description", nullable(p.Description()),
		"keywords", keywords,
		"type", p.Type(),
		"homepage", nullable(p.Homepage()),
		"names", php.StringList(p.Names(true)),
	)

	appendVersions(j, versions)
	appendLicenses(j, p)

	if latestPackage != nil {
		j.Set("latest", latestPackage.PrettyVersion())
	} else {
		latestPackage = p
	}

	if p.SourceType().Valid {
		j.Set("source", php.ArrayOf(
			"type", p.SourceType().S,
			"url", nullable(p.SourceURL()),
			"reference", nullable(p.SourceReference()),
		))
	}

	if p.DistType().Valid {
		j.Set("dist", php.ArrayOf(
			"type", p.DistType().S,
			"url", nullable(p.DistURL()),
			"reference", nullable(p.DistReference()),
		))
	}

	if !repository.IsPlatformPackage(p.Name()) {
		has, err := installedRepo.HasPackage(p)
		if err != nil {
			return err
		}
		if has {
			comp, err := c.RequireComposer(nil, nil)
			if err != nil {
				return err
			}
			path, ok, err := comp.InstallationManager().InstallPath(p)
			if err != nil {
				return err
			}
			if ok {
				if real, found := php.Realpath(path); found {
					j.Set("path", real)
				}
			} else {
				j.Set("path", nil)
			}

			if releaseDate, ok := p.ReleaseDate(); ok {
				j.Set("released", releaseDate.Format(atomFormat))
			}
		}
	}

	if lp, ok := latestPackage.(pkg.CompletePackageInterface); ok && lp.IsAbandoned() {
		j.Set("replacement", nullable(lp.ReplacementPackage()))
	}

	if suggests := p.Suggests(); suggests.Len() > 0 {
		j.Set("suggests", suggests)
	}

	if support := p.Support(); support.Len() > 0 {
		j.Set("support", support)
	}

	appendAutoload(j, p)

	if includePaths := p.IncludePaths(); includePaths.Len() > 0 {
		j.Set("include_path", includePaths)
	}

	if err := appendLinks(j, p); err != nil {
		return err
	}

	encoded, err := json.EncodeDefault(j)
	if err != nil {
		return err
	}
	c.IO().Write(encoded, true, mio.Normal)

	return nil
}

// appendVersions ports ShowCommand::appendVersions.
func appendVersions(j, versions *php.Array) {
	sorted := versions.Clone()
	php.Uasort(sorted, func(a, b any) int {
		return semver.VersionCompare(php.ToString(a), php.ToString(b))
	})
	keys := sorted.Keys()
	list := php.NewArray()
	for _, k := range slices.Backward(keys) {
		list.Append(k.Value())
	}
	j.Set("versions", list)
}

// appendLicenses ports ShowCommand::appendLicenses.
func appendLicenses(j *php.Array, p pkg.CompletePackageInterface) {
	licenses := p.License()
	if licenses.Len() == 0 {
		return
	}
	spdxLicenses := spdx.New()

	out := php.NewArray()
	for k, v := range licenses.All() {
		licenseID := php.ToString(v)
		license, ok := spdxLicenses.GetLicenseByIdentifier(licenseID)
		if !ok {
			out.SetKey(k, v)

			continue
		}
		out.SetKey(k, php.ArrayOf(
			"name", license.Name,
			"osi", licenseID,
			"url", license.URL,
		))
	}
	j.Set("licenses", out)
}

// appendAutoload ports ShowCommand::appendAutoload.
func appendAutoload(j *php.Array, p pkg.CompletePackageInterface) {
	autoloadConfig := p.Autoload()
	if autoloadConfig.Len() == 0 {
		return
	}
	autoload := php.NewArray()

	for typ, autoloads := range autoloadConfig.All() {
		switch typ.String() {
		case "psr-0", "psr-4":
			psr := php.NewArray()
			if a, ok := autoloads.(*php.Array); ok {
				for name, path := range a.All() {
					if !php.ToBool(path) {
						path = "."
					}
					if php.ToBool(name.Value()) {
						psr.SetKey(name, path)
					} else {
						psr.Set("*", path)
					}
				}
			}
			autoload.SetKey(typ, psr)
		case "classmap":
			autoload.Set("classmap", autoloads)
		}
	}

	j.Set("autoload", autoload)
}

// appendLinks ports ShowCommand::appendLinks.
func appendLinks(j *php.Array, p pkg.CompletePackageInterface) error {
	for _, linkType := range pkg.LinkTypes() {
		links := linksOf(p, linkType)
		if links.Len() == 0 {
			continue
		}
		out := php.NewArray()
		for link := range links.Values() {
			constraint, err := link.PrettyConstraint()
			if err != nil {
				return err
			}
			out.Set(link.Target(), constraint)
		}
		j.Set(linkType, out)
	}

	return nil
}

// initStyles ports ShowCommand::initStyles.
func (c *ShowCommand) initStyles(output console.Output) error {
	c.colors = []string{"green", "yellow", "cyan", "magenta", "blue"}

	for _, color := range c.colors {
		style, err := console.NewOutputFormatterStyle(color, "")
		if err != nil {
			return err
		}
		output.Formatter().SetStyle(color, style)
	}

	return nil
}

// treeNode is an entry of the package tree arrays.
type treeNode struct {
	name    string
	version string
	// description is set for top-level entries only.
	hasDescription bool
	description    pkg.NullString
	requires       []*treeNode
}

func (n *treeNode) toArray() *php.Array {
	a := php.ArrayOf("name", n.name, "version", n.version)
	if n.hasDescription {
		a.Set("description", nullable(n.description))
	}
	if len(n.requires) > 0 {
		list := php.NewArray()
		for _, r := range n.requires {
			list.Append(r.toArray())
		}
		a.Set("requires", list)
	}

	return a
}

// displayPackageTree ports ShowCommand::displayPackageTree.
func (c *ShowCommand) displayPackageTree(arrayTree []*treeNode) {
	io := c.IO()
	for _, p := range arrayTree {
		io.Write("<info>"+p.name+"</info>", false, mio.Normal)
		io.Write(" "+p.version, false, mio.Normal)
		if p.hasDescription && p.description.Valid {
			tok, _ := strtok(p.description.S)
			io.Write(" "+tok, true, mio.Normal)
		} else {
			// output newline
			io.Write("", true, mio.Normal)
		}

		treeBar := "├"
		total := len(p.requires)
		for j, require := range p.requires {
			if j+1 == total {
				treeBar = "└"
			}
			level := 1
			color := c.colors[level]
			c.writeTreeLine(treeBar + "──<" + color + ">" + require.name + "</" + color + "> " + require.version)

			treeBar = strings.ReplaceAll(treeBar, "└", " ")
			packagesInTree := []string{p.name, require.name}

			c.displayTree(require, packagesInTree, treeBar, level+1)
		}
	}
}

// generatePackageTree ports ShowCommand::generatePackageTree.
func (c *ShowCommand) generatePackageTree(p pkg.PackageInterface, installedRepo *repository.InstalledRepository, remoteRepos repository.RepositoryInterface) (*treeNode, error) {
	requires := sortedLinks(p.Requires())
	var children []*treeNode
	for _, require := range requires {
		requireName := require.name
		packagesInTree := []string{p.Name(), requireName}

		constraint, err := require.link.PrettyConstraint()
		if err != nil {
			return nil, err
		}
		treeChildDesc := &treeNode{name: requireName, version: constraint}

		deepChildren, err := c.addTree(requireName, require.link, installedRepo, remoteRepos, packagesInTree)
		if err != nil {
			return nil, err
		}
		treeChildDesc.requires = deepChildren

		children = append(children, treeChildDesc)
	}
	tree := &treeNode{name: p.PrettyName(), version: p.PrettyVersion(), hasDescription: true, description: pkg.Str("")}
	if cp, ok := p.(pkg.CompletePackageInterface); ok {
		tree.description = cp.Description()
	}
	tree.requires = children

	return tree, nil
}

type namedLink struct {
	name string
	link *pkg.Link
}

// sortedLinks is ksort() of a link map.
func sortedLinks(links pkg.Links) []namedLink {
	a := php.NewArray()
	for i := range links.Len() {
		a.Set(links.Key(i), int64(i))
	}
	php.Ksort(a, 0)
	out := make([]namedLink, 0, a.Len())
	for k, v := range a.All() {
		i, _ := v.(int64)
		out = append(out, namedLink{k.String(), links.At(int(i))})
	}

	return out
}

// displayTree ports ShowCommand::displayTree.
func (c *ShowCommand) displayTree(p *treeNode, packagesInTree []string, previousTreeBar string, level int) {
	previousTreeBar = strings.ReplaceAll(previousTreeBar, "├", "│")
	treeBar := previousTreeBar + "  ├"
	total := len(p.requires)
	for i, require := range p.requires {
		currentTree := slices.Clone(packagesInTree)
		if i+1 == total {
			treeBar = previousTreeBar + "  └"
		}
		color := c.colors[level%len(c.colors)]

		circularWarn := ""
		if slices.Contains(currentTree, require.name) {
			circularWarn = "(circular dependency aborted here)"
		}
		info := php.Rtrim(treeBar + "──<" + color + ">" + require.name + "</" + color + "> " + require.version + " " + circularWarn)
		c.writeTreeLine(info)

		treeBar = strings.ReplaceAll(treeBar, "└", " ")

		currentTree = append(currentTree, require.name)
		c.displayTree(require, currentTree, treeBar, level+1)
	}
}

// addTree ports ShowCommand::addTree.
func (c *ShowCommand) addTree(name string, link *pkg.Link, installedRepo *repository.InstalledRepository, remoteRepos repository.RepositoryInterface, packagesInTree []string) ([]*treeNode, error) {
	var children []*treeNode
	prettyConstraint, err := link.PrettyConstraint()
	if err != nil {
		return nil, err
	}
	var ver any = prettyConstraint
	if prettyConstraint == "self.version" {
		ver = link.Constraint()
	}
	p, _, err := c.getPackage(installedRepo, remoteRepos, name, ver)
	if err != nil {
		return nil, err
	}
	if p != nil {
		for _, require := range sortedLinks(p.Requires()) {
			requireName := require.name
			currentTree := slices.Clone(packagesInTree)

			constraint, err := require.link.PrettyConstraint()
			if err != nil {
				return nil, err
			}
			treeChildDesc := &treeNode{name: requireName, version: constraint}

			if !slices.Contains(currentTree, requireName) {
				currentTree = append(currentTree, requireName)
				deepChildren, err := c.addTree(requireName, require.link, installedRepo, remoteRepos, currentTree)
				if err != nil {
					return nil, err
				}
				treeChildDesc.requires = deepChildren
			}

			children = append(children, treeChildDesc)
		}
	}

	return children, nil
}

func updateStatusToVersionStyle(updateStatus string) string {
	// 'up-to-date' is printed green
	// 'semver-safe-update' is printed red
	// 'update-possible' is printed yellow
	return strings.NewReplacer("up-to-date", "info", "semver-safe-update", "highlight", "update-possible", "comment").Replace(updateStatus)
}

func getVersionStyle(latestPackage, p pkg.PackageInterface) (string, error) {
	status, err := getUpdateStatus(latestPackage, p)

	return updateStatusToVersionStyle(status), err
}

func getUpdateStatus(latestPackage, p pkg.PackageInterface) (string, error) {
	if latestPackage.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev) == p.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev) {
		return "up-to-date", nil
	}

	constraint := p.Version()
	if !strings.HasPrefix(constraint, "dev-") {
		constraint = "^" + constraint
	}
	if latestPackage.Version() != "" && latestPackage.Version() != "0" {
		ok, err := semver.Semver.Satisfies(latestPackage.Version(), constraint)
		if err != nil {
			return "", err
		}
		if ok {
			// it needs an immediate semver-compliant upgrade
			return "semver-safe-update", nil
		}
	}

	// it needs an upgrade but has potential BC breaks so is not urgent
	return "update-possible", nil
}

func (c *ShowCommand) writeTreeLine(line string) {
	io := c.IO()
	if !io.IsDecorated() {
		line = strings.NewReplacer("└", "`-", "├", "|-", "──", "-", "│", "|").Replace(line)
	}

	io.Write(line, true, mio.Normal)
}

var (
	showMajorPattern = php.MustCompile(`{^(?P<zero_major>(?:0\.)+)?(?P<first_meaningful>\d+)\.}`)
	showTrailingZero = php.MustCompile(`{(\.0)+$}D`)
)

// findLatestPackage ports ShowCommand::findLatestPackage.
func (c *ShowCommand) findLatestPackage(p pkg.PackageInterface, comp *composer.Composer, platformRepo *repository.PlatformRepository, majorOnly, minorOnly, patchOnly bool, platformReqFilter version.PlatformRequirementFilter) (pkg.PackageInterface, error) {
	// find the latest version allowed in this repo set
	name := p.Name()
	repositorySet, err := c.getRepositorySet(comp)
	if err != nil {
		return nil, err
	}
	platformPackages, err := platformRepo.Packages()
	if err != nil {
		return nil, err
	}
	versionSelector := version.NewVersionSelector(repositorySet, platformPackages)
	stability := comp.Package().MinimumStability()
	flags := comp.Package().StabilityFlags()
	if flag, ok := flags.Get(name); ok {
		stability = ""
		for k, v := range pkg.Stabilities().All() {
			if php.StrictEquals(v, flag) {
				stability = k.String()

				break
			}
		}
	}

	bestStability := stability
	if comp.Package().PreferStable() {
		bestStability = p.Stability()
	}

	targetVersion := ""
	if strings.HasPrefix(p.Version(), "dev-") {
		targetVersion = p.Version()

		// dev-x branches are considered to be on the latest major version always, do not look up for a new commit as that is deemed a minor upgrade (albeit risky)
		if majorOnly {
			return nil, nil
		}
	}

	if targetVersion == "" {
		if majorOnly {
			m, err := showMajorPattern.Match(p.Version())
			if err != nil {
				return nil, err
			}
			if m != nil {
				fm, _ := m.Named("first_meaningful")
				zm, _ := m.Named("zero_major")
				firstMeaningful, _ := strconv.Atoi(fm)
				targetVersion = ">=" + zm + strconv.Itoa(firstMeaningful+1) + ",<9999999-dev"
			}
		}

		if minorOnly {
			targetVersion = "^" + p.Version()
		}

		if patchOnly {
			trimmedVersion, _, err := showTrailingZero.Replace(p.Version(), "", -1)
			if err != nil {
				return nil, err
			}
			partsNeeded := 3
			if strings.HasPrefix(trimmedVersion, "0") {
				partsNeeded = 4
			}
			for strings.Count(trimmedVersion, ".")+1 < partsNeeded {
				trimmedVersion += ".0"
			}
			targetVersion = "~" + trimmedVersion
		}
	}

	var showWarnings func(pkg.PackageInterface) bool
	if !c.IO().IsVerbose() {
		showWarnings = func(candidate pkg.PackageInterface) bool {
			if strings.HasPrefix(candidate.Version(), "dev-") || strings.HasPrefix(p.Version(), "dev-") {
				return false
			}

			return semver.VersionCompare(candidate.Version(), p.Version()) <= 0
		}
	}
	candidate, err := versionSelector.FindBestCandidate(name, version.FindBestCandidateOptions{
		TargetPackageVersion:      targetVersion,
		PreferredStability:        bestStability,
		PlatformRequirementFilter: platformReqFilter,
		IO:                        c.IO(),
		ShowWarnings:              showWarnings,
	})
	if err != nil {
		return nil, err
	}
	for {
		alias, ok := candidate.(pkg.Alias)
		if !ok {
			break
		}
		candidate = alias.AliasOf()
	}

	return candidate, nil
}

func (c *ShowCommand) getRepositorySet(comp *composer.Composer) (*repository.RepositorySet, error) {
	if c.repositorySet == nil {
		set, err := repository.NewRepositorySet(comp.Package().MinimumStability(), comp.Package().StabilityFlags(), nil, nil, nil, nil)
		if err != nil {
			return nil, err
		}
		composite, err := repository.NewCompositeRepository(comp.RepositoryManager().Repositories())
		if err != nil {
			return nil, err
		}
		if err := set.AddRepository(composite); err != nil {
			return nil, err
		}
		c.repositorySet = set
	}

	return c.repositorySet, nil
}
