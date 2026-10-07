// Ports src/Composer/Command/InitCommand.php.

package command

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/repository/composerrepo"
	rvcs "github.com/stubbedev/maestro/internal/repository/vcs"
	"github.com/stubbedev/maestro/internal/spdx"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

func init() {
	registerCommand(OrderInit, func() console.Commander { return NewInitCommand() })
}

// InitCommand is Composer\Command\InitCommand.
type InitCommand struct {
	*BaseCommand
	PackageDiscovery

	// gitConfig is $gitConfig; nil is null.
	gitConfig map[string]string
}

// NewInitCommand ports new InitCommand() (configure()).
func NewInitCommand() *InitCommand {
	c := &InitCommand{BaseCommand: NewBaseCommand("")}
	c.PackageDiscovery = NewPackageDiscovery(c.BaseCommand)
	c.SetImpl(c)
	c.SetName("init")
	c.SetDescription("Creates a basic composer.json file in current directory")
	c.SetDefinitionItems(
		console.MustOption("name", "", console.OptionValueRequired, "Name of the package", nil),
		console.MustOption("description", "", console.OptionValueRequired, "Description of package", nil),
		console.MustOption("author", "", console.OptionValueRequired, "Author name of package", nil),
		console.MustOption("type", "", console.OptionValueRequired, "Type of package (e.g. library, project, metapackage, composer-plugin)", nil),
		console.MustOption("homepage", "", console.OptionValueRequired, "Homepage of package", nil),
		optionWithSuggestFunc("require", "", console.OptionValueIsArray|console.OptionValueRequired, `Package to require with a version constraint, e.g. foo/bar:1.0.0 or foo/bar=1.0.0 or "foo/bar 1.0.0"`, nil, c.SuggestAvailablePackageInclPlatform()),
		optionWithSuggestFunc("require-dev", "", console.OptionValueIsArray|console.OptionValueRequired, `Package to require for development with a version constraint, e.g. foo/bar:1.0.0 or foo/bar=1.0.0 or "foo/bar 1.0.0"`, nil, c.SuggestAvailablePackageInclPlatform()),
		console.MustOption("stability", "s", console.OptionValueRequired, "Minimum stability (empty or one of: "+strings.Join(pkg.StabilityNames(), ", ")+")", nil),
		console.MustOption("license", "l", console.OptionValueRequired, "License of package", nil),
		console.MustOption("repository", "", console.OptionValueRequired|console.OptionValueIsArray, "Add custom repositories, either by URL or using JSON arrays", nil),
		console.MustOption("autoload", "a", console.OptionValueRequired, "Add PSR-4 autoload mapping. Maps your package's namespace to the provided directory. (Expects a relative path, e.g. src/)", nil),
	)
	c.SetHelp(`The <info>init</info> command creates a basic composer.json file
in the current directory.

<info>php composer.phar init</info>

Read more at https://getcomposer.org/doc/03-cli.md#init`)

	return c
}

// ClassName implements console.ClassNamer.
func (*InitCommand) ClassName() string { return `Composer\Command\InitCommand` }

// packageNameRegexp is the package name check of execute() and interact().
var packageNameRegexp = php.MustCompile(`{^[a-z0-9]([_.-]?[a-z0-9]+)*\/[a-z0-9](([_.]|-{1,2})?[a-z0-9]+)*$}D`)

func invalidPackageNameError(name string) error {
	return NewError(ClassInvalidArgument, "The package name "+name+" is invalid, it should be lowercase and have a vendor name, a forward slash, and a package name, matching: [a-z0-9_.-]+/[a-z0-9_.-]+")
}

// initAllowlist are the options execute() turns into composer.json keys.
var initAllowlist = []string{"name", "description", "author", "type", "homepage", "require", "require-dev", "stability", "license", "autoload"}

