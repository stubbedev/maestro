// Ports src/Composer/Command/BumpCommand.php.

package command

import (
	"os"
	"strconv"

	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util"
)

const bumpCommandFile = "BumpCommand.php"

// The BumpCommand exit codes.
const (
	bumpErrorGeneric      = 1
	bumpErrorLockOutdated = 2
)

func init() {
	registerCommand(OrderBump, func() console.Commander { return NewBumpCommand() })
}

// BumpCommand is Composer\Command\BumpCommand.
type BumpCommand struct{ *BaseCommand }

// ClassName implements console.ClassNamer.
func (*BumpCommand) ClassName() string { return `Composer\Command\BumpCommand` }

// NewBumpCommand ports new BumpCommand() (configure()).
func NewBumpCommand() *BumpCommand {
	c := &BumpCommand{BaseCommand: NewBaseCommand("")}
	c.SetImpl(c)
	c.SetName("bump").
		SetDescription("Increases the lower limit of your composer.json requirements to the currently installed versions").
		SetDefinitionItems(
			console.MustArgument("packages", console.ArgumentIsArray|console.ArgumentOptional, "Optional package name(s) to restrict which packages are bumped.", nil).WithSuggestFunc(c.SuggestRootRequirement()),
			console.MustOption("dev-only", "D", console.OptionValueNone, `Only bump requirements in "require-dev".`, nil),
			console.MustOption("no-dev-only", "R", console.OptionValueNone, `Only bump requirements in "require".`, nil),
			console.MustOption("dry-run", "", console.OptionValueNone, "Outputs the packages to bump, but will not execute anything.", nil),
		).
		SetHelp(`The <info>bump</info> command increases the lower limit of your composer.json requirements
to the currently installed versions. This helps to ensure your dependencies do not
accidentally get downgraded due to some other conflict, and can slightly improve
dependency resolution performance as it limits the amount of package versions
Composer has to look at.

Running this blindly on libraries is **NOT** recommended as it will narrow down
your allowed dependencies, which may cause dependency hell for your users.
Running it with <info>--dev-only</info> on libraries may be fine however as dev requirements
are local to the library and do not affect consumers of the package.
`)

	return c
}

// Execute ports execute().
func (c *BumpCommand) Execute(in console.Input, _ console.Output) (int, error) {
	return c.DoBump(
		c.IO(),
		console.BoolOption(in, "dev-only"),
		console.BoolOption(in, "no-dev-only"),
		console.BoolOption(in, "dry-run"),
		console.StringsArgument(in, "packages"),
		"--dev-only",
	)
}

