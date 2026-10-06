// Ports src/Composer/Command/HomeCommand.php.

package command

import (
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util"
)

func init() {
	registerCommand(OrderHome, func() console.Commander { return NewHomeCommand() })
}

// HomeCommand is Composer\Command\HomeCommand.
type HomeCommand struct{ *BaseCommand }

// NewHomeCommand ports new HomeCommand() (configure()).
func NewHomeCommand() *HomeCommand {
	c := &HomeCommand{BaseCommand: NewBaseCommand("")}
	c.SetImpl(c)
	c.SetName("browse")
	c.SetAliases("home")
	c.SetDescription("Opens the package's repository URL or homepage in your browser")
	c.SetDefinitionItems(
		console.MustArgument("packages", console.ArgumentIsArray, "Package(s) to browse to.", nil).WithSuggestFunc(c.SuggestInstalledPackage(true, false)),
		console.MustOption("homepage", "H", console.OptionValueNone, "Open the homepage instead of the repository URL.", nil),
		console.MustOption("show", "s", console.OptionValueNone, "Only show the homepage or repository URL.", nil),
	)
	c.SetHelp(`The home command opens or shows a package's repository URL or
homepage in your default browser.

To open the homepage by default, use -H or --homepage.
To show instead of open the repository or homepage URL, use -s or --show.

Read more at https://getcomposer.org/doc/03-cli.md#browse-home`)

	return c
}

// ClassName implements console.ClassNamer.
func (*HomeCommand) ClassName() string { return `Composer\Command\HomeCommand` }

// Execute ports execute().
func (c *HomeCommand) Execute(in console.Input, _ console.Output) (int, error) {
	repos, err := c.initializeRepos()
	if err != nil {
		return 0, err
	}
	cio := c.IO()
	ret := 0

	packages := console.StringsArgument(in, "packages")
	if len(packages) == 0 {
		cio.WriteError("No package specified, opening homepage for the root package", true, io.Normal)
		comp, err := c.RequireComposer(nil, nil)
		if err != nil {
			return 0, err
		}
		packages = []string{comp.Package().Name()}
	}

	showHomepage := console.BoolOption(in, "homepage")
	showOnly := console.BoolOption(in, "show")
	for _, packageName := range packages {
		handled := false
		packageExists := false
	repos:
		for _, repo := range repos {
			found, err := repo.FindPackages(packageName, nil)
			if err != nil {
				return 0, err
			}
			for _, p := range found {
				packageExists = true
				if cp, ok := p.(pkg.CompletePackageInterface); ok {
					if c.handlePackage(cp, showHomepage, showOnly) {
						handled = true

						break repos
					}
				}
			}
		}

		if !packageExists {
			ret = 1
			cio.WriteError("<warning>Package "+packageName+" not found</warning>", true, io.Normal)
		}

		if !handled {
			ret = 1
			what := "Invalid or missing repository URL"
			if showHomepage {
				what = "Invalid or missing homepage"
			}
			cio.WriteError("<warning>"+what+" for "+packageName+"</warning>", true, io.Normal)
		}
	}

	return ret, nil
}

// handlePackage ports HomeCommand::handlePackage.
func (c *HomeCommand) handlePackage(p pkg.CompletePackageInterface, showHomepage, showOnly bool) bool {
	var url any
	if support := p.Support(); support != nil {
		url, _ = support.Get("source")
	}
	if url == nil {
		if s := p.SourceURL(); s.Valid {
			url = s.S
		}
	}
	if !php.ToBool(url) || showHomepage {
		url = nil
		if h := p.Homepage(); h.Valid {
			url = h.S
		}
	}

	if !php.ToBool(url) || !util.FilterValidateURL(php.ToString(url)) {
		return false
	}

	if showOnly {
		c.IO().Write("<info>"+php.ToString(url)+"</info>", true, io.Normal)
	} else {
		c.openBrowser(php.ToString(url))
	}

	return true
}

// openBrowser ports HomeCommand::openBrowser: it opens a url in the
// system default browser.
func (c *HomeCommand) openBrowser(url string) {
	process := util.NewProcessExecutor(c.IO())
	var output string
	if util.IsWindows() {
		_, _ = process.Execute(util.Cmd("start", `"web"`, "explorer", url), &output, "")

		return
	}

	linux, _ := process.Execute(util.Cmd("which", "xdg-open"), &output, "")
	osx, _ := process.Execute(util.Cmd("which", "open"), &output, "")

	switch {
	case linux == 0:
		_, _ = process.Execute(util.Cmd("xdg-open", url), &output, "")
	case osx == 0:
		_, _ = process.Execute(util.Cmd("open", url), &output, "")
	default:
		c.IO().WriteError("No suitable browser opening command found, open yourself: "+url, true, io.Normal)
	}
}

// initializeRepos ports HomeCommand::initializeRepos: the repositories in
// the order they are checked.
func (c *HomeCommand) initializeRepos() ([]repository.RepositoryInterface, error) {
	comp, err := c.TryComposer(nil, nil)
	if err != nil {
		return nil, err
	}

	if comp != nil {
		root, _ := pkg.Clone(comp.Package()).(pkg.RootPackageInterface)
		rootRepo, err := repository.NewRootPackageRepository(root)
		if err != nil {
			return nil, err
		}

		return append([]repository.RepositoryInterface{
			rootRepo, // root package
			comp.RepositoryManager().LocalRepository(), // installed packages
		}, comp.RepositoryManager().Repositories()...), nil // remotes
	}

	defaultRepos, err := defaultReposWithDefaultManager(c.IO(), c.processFactory())
	if err != nil {
		return nil, err
	}

	return repoMapValues(defaultRepos), nil
}