// Execute ports execute().
func (c *InitCommand) Execute(in console.Input, out console.Output) (int, error) {
	cio := c.IO()

	// array_filter(array_intersect_key($input->getOptions(), array_flip($allowlist)), not null and not [])
	options := php.NewArray()
	for _, o := range in.Options() {
		if !slices.Contains(initAllowlist, o.Name) || o.Value == nil {
			continue
		}
		if len(toStringList(optionToPHP(o.Value))) == 0 {
			if _, isList := optionToPHP(o.Value).(*php.Array); isList {
				continue
			}
		}
		options.Set(o.Name, optionToPHP(o.Value))
	}

	if name, ok := options.Get("name"); ok {
		matched, err := packageNameRegexp.IsMatch(php.ToString(name))
		if err != nil {
			return 0, err
		}
		if !matched {
			return 0, invalidPackageNameError(php.ToString(name))
		}
	}

	if author, ok := options.Get("author"); ok {
		authors, err := c.formatAuthors(php.ToString(author))
		if err != nil {
			return 0, err
		}
		options.Delete("author")
		options.Set("authors", authors)
	}

	if repositories := console.StringsOption(in, "repository"); len(repositories) > 0 {
		cfg, err := c.factory().CreateConfig(cio, "")
		if err != nil {
			return 0, err
		}
		repos := php.NewArray()
		for _, repo := range repositories {
			repoConfig, err := repoConfigFromString(cio, c.factory(), cfg, repo)
			if err != nil {
				return 0, err
			}
			repos.Append(repoConfig)
		}
		options.Set("repositories", repos)
	}

	if stability, ok := options.Get("stability"); ok {
		options.Set("minimum-stability", stability)
		options.Delete("stability")
	}

	var require any = php.NewObject()
	if requires, ok := options.Get("require"); ok {
		formatted, err := c.FormatRequirements(toStringList(requires))
		if err != nil {
			return 0, err
		}
		if formatted.Len() > 0 {
			require = formatted
		}
	}
	options.Set("require", require)

	if requiresDev, ok := options.Get("require-dev"); ok {
		formatted, err := c.FormatRequirements(toStringList(requiresDev))
		if err != nil {
			return 0, err
		}
		if formatted.Len() == 0 {
			options.Set("require-dev", php.NewObject())
		} else {
			options.Set("require-dev", formatted)
		}
	}

	// --autoload - create autoload object
	var autoloadPath any
	if v, ok := options.Get("autoload"); ok {
		autoloadPath = v
		namespace := c.NamespaceFromPackageName(php.ToString(in.Option("name")))
		autoload := php.NewObject()
		autoload.Set("psr-4", php.ArrayOf(namespace+`\`, autoloadPath))
		options.Set("autoload", autoload)
	}

	composerFile, err := composer.GetComposerFile()
	if err != nil {
		return 0, err
	}
	file, err := json.NewFile(composerFile, nil, nil)
	if err != nil {
		return 0, err
	}
	encoded, err := json.EncodeDefault(options)
	if err != nil {
		return 0, err
	}

	if in.IsInteractive() {
		cio.WriteErrorMessages([]string{"", encoded, ""}, true, io.Normal)
		ok, err := cio.AskConfirmation("Do you confirm generation [<comment>yes</comment>]? ", true)
		if err != nil {
			return 0, err
		}
		if !ok {
			cio.WriteError("<error>Command aborted</error>", true, io.Normal)

			return 1, nil
		}
	} else {
		cio.WriteError("Writing "+file.Path(), true, io.Normal)
	}

	if err := file.Write(options, json.DefaultEncodeFlags); err != nil {
		return 0, err
	}
	if err := file.ValidateSchema(json.LaxSchema, ""); err != nil {
		ve, ok := errors.AsType[*json.ValidationError](err)
		if !ok {
			return 0, err
		}
		cio.WriteError("<error>Schema validation error, aborting</error>", true, io.Normal)
		errs := " - " + strings.Join(ve.Errors, php.EOL+" - ")
		cio.WriteError(ve.Message+":"+php.EOL+errs, true, io.Normal)
		_ = os.Remove(file.Path())

		return 1, nil
	}

	// --autoload - Create src folder
	if php.ToBool(autoloadPath) {
		if err := util.EnsureDirectoryExists(php.ToString(autoloadPath)); err != nil {
			return 0, err
		}

		// dump-autoload only for projects without added dependencies.
		if !hasDependencies(options) {
			c.runDumpAutoloadCommand(out)
		}
	}

	if in.IsInteractive() && php.IsDir(".git") {
		ignoreFile, ok := php.Realpath(".gitignore")
		if !ok {
			cwd, _ := php.Realpath(".")
			ignoreFile = cwd + "/.gitignore"
		}

		if !hasVendorIgnore(ignoreFile, "vendor") {
			question := "Would you like the <info>vendor</info> directory added to your <info>.gitignore</info> [<comment>yes</comment>]? "

			ok, err := cio.AskConfirmation(question, true)
			if err != nil {
				return 0, err
			}
			if ok {
				addVendorIgnore(ignoreFile, "/vendor/")
			}
		}
	}

	question := "Would you like to install dependencies now [<comment>yes</comment>]? "
	if in.IsInteractive() && hasDependencies(options) {
		ok, err := cio.AskConfirmation(question, true)
		if err != nil {
			return 0, err
		}
		if ok {
			c.updateDependencies(out)
		}
	}

	// --autoload - Show post-install configuration info
	if php.ToBool(autoloadPath) {
		namespace := c.NamespaceFromPackageName(php.ToString(in.Option("name")))

		cio.WriteError(`PSR-4 autoloading configured. Use "<comment>namespace `+namespace+`;</comment>" in `+php.ToString(autoloadPath), true, io.Normal)
		cio.WriteError(`Include the Composer autoloader with: <comment>require 'vendor/autoload.php';</comment>`, true, io.Normal)
	}

	return 0, nil
}

// optionToPHP is an option value as PHP holds it: lists become arrays.
func optionToPHP(v any) any {
	switch l := v.(type) {
	case []string:
		a := php.NewArray()
		for _, s := range l {
			a.Append(s)
		}

		return a
	case []any:
		return php.ListOf(l...)
	}

	return v
}

// toStringList is a string-list value of options.
func toStringList(v any) []string {
	a, ok := v.(*php.Array)
	if !ok {
		return nil
	}
	out := make([]string, 0, a.Len())
	for _, s := range a.All() {
		out = append(out, php.ToString(s))
	}

	return out
}

// Initialize ports initialize().
func (c *InitCommand) Initialize(in console.Input, out console.Output) error {
	if err := c.BaseCommand.Initialize(in, out); err != nil {
		return err
	}

	if !in.IsInteractive() {
		if in.Option("name") == nil {
			in.SetOption("name", c.defaultPackageName())
		}

		if in.Option("author") == nil {
			if author := c.defaultAuthor(); author != nil {
				in.SetOption("author", *author)
			}
		}
	}

	return nil
}

// Interact ports interact().
func (c *InitCommand) Interact(in console.Input, out console.Output) error {
	cio := c.IO()
	formatter := &console.FormatterHelper{}

	// initialize repos if configured
	if repositories := console.StringsOption(in, "repository"); len(repositories) > 0 {
		if err := c.initRepositories(cio, repositories); err != nil {
			return err
		}
	}

	cio.WriteErrorMessages([]string{
		"",
		formatter.FormatBlock([]string{"Welcome to the Composer config generator"}, "bg=blue;fg=white", true),
		"",
	}, true, io.Normal)

	// namespace
	cio.WriteErrorMessages([]string{
		"",
		"This command will guide you through creating your composer.json config.",
		"",
	}, true, io.Normal)

	name := php.ToString(in.Option("name"))
	if in.Option("name") == nil {
		name = c.defaultPackageName()
	}

	answer, err := cio.AskAndValidate(
		"Package name (<vendor>/<name>) [<comment>"+name+"</comment>]: ",
		func(value any) (any, error) {
			if value == nil {
				return name, nil
			}

			matched, err := packageNameRegexp.IsMatch(php.ToString(value))
			if err != nil {
				return nil, err
			}
			if !matched {
				return nil, invalidPackageNameError(php.ToString(value))
			}

			return value, nil
		},
		0,
		name,
	)
	if err != nil {
		return err
	}
	in.SetOption("name", answer)

	var description any
	if v := in.Option("description"); php.ToBool(v) {
		description = v
	}
	if description, err = cio.Ask("Description [<comment>"+php.ToString(description)+"</comment>]: ", description); err != nil {
		return err
	}
	in.SetOption("description", description)

	author := in.Option("author")
	if author == nil {
		if a := c.defaultAuthor(); a != nil {
			author = *a
		}
	}

	authorPrompt := "Author ["
	if s, ok := author.(string); ok {
		authorPrompt += "<comment>" + s + "</comment>, "
	}
	authorPrompt += "n to skip]: "
	if author, err = cio.AskAndValidate(
		authorPrompt,
		func(value any) (any, error) {
			if value == "n" || value == "no" {
				return nil, nil
			}
			if !php.ToBool(value) {
				value = author
			}
			parsed, err := c.parseAuthorString(php.ToString(value))
			if err != nil {
				return nil, err
			}

			if parsed.email == nil {
				return parsed.name, nil
			}

			return parsed.name + " <" + *parsed.email + ">", nil
		},
		0,
		author,
	); err != nil {
		return err
	}
	in.SetOption("author", author)

	var minimumStability any
	if v := in.Option("stability"); php.ToBool(v) {
		minimumStability = v
	}
	if minimumStability, err = cio.AskAndValidate(
		"Minimum Stability [<comment>"+php.ToString(minimumStability)+"</comment>]: ",
		func(value any) (any, error) {
			if value == nil {
				return minimumStability, nil
			}

			if _, ok := pkg.StabilityValue(php.ToString(value)); !ok {
				return nil, NewError(ClassInvalidArgument, `Invalid minimum stability "`+php.ToString(value)+`". Must be empty or one of: `+strings.Join(pkg.StabilityNames(), ", "))
			}

			return value, nil
		},
		0,
		minimumStability,
	); err != nil {
		return err
	}
	in.SetOption("stability", minimumStability)

	typ := in.Option("type")
	if typ, err = cio.Ask("Package Type (e.g. library, project, metapackage, composer-plugin) [<comment>"+php.ToString(typ)+"</comment>]: ", typ); err != nil {
		return err
	}
	if typ == "" || typ == false {
		typ = nil
	}
	in.SetOption("type", typ)

	license := in.Option("license")
	if license == nil {
		if v := os.Getenv("COMPOSER_DEFAULT_LICENSE"); php.ToBool(v) {
			license = v
		}
	}

	if license, err = cio.Ask("License [<comment>"+php.ToString(license)+"</comment>]: ", license); err != nil {
		return err
	}
	if license != nil && !spdx.New().Validate(php.ToString(license)) && license != "proprietary" {
		return NewError(ClassInvalidArgument, "Invalid license provided: "+php.ToString(license)+`. Only SPDX license identifiers (https://spdx.org/licenses/) or "proprietary" are accepted.`)
	}
	in.SetOption("license", license)

	cio.WriteErrorMessages([]string{"", "Define your dependencies.", ""}, true, io.Normal)

	// prepare to resolve dependencies
	repos, err := c.Repos()
	if err != nil {
		return err
	}
	preferredStability := php.ToString(minimumStability)
	if !php.ToBool(minimumStability) {
		preferredStability = "stable"
	}
	var platformRepo *repository.PlatformRepository
	for _, candidateRepo := range repos.Repositories() {
		if p, ok := candidateRepo.(*repository.PlatformRepository); ok {
			platformRepo = p

			break
		}
	}

	question := "Would you like to define your dependencies (require) interactively [<comment>yes</comment>]? "
	require := console.StringsOption(in, "require")
	requirements := []string{}
	ok := len(require) > 0
	if !ok {
		if ok, err = cio.AskConfirmation(question, true); err != nil {
			return err
		}
	}
	if ok {
		if requirements, err = c.DetermineRequirements(in, out, require, platformRepo, preferredStability, false, false); err != nil {
			return err
		}
	}
	in.SetOption("require", stringsToAny(requirements))

	question = "Would you like to define your dev dependencies (require-dev) interactively [<comment>yes</comment>]? "
	requireDev := console.StringsOption(in, "require-dev")
	devRequirements := []string{}
	ok = len(requireDev) > 0
	if !ok {
		if ok, err = cio.AskConfirmation(question, true); err != nil {
			return err
		}
	}
	if ok {
		if devRequirements, err = c.DetermineRequirements(in, out, requireDev, platformRepo, preferredStability, false, false); err != nil {
			return err
		}
	}
	in.SetOption("require-dev", stringsToAny(devRequirements))

	// --autoload - input and validation
	autoload := in.Option("autoload")
	if !php.ToBool(autoload) {
		autoload = "src/"
	}
	namespace := c.NamespaceFromPackageName(php.ToString(in.Option("name")))
	if autoload, err = cio.AskAndValidate(
		`Add PSR-4 autoload mapping? Maps namespace "`+namespace+`" to the entered relative path. [<comment>`+php.ToString(autoload)+`</comment>, n to skip]: `,
		func(value any) (any, error) {
			if value == nil {
				return autoload, nil
			}

			if value == "n" || value == "no" {
				return nil, nil
			}

			if !php.ToBool(value) {
				value = autoload
			}

			matched, err := autoloadPathRegexp.IsMatch(php.ToString(value))
			if err != nil {
				return nil, err
			}
			if !matched {
				return nil, NewError(ClassInvalidArgument, `The src folder name "`+php.ToString(value)+`" is invalid. Please add a relative path with tailing forward slash. [A-Za-z0-9_-/]+/`)
			}

			return value, nil
		},
		0,
		autoload,
	); err != nil {
		return err
	}
	in.SetOption("autoload", autoload)

	return nil
}

var autoloadPathRegexp = php.MustCompile(`{^[^/][A-Za-z0-9\-_/]+/$}`)

func stringsToAny(list []string) []any {
	out := make([]any, len(list))
	for i, s := range list {
		out[i] = s
	}

	return out
}

// initRepositories is interact()'s "initialize repos if configured"
// block: the --repository repositories, the platform and Packagist
// (unless disabled) become the repositories of the PackageDiscoveryTrait.
func (c *InitCommand) initRepositories(cio io.IO, repositories []string) error {
	factory := c.factory()
	cfg, err := factory.CreateConfig(cio, "")
	if err != nil {
		return err
	}
	if err := cio.LoadConfiguration(cfg.ForIO(), util.SetProcessTimeout); err != nil {
		return err
	}

	// RepositoryFactory::manager($io, $config)
	httpDownloader, err := factory.CreateHttpDownloader(cio, cfg, nil)
	if err != nil {
		return err
	}
	process := http.NewProcessExecutor(cio)
	process.EnableAsync()
	repoManager := repository.Manager(cio, cfg, httpDownloader, nil, process, repository.ExternalTypes{
		Composer: composerrepo.Constructor,
		VCS:      rvcs.NewRepository,
	})

	platformOptions, err := factory.Runtime.PlatformOptions(process)
	if err != nil {
		return err
	}
	platformRepo, err := repository.NewPlatformRepository(nil, nil, platformOptions)
	if err != nil {
		return err
	}
	repos := []repository.RepositoryInterface{platformRepo}
	createDefaultPackagistRepo := true
	for _, repo := range repositories {
		repoConfig, err := repository.ConfigFromString(repo, true, httpDownloader) // configFromString's own HttpDownloader is equivalent
		if err != nil {
			return err
		}
		if isPackagistDisabledConfig(repoConfig) {
			createDefaultPackagistRepo = false

			continue
		}
		created, err := repository.CreateRepo(repoConfig, repoManager)
		if err != nil {
			return err
		}
		repos = append(repos, created)
	}

	if createDefaultPackagistRepo {
		created, err := repository.CreateRepo(php.ArrayOf(
			"type", "composer",
			"url", "https://repo.packagist.org",
		), repoManager)
		if err != nil {
			return err
		}
		repos = append(repos, created)
	}

	composite, err := repository.NewCompositeRepository(repos)
	if err != nil {
		return err
	}
	c.repos = composite

	return nil
}

// repoConfigFromString is RepositoryFactory::configFromString($io,
// $config, $repo, true): a .json file is read with an HttpDownloader
// created from io and cfg.
func repoConfigFromString(out io.IO, factory *composer.Factory, cfg *config.Config, repo string) (any, error) {
	var httpDownloader *http.HttpDownloader
	if filepath.Ext(repo) == ".json" {
		var err error
		if httpDownloader, err = factory.CreateHttpDownloader(out, cfg, nil); err != nil {
			return nil, err
		}
	}

	return repository.ConfigFromString(repo, true, httpDownloader)
}

// authorString is parseAuthorString's array{name: string, email: string|null}.
type authorString struct {
	name  string
	email *string
}

var authorRegexp = php.MustCompile(`/^(?P<name>[- .,\p{L}\p{N}\p{Mn}'’"()]+)(?:\s+<(?P<email>.+?)>)?$/u`)

// parseAuthorString ports parseAuthorString.
func (c *InitCommand) parseAuthorString(author string) (authorString, error) {
	m, err := authorRegexp.Match(author)
	if err != nil {
		return authorString{}, err
	}
	if m != nil {
		var email *string
		if e, ok := m.Named("email"); ok {
			email = &e
		}
		if email != nil && !isValidEmail(*email) {
			return authorString{}, NewError(ClassInvalidArgument, `Invalid email "`+*email+`"`)
		}

		return authorString{name: strings.TrimSpace(m.Get(1)), email: email}, nil
	}

	return authorString{}, NewError(ClassInvalidArgument, "Invalid author string.  Must be in the formats: Jane Doe or John Smith <john@example.com>")
}

// formatAuthors ports formatAuthors.
func (c *InitCommand) formatAuthors(author string) (*php.Array, error) {
	parsed, err := c.parseAuthorString(author)
	if err != nil {
		return nil, err
	}
	entry := php.ArrayOf("name", parsed.name)
	if parsed.email != nil {
		entry.Set("email", *parsed.email)
	}

	return php.ListOf(entry), nil
}

var nonAlnum = php.MustCompile(`/[^a-z0-9]/i`)

// NamespaceFromPackageName ports namespaceFromPackageName ("" for null):
// new_projects.acme-extra/package-name becomes
// "NewProjectsAcmeExtra\PackageName".
func (*InitCommand) NamespaceFromPackageName(packageName string) string {
	if !php.ToBool(packageName) || !strings.Contains(packageName, "/") {
		return ""
	}

	parts := strings.Split(packageName, "/")
	for i, part := range parts {
		// A single character class: Preg::replace cannot fail here.
		part, _, _ = nonAlnum.Replace(part, " ", -1)
		part = php.Ucwords(part, php.UcwordsDelimiters)
		parts[i] = strings.ReplaceAll(part, " ", "")
	}

	return strings.Join(parts, `\`)
}

var gitConfigLine = php.MustCompile(`{^([^=]+)=(.*)$}m`)

// getGitConfig ports getGitConfig.
func (c *InitCommand) getGitConfig() map[string]string {
	if c.gitConfig != nil {
		return c.gitConfig
	}

	process := http.NewProcessExecutor(c.IO())

	var output string
	if code, err := process.Execute(util.Cmd("git", "config", "-l"), &output, ""); err == nil && code == 0 {
		c.gitConfig = map[string]string{}
		matches, _ := gitConfigLine.MatchAll(output)
		for _, m := range matches {
			c.gitConfig[m.Get(1)] = m.Get(2)
		}

		return c.gitConfig
	}

	c.gitConfig = map[string]string{}

	return c.gitConfig
}

// hasVendorIgnore ports hasVendorIgnore: whether the .gitignore file
// ignores the vendor directory ("/$vendor", "$vendor", "$vendor/",
// "/$vendor/", "/$vendor/*", "$vendor/*").
func hasVendorIgnore(ignoreFile, vendor string) bool {
	data, err := os.ReadFile(ignoreFile)
	if err != nil {
		return false
	}

	pattern := php.MustCompile(`{^/?` + php.PregQuote(vendor, "") + `(/\*?)?$}`)

	for _, line := range phpFileLines(string(data)) {
		if ok, _ := pattern.IsMatch(line); ok {
			return true
		}
	}

	return false
}

// phpFileLines is file($path, FILE_IGNORE_NEW_LINES).
func phpFileLines(data string) []string {
	if data == "" {
		return nil
	}
	lines := strings.Split(data, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return lines
}

// addVendorIgnore ports addVendorIgnore.
func addVendorIgnore(ignoreFile, vendor string) {
	contents := ""
	if data, err := os.ReadFile(ignoreFile); err == nil {
		contents = string(data)

		if !strings.HasPrefix(contents, "\n") {
			contents += "\n"
		}
	}

	_ = os.WriteFile(ignoreFile, []byte(contents+vendor+"\n"), 0o666)
}

// isValidEmail ports isValidEmail.
func isValidEmail(email string) bool {
	return util.FilterValidateEmail(email)
}

// updateDependencies ports updateDependencies.
func (c *InitCommand) updateDependencies(out console.Output) {
	if err := c.runCommand("update", out); err != nil {
		c.IO().WriteError("Could not update dependencies. Run `composer update` to see more information.", true, io.Normal)
	}
}

// runDumpAutoloadCommand ports runDumpAutoloadCommand.
func (c *InitCommand) runDumpAutoloadCommand(out console.Output) {
	if err := c.runCommand("dump-autoload", out); err != nil {
		c.IO().WriteError("Could not run dump-autoload.", true, io.Normal)
	}
}

// runCommand is `$this->getApplication()->find($name)`,
// resetComposer() and `$command->run(new ArrayInput([]), $output)`.
func (c *InitCommand) runCommand(name string, out console.Output) error {
	app, err := c.App()
	if err != nil {
		return err
	}
	command, err := app.Find(name)
	if err != nil {
		return err
	}
	app.ResetComposer()
	in, err := console.NewArrayInput(nil, nil)
	if err != nil {
		return err
	}
	_, err = command.Run(in, out)

	return err
}

// hasDependencies ports hasDependencies.
func hasDependencies(options *php.Array) bool {
	count := func(v any) int {
		switch t := v.(type) {
		case *php.Array:
			return t.Len()
		case *php.Object:
			return t.Len()
		}

		return 0
	}
	requires, _ := options.Get("require")
	devRequires, _ := options.Get("require-dev")

	return count(requires) > 0 || count(devRequires) > 0
}

var (
	packageNameCamel   = php.MustCompile(`{(?:([a-z])([A-Z])|([A-Z])([A-Z][a-z]))}`)
	packageNameTrim    = php.MustCompile(`{^[_.-]+|[_.-]+$|[^a-z0-9_.-]}u`)
	packageNameRepeats = php.MustCompile(`{([_.-]){2,}}u`)
)

// sanitizePackageNameComponent ports sanitizePackageNameComponent.
func sanitizePackageNameComponent(name string) string {
	// Fixed-width alternatives and single classes: these cannot fail on
	// any input (and the input is a directory or user name).
	name, _, _ = packageNameCamel.Replace(name, `\1\3-\2\4`, -1)
	name = php.Strtolower(name)
	name, _, _ = packageNameTrim.Replace(name, "", -1)
	name, _, _ = packageNameRepeats.Replace(name, "$1", -1)

	return name
}

// defaultPackageName ports getDefaultPackageName.
func (c *InitCommand) defaultPackageName() string {
	git := c.getGitConfig()
	cwd, _ := php.Realpath(".")
	name := sanitizePackageNameComponent(filepath.Base(cwd))

	vendor := name
	if v := os.Getenv("COMPOSER_DEFAULT_VENDOR"); php.ToBool(v) {
		vendor = v
	} else if v, ok := git["github.user"]; ok {
		vendor = v
	} else if v := os.Getenv("USERNAME"); php.ToBool(v) {
		vendor = v
	} else if v := os.Getenv("USER"); php.ToBool(v) {
		vendor = v
	} else if v := currentUser(); php.ToBool(v) {
		vendor = v
	}

	return sanitizePackageNameComponent(vendor) + "/" + name
}

// defaultAuthor ports getDefaultAuthor (nil for null).
func (c *InitCommand) defaultAuthor() *string {
	git := c.getGitConfig()

	var authorName, authorEmail *string
	if v := os.Getenv("COMPOSER_DEFAULT_AUTHOR"); php.ToBool(v) {
		authorName = &v
	} else if v, ok := git["user.name"]; ok {
		authorName = &v
	}

	if v := os.Getenv("COMPOSER_DEFAULT_EMAIL"); php.ToBool(v) {
		authorEmail = &v
	} else if v, ok := git["user.email"]; ok {
		authorEmail = &v
	}

	if authorName != nil && authorEmail != nil {
		s := *authorName + " <" + *authorEmail + ">"

		return &s
	}

	return nil
}
