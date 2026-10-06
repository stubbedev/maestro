// Ports src/Composer/Command/ArchiveCommand.php.

package command

import (
	"strings"

	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/script"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

func init() {
	registerCommand(OrderArchive, func() console.Commander { return NewArchiveCommand() })
}

// archiveFormats is ArchiveCommand::FORMATS.
var archiveFormats = []string{"tar", "tar.gz", "tar.bz2", "zip"}

// packageArchiver is the part of ArchiveManager the command calls.
type packageArchiver interface {
	Archive(p pkg.CompletePackageInterface, format, targetDir string, fileName pkg.NullString, ignoreFilters bool) (string, error)
}

// archiveArgs are ArchiveCommand::archive()'s arguments.
type archiveArgs struct {
	io            io.IO
	config        *config.Config
	packageName   pkg.NullString
	version       pkg.NullString
	format        string
	dest          string
	fileName      pkg.NullString
	ignoreFilters bool
	composer      *composer.Composer
}

// ArchiveCommand is Composer\Command\ArchiveCommand.
type ArchiveCommand struct {
	*BaseCommand

	// Overridable methods (PHPUnit mocks them in ArchiveCommandTest);
	// nil uses the real ones.
	tryComposerFunc     func() (*composer.Composer, error)
	requireComposerFunc func() (*composer.Composer, error)
	archiveFunc         func(a archiveArgs) (int, error)
	// archiveManagerOf is $composer->getArchiveManager().
	archiveManagerOf func(c *composer.Composer) packageArchiver
}

// NewArchiveCommand ports new ArchiveCommand() (configure()).
func NewArchiveCommand() *ArchiveCommand {
	c := &ArchiveCommand{BaseCommand: NewBaseCommand("")}
	c.SetImpl(c)
	c.SetName("archive")
	c.SetDescription("Creates an archive of this composer package")
	c.SetDefinitionItems(
		console.MustArgument("package", console.ArgumentOptional, "The package to archive instead of the current project", nil).WithSuggestFunc(c.SuggestAvailablePackage(99)),
		console.MustArgument("version", console.ArgumentOptional, "A version constraint to find the package to archive", nil),
		optionWithSuggestions("format", "f", console.OptionValueRequired, "Format of the resulting archive: tar, tar.gz, tar.bz2 or zip (default tar)", nil, archiveFormats...),
		console.MustOption("dir", "", console.OptionValueRequired, "Write the archive to this directory", nil),
		console.MustOption("file", "", console.OptionValueRequired, "Write the archive with the given file name. Note that the format will be appended.", nil),
		console.MustOption("ignore-filters", "", console.OptionValueNone, "Ignore filters when saving package", nil),
	)
	c.SetHelp(`The <info>archive</info> command creates an archive of the specified format
containing the files and directories of the Composer project or the specified
package in the specified version and writes it to the specified directory.

<info>php composer.phar archive [--format=zip] [--dir=/foo] [--file=filename] [package [version]]</info>

Read more at https://getcomposer.org/doc/03-cli.md#archive`)

	return c
}

// ClassName implements console.ClassNamer.
func (*ArchiveCommand) ClassName() string { return `Composer\Command\ArchiveCommand` }

func (c *ArchiveCommand) tryComposer() (*composer.Composer, error) {
	if c.tryComposerFunc != nil {
		return c.tryComposerFunc()
	}

	return c.TryComposer(nil, nil)
}

func (c *ArchiveCommand) requireComposer() (*composer.Composer, error) {
	if c.requireComposerFunc != nil {
		return c.requireComposerFunc()
	}

	return c.RequireComposer(nil, nil)
}

// nullStringArg is a nullable string argument or option.
func nullStringArg(v any) pkg.NullString {
	if s, ok := v.(string); ok {
		return pkg.Str(s)
	}

	return pkg.NullString{}
}

// Execute ports execute().
func (c *ArchiveCommand) Execute(in console.Input, out console.Output) (int, error) {
	comp, err := c.tryComposer()
	if err != nil {
		return 0, err
	}
	var cfg *config.Config

	if comp != nil {
		cfg = comp.Config()
		commandEvent := eventdispatcher.NewCommandEvent(eventdispatcher.PluginCommand, "archive", in, out, nil, nil)
		eventDispatcher := comp.EventDispatcher()
		if _, err := eventDispatcher.Dispatch(commandEvent.Name(), commandEvent); err != nil {
			return 0, err
		}
		if _, err := eventDispatcher.DispatchScript(script.PreArchiveCmd, false, nil, nil); err != nil {
			return 0, err
		}
	}

	if cfg == nil {
		if cfg, err = c.processFactory().CreateConfig(nil, ""); err != nil {
			return 0, err
		}
	}

	format, err := optionOrConfigString(in, "format", cfg, "archive-format")
	if err != nil {
		return 0, err
	}
	dir, err := optionOrConfigString(in, "dir", cfg, "archive-dir")
	if err != nil {
		return 0, err
	}

	archive := c.archive
	if c.archiveFunc != nil {
		archive = c.archiveFunc
	}
	returnCode, err := archive(archiveArgs{
		io:            c.IO(),
		config:        cfg,
		packageName:   nullStringArg(in.Argument("package")),
		version:       nullStringArg(in.Argument("version")),
		format:        format,
		dest:          dir,
		fileName:      nullStringArg(in.Option("file")),
		ignoreFilters: console.BoolOption(in, "ignore-filters"),
		composer:      comp,
	})
	if err != nil {
		return 0, err
	}

	if returnCode == 0 && comp != nil {
		if _, err := comp.EventDispatcher().DispatchScript(script.PostArchiveCmd, false, nil, nil); err != nil {
			return 0, err
		}
	}

	return returnCode, nil
}

// optionOrConfigString is `$input->getOption($name) ??
// $config->get($key)`.
func optionOrConfigString(in console.Input, name string, cfg *config.Config, key string) (string, error) {
	if v, ok := in.Option(name).(string); ok {
		return v, nil
	}
	v, err := cfg.Get(key, 0)
	if err != nil {
		return "", err
	}

	return php.ToString(v), nil
}

// archive ports archive().
func (c *ArchiveCommand) archive(a archiveArgs) (int, error) {
	var archiveManager packageArchiver
	switch {
	case a.composer != nil && c.archiveManagerOf != nil:
		archiveManager = c.archiveManagerOf(a.composer)
	case a.composer != nil:
		archiveManager = a.composer.ArchiveManager()
	default:
		factory := c.processFactory()
		process := util.NewProcessExecutor(nil)
		httpDownloader, err := factory.CreateHttpDownloader(a.io, a.config, nil)
		if err != nil {
			return 0, err
		}
		downloadManager, err := factory.CreateDownloadManager(a.io, a.config, httpDownloader, process, nil)
		if err != nil {
			return 0, err
		}
		archiveManager = factory.CreateArchiveManager(a.config, downloadManager, http.NewLoop(httpDownloader, process))
	}

	var p pkg.CompletePackageInterface
	if a.packageName.Valid && php.ToBool(a.packageName.S) {
		selected, err := c.selectPackage(a.io, a.packageName.S, a.version)
		if err != nil {
			return 0, err
		}
		if selected == nil {
			return 1, nil
		}
		p = selected
	} else {
		comp, err := c.requireComposer()
		if err != nil {
			return 0, err
		}
		p = comp.Package()
	}

	a.io.WriteError(`<info>Creating the archive into "`+a.dest+`".</info>`, true, io.Normal)
	packagePath, err := archiveManager.Archive(p, a.format, a.dest, a.fileName, a.ignoreFilters)
	if err != nil {
		return 0, err
	}
	cwd, err := util.GetCwd(false)
	if err != nil {
		return 0, err
	}
	shortPath, err := util.FindShortestPath(cwd, packagePath, true, false)
	if err != nil {
		return 0, err
	}

	a.io.WriteError("Created: ", false, io.Normal)
	if len(shortPath) < len(packagePath) {
		a.io.Write(shortPath, true, io.Normal)
	} else {
		a.io.Write(packagePath, true, io.Normal)
	}

	return 0, nil
}

var archiveStabilitySuffix = php.MustCompile(`{@(stable|RC|beta|alpha|dev)$}i`)

// selectPackage ports selectPackage: nil is PHP's false.
func (c *ArchiveCommand) selectPackage(out io.IO, packageName string, ver pkg.NullString) (pkg.CompletePackageInterface, error) {
	out.WriteError("<info>Searching for the specified package.</info>", true, io.Normal)

	var repo repository.RepositoryInterface
	var minStability string
	comp, err := c.tryComposer()
	if err != nil {
		return nil, err
	}
	if comp != nil {
		localRepo := comp.RepositoryManager().LocalRepository()
		if repo, err = repository.NewCompositeRepository(append([]repository.RepositoryInterface{localRepo}, comp.RepositoryManager().Repositories()...)); err != nil {
			return nil, err
		}
		minStability = comp.Package().MinimumStability()
	} else {
		defaultRepos, err := defaultReposWithDefaultManager(out, c.processFactory())
		if err != nil {
			return nil, err
		}
		out.WriteError("No composer.json found in the current directory, searching packages from "+strings.Join(repoMapNames(defaultRepos), ", "), true, io.Normal)
		if repo, err = repository.NewCompositeRepository(repoMapValues(defaultRepos)); err != nil {
			return nil, err
		}
		minStability = "stable"
	}

	if ver.Valid {
		// a fixed-length alternation anchored at the end cannot exceed the match limit
		if m, _ := archiveStabilitySuffix.MatchStrictGroups(ver.S); m != nil {
			if minStability, err = semver.NormalizeStability(m.Get(1)); err != nil {
				return nil, err
			}
			ver = pkg.Str(ver.S[:len(ver.S)-len(m.Get(0))])
		}
	}

	repoSet, err := repository.NewRepositorySet(minStability, nil, nil, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	if err := repoSet.AddRepository(repo); err != nil {
		return nil, err
	}
	parser := pkg.NewVersionParser()
	var constraint semver.ConstraintInterface
	if ver.Valid {
		if constraint, err = parser.ParseConstraints(ver.S); err != nil {
			return nil, err
		}
	}
	packages, err := repoSet.FindPackages(php.Strtolower(packageName), constraint, 0)
	if err != nil {
		return nil, err
	}

	var p pkg.PackageInterface
	switch {
	case len(packages) > 1:
		versionSelector := version.NewVersionSelector(repoSet, nil)
		if p, err = versionSelector.FindBestCandidate(php.Strtolower(packageName), version.FindBestCandidateOptions{
			TargetPackageVersion: ver.S,
			PreferredStability:   minStability,
		}); err != nil {
			return nil, err
		}
		if p == nil {
			p = packages[0]
		}

		out.WriteError("<info>Found multiple matches, selected "+p.PrettyString()+".</info>", true, io.Normal)
		alternatives := make([]string, len(packages))
		for i, alt := range packages {
			alternatives[i] = alt.PrettyString()
		}
		out.WriteError("Alternatives were "+strings.Join(alternatives, ", ")+".", true, io.Normal)
		out.WriteError("<comment>Please use a more specific constraint to pick a different package.</comment>", true, io.Normal)
	case len(packages) == 1:
		p = packages[0]
		out.WriteError("<info>Found an exact match "+p.PrettyString()+".</info>", true, io.Normal)
	default:
		out.WriteError("<error>Could not find a package matching "+packageName+".</error>", true, io.Normal)

		return nil, nil
	}

	cp, ok := p.(pkg.CompletePackageInterface)
	if !ok {
		return nil, NewError(ClassLogic, "ArchiveCommand.php", 204, "Expected a CompletePackageInterface instance but found "+p.Class())
	}
	// every Go package is a BasePackage (ArchiveCommand.php line 207)

	return cp, nil
}