// DoBump ports doBump (also called by UpdateCommand's --bump-after-update).
func (c *BumpCommand) DoBump(out io.IO, devOnly, noDevOnly, dryRun bool, packagesFilter []string, devOnlyFlagHint string) (int, error) {
	composerJSONPath, err := composer.GetComposerFile()
	if err != nil {
		return 0, err
	}

	if !util.IsReadable(composerJSONPath) {
		out.WriteError("<error>"+composerJSONPath+" is not readable.</error>", true, io.Normal)

		return bumpErrorGeneric, nil
	}

	composerJSON, err := json.NewFile(composerJSONPath, nil, nil)
	if err != nil {
		return 0, err
	}
	contents, err := os.ReadFile(composerJSON.Path())
	if err != nil {
		out.WriteError("<error>"+composerJSONPath+" is not readable.</error>", true, io.Normal)

		return bumpErrorGeneric, nil //nolint:nilerr // file_get_contents() returning false
	}

	// check for writability by writing to the file as is_writable can not be trusted on network-mounts
	// see https://github.com/composer/composer/issues/8231 and https://bugs.php.net/bug.php?id=68926
	if !util.IsWritable(composerJSONPath) && os.WriteFile(composerJSONPath, contents, 0o666) != nil {
		out.WriteError("<error>"+composerJSONPath+" is not writable.</error>", true, io.Normal)

		return bumpErrorGeneric, nil
	}

	c2, err := c.RequireComposer(nil, nil)
	if err != nil {
		return 0, err
	}
	cfg := c2.Config()
	lockCfg, err := cfg.Get("lock", 0)
	if err != nil {
		return 0, err
	}
	hasLockfileDisabled := !cfg.Has("lock") || php.ToBool(lockCfg)
	var repo repository.RepositoryInterface
	locker := c2.Locker()
	if !hasLockfileDisabled {
		if repo, err = lockedRepository(locker.LockedRepository(true)); err != nil {
			return 0, err
		}
	} else if locked, err := locker.IsLocked(); err != nil {
		return 0, err
	} else if locked {
		fresh, err := locker.IsFresh()
		if err != nil {
			return 0, err
		}
		if !fresh {
			out.WriteError("<error>The lock file is not up to date with the latest changes in composer.json. Run the appropriate `update` to fix that before you use the `bump` command.</error>", true, io.Normal)

			return bumpErrorLockOutdated, nil
		}

		if repo, err = lockedRepository(locker.LockedRepository(true)); err != nil {
			return 0, err
		}
	} else {
		repo = c2.RepositoryManager().LocalRepository()
	}

	root := c2.Package()
	if root.Type() != "project" && !devOnly {
		out.WriteError("<warning>Warning: Bumping dependency constraints is not recommended for libraries as it will narrow down your dependencies and may cause problems for your users.</warning>", true, io.Normal)

		read, err := composerJSON.Read()
		if err != nil {
			return 0, err
		}
		if a, _ := read.(*php.Array); a == nil || !arrayIsset(a, "type") {
			out.WriteError(`<warning>If your package is not a library, you can explicitly specify the "type" by using "composer config type project".</warning>`, true, io.Normal)
			out.WriteError("<warning>Alternatively you can use "+devOnlyFlagHint+` to only bump dependencies within "require-dev".</warning>`, true, io.Normal)
		}
	}

	type task struct {
		key  string
		reqs pkg.Links
	}
	var tasks []task
	if !devOnly {
		tasks = append(tasks, task{"require", root.Requires()})
	}
	if !noDevOnly {
		tasks = append(tasks, task{"require-dev", root.DevRequires()})
	}

	var pattern *php.Regexp
	if len(packagesFilter) > 0 {
		// support proxied args from the update command that contain constraints together with the package names
		names := make([]string, 0, len(packagesFilter))
		seen := map[string]bool{}
		for _, constraint := range packagesFilter {
			name, _, err := php.PregReplace(`{[:= ].+}`, "", constraint, -1)
			if err != nil {
				return 0, err
			}
			name = php.Strtolower(name)
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
		if pattern, err = php.Compile(pkg.PackageNamesToRegexp(names, "{^(?:%s)$}iD")); err != nil {
			return 0, err
		}
	}

	bumper := version.VersionBumper{}
	var updates []bumpUpdate
	for _, t := range tasks {
		for pkgName, link := range t.reqs.All() {
			if pattern != nil {
				ok, err := pattern.IsMatch(pkgName)
				if err != nil {
					return 0, err
				}
				if !ok {
					continue
				}
			}
			if pkg.IsPlatformPackage(pkgName) {
				continue
			}
			currentConstraint, err := link.PrettyConstraint()
			if err != nil {
				return 0, err
			}

			p, err := repo.FindPackage(pkgName, nil)
			if err != nil {
				return 0, err
			}
			// name must be provided or replaced
			if p == nil {
				continue
			}
			for {
				a, ok := p.(pkg.Alias)
				if !ok {
					break
				}
				p = a.AliasOf()
			}

			bumped, err := bumper.BumpRequirement(link.Constraint(), p)
			if err != nil {
				return 0, err
			}

			if bumped == currentConstraint {
				continue
			}

			updates = append(updates, bumpUpdate{t.key, pkgName, bumped})
		}
	}

	if !dryRun {
		ok, err := bumpUpdateFileCleanly(composerJSON, updates)
		if err != nil {
			return 0, err
		}
		if !ok {
			read, err := composerJSON.Read()
			if err != nil {
				return 0, err
			}
			composerDefinition, _ := read.(*php.Array)
			if composerDefinition == nil {
				composerDefinition = php.NewArray()
			}
			for _, u := range updates {
				arraySetNested(composerDefinition, u.key, u.pkg, u.version)
			}
			if err := composerJSON.Write(composerDefinition, json.DefaultEncodeFlags); err != nil {
				return 0, err
			}
		}
	}

	changeCount := len(updates)
	if changeCount > 0 {
		if dryRun {
			out.Write("<info>"+composerJSONPath+" would be updated with:</info>", true, io.Normal)
			for _, u := range updates {
				out.Write("<info> - "+u.key+"."+u.pkg+": "+u.version+"</info>", true, io.Normal)
			}
		} else {
			out.Write("<info>"+composerJSONPath+" has been updated ("+strconv.Itoa(changeCount)+" changes).</info>", true, io.Normal)
		}
	} else {
		out.Write("<info>No requirements to update in "+composerJSONPath+".</info>", true, io.Normal)
	}

	if !dryRun && changeCount > 0 {
		locked, err := locker.IsLocked()
		if err != nil {
			return 0, err
		}
		lockEnabled, err := cfg.Get("lock", 0)
		if err != nil {
			return 0, err
		}
		if locked && php.ToBool(lockEnabled) {
			if err := locker.UpdateHash(composerJSON.Path(), nil); err != nil {
				return 0, err
			}
		}
	}

	if dryRun && changeCount > 0 {
		return bumpErrorGeneric, nil
	}

	return 0, nil
}

// bumpUpdate is one $updates[$key][$package] = $version entry, in the
// order PHP's nested array iterates them.
type bumpUpdate struct{ key, pkg, version string }

// arrayIsset is isset($a[$key]).
func arrayIsset(a *php.Array, key string) bool {
	v, ok := a.Get(key)

	return ok && v != nil
}

// lockedRepository adapts Locker::getLockedRepository's result.
func lockedRepository(repo *repository.LockArrayRepository, err error) (repository.RepositoryInterface, error) {
	if err != nil {
		return nil, err
	}

	return repo, nil
}

// arraySetNested is $a[$k1][$k2] = $v.
func arraySetNested(a *php.Array, k1, k2 string, v any) {
	v1, _ := a.Get(k1)
	inner, ok := v1.(*php.Array)
	if !ok {
		inner = php.NewArray()
		a.Set(k1, inner)
	}
	inner.Set(k2, v)
}

// bumpUpdateFileCleanly ports updateFileCleanly.
func bumpUpdateFileCleanly(file *json.File, updates []bumpUpdate) (bool, error) {
	contents, err := os.ReadFile(file.Path())
	if err != nil {
		return false, NewError(ClassRuntime, bumpCommandFile, 245, "Unable to read "+file.Path()+" contents.")
	}

	manipulator, err := json.NewManipulator(string(contents))
	if err != nil {
		return false, err
	}

	for _, u := range updates {
		ok, err := manipulator.AddLink(u.key, u.pkg, u.version, false)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}

	if err := os.WriteFile(file.Path(), []byte(manipulator.Contents()), 0o666); err != nil {
		return false, NewError(ClassRuntime, bumpCommandFile, 259, "Unable to write new "+file.Path()+" contents.")
	}

	return true, nil
}
