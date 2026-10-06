// Ports src/Composer/Command/RepositoryCommand.php.

package command

import (
	"strings"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
)

func init() {
	registerCommand(OrderRepository, func() console.Commander { return NewRepositoryCommand() })
}

// RepositoryCommand is Composer\Command\RepositoryCommand.
type RepositoryCommand struct{ *BaseConfigCommand }

func mustOpt(o *console.InputOption, err error) *console.InputOption {
	if err != nil {
		panic(err)
	}

	return o
}

// NewRepositoryCommand ports new RepositoryCommand().
func NewRepositoryCommand() *RepositoryCommand {
	c := &RepositoryCommand{BaseConfigCommand: NewBaseConfigCommand("")}
	c.SetImpl(c)
	c.SetName("repository").
		SetAliases("repo").
		SetDescription("Manages repositories").
		SetDefinitionItems(
			console.MustOption("global", "g", console.OptionValueNone, "Apply command to the global config file", nil),
			console.MustOption("file", "f", console.OptionValueRequired, "If you want to choose a different composer.json or config.json", nil),
			console.MustOption("append", "", console.OptionValueNone, "When adding a repository, append it (lower priority) instead of prepending it", nil),
			mustOpt(console.MustOption("before", "", console.OptionValueRequired, "When adding a repository, insert it before the given repository name", nil).WithSuggestFunc(c.suggestRepoNames())),
			mustOpt(console.MustOption("after", "", console.OptionValueRequired, "When adding a repository, insert it after the given repository name", nil).WithSuggestFunc(c.suggestRepoNames())),
			console.MustArgument("action", console.ArgumentOptional, "Action to perform: list, add, remove, set-url, get-url, enable, disable", "list").WithSuggestedValues("list", "add", "remove", "set-url", "get-url", "enable", "disable"),
			console.MustArgument("name", console.ArgumentOptional, "Repository name (or special name packagist.org for enable/disable)", nil).WithSuggestFunc(c.suggestRepoNames()),
			console.MustArgument("arg1", console.ArgumentOptional, "Type for add, or new URL for set-url, or JSON config for add", nil).WithSuggestFunc(suggestTypeForAdd),
			console.MustArgument("arg2", console.ArgumentOptional, "URL for add (if not using JSON)", nil),
		).
		SetHelp(`This command lets you manage repositories in your composer.json.

Examples:
  composer repo list
  composer repo add foo vcs https://github.com/acme/foo
  composer repo add bar composer https://repo.packagist.com/bar
  composer repo add zips '{"type":"artifact","url":"/path/to/dir/with/zips"}'
  composer repo add baz vcs https://example.org --before foo
  composer repo add qux vcs https://example.org --after bar
  composer repo remove foo
  composer repo set-url foo https://git.example.org/acme/foo
  composer repo get-url foo
  composer repo disable packagist.org
  composer repo enable packagist.org

Use --global/-g to alter the global config.json instead.
Use --file to alter a specific file.`)

	return c
}

// ClassName implements console.ClassNamer.
func (*RepositoryCommand) ClassName() string { return `Composer\Command\RepositoryCommand` }

var jsonConfigStart = php.MustCompile(`{^\s*\{}`)

func isPackagist(name any) bool { return name == "packagist" || name == "packagist.org" }

// Execute implements console.Executor.
func (c *RepositoryCommand) Execute(in console.Input, _ console.Output) (int, error) {
	action := strings.ToLower(php.ToString(in.Argument("action")))
	name := in.Argument("name")
	arg1 := in.Argument("arg1")
	arg2 := in.Argument("arg2")

	data, err := c.ConfigFile.Read()
	if err != nil {
		return 0, err
	}
	dataArr, ok := data.(*php.Array)
	if !ok {
		return 0, pkg.ArgumentTypeError(`Composer\Config::merge`, 1, "config", "array", data)
	}
	if err := c.Config.Merge(dataArr, c.ConfigFile.Path()); err != nil {
		return 0, err
	}
	repos := c.Config.Repositories()

	runtimeErr := func(msg string) (int, error) {
		return 0, NewError(ClassRuntime, msg)
	}

	switch action {
	case "list", "ls", "show":
		c.listRepositories(repos.Clone())

		return 0, nil

	case "add":
		if name == nil {
			return runtimeErr("You must pass a repository name. Example: composer repo add foo vcs https://example.org")
		}
		if arg1 == nil {
			return runtimeErr("You must pass the type and a url, or a JSON string.")
		}
		var repoConfig any
		s, isString := arg1.(string)
		isJSON := false
		if isString {
			if isJSON, err = jsonConfigStart.IsMatch(s); err != nil {
				return 0, err
			}
		}
		if isJSON {
			// JSON config
			if repoConfig, err = json.ParseJSON(s, ""); err != nil {
				return 0, err
			}
		} else {
			if arg2 == nil {
				return runtimeErr("You must pass the type and a url. Example: composer repo add foo vcs https://example.org")
			}
			repoConfig = php.ArrayOf("type", php.ToString(arg1), "url", php.ToString(arg2))
		}

		// ordering options
		before := in.Option("before")
		after := in.Option("after")
		if before != nil && after != nil {
			return runtimeErr("You can not combine --before and --after")
		}

		if before != nil || after != nil {
			if repoConfig == false {
				return runtimeErr("Cannot use --before/--after with boolean repository values")
			}
			ref, offset := before, 0
			if after != nil {
				ref, offset = after, 1
			}
			if err := c.ConfigSource.InsertRepository(php.ToString(name), repoConfig, php.ToString(ref), offset); err != nil {
				return 0, err
			}

			return 0, nil
		}

		if err := c.ConfigSource.AddRepository(php.ToString(name), repoConfig, console.BoolOption(in, "append")); err != nil {
			return 0, err
		}

		return 0, nil

	case "remove", "rm", "delete":
		if name == nil {
			return runtimeErr("You must pass the repository name to remove.")
		}
		if err := c.ConfigSource.RemoveRepository(php.ToString(name)); err != nil {
			return 0, err
		}
		if isPackagist(name) {
			if err := c.ConfigSource.AddRepository("packagist.org", false, false); err != nil {
				return 0, err
			}
		}

		return 0, nil

	case "set-url", "seturl":
		if name == nil || arg1 == nil {
			return runtimeErr("Usage: composer repo set-url <name> <new-url>")
		}

		if err := c.ConfigSource.SetRepositoryURL(php.ToString(name), php.ToString(arg1)); err != nil {
			return 0, err
		}

		return 0, nil

	case "get-url", "geturl":
		if name == nil {
			return runtimeErr("Usage: composer repo get-url <name>")
		}
		nameStr := php.ToString(name)
		if repo, ok := repos.GetArray(nameStr); ok {
			url, ok := repo.GetString("url")
			if !ok {
				return 0, NewError(ClassInvalidArgument, "The "+nameStr+" repository does not have a URL")
			}
			c.IO().Write(url, true, io.Normal)

			return 0, nil
		}
		// try named-list: find entry with matching name
		for _, val := range repos.All() {
			if repo, ok := val.(*php.Array); ok {
				if n, ok := repo.Get("name"); ok && n == name {
					url, ok := repo.GetString("url")
					if !ok {
						return 0, NewError(ClassInvalidArgument, "The "+nameStr+" repository does not have a URL")
					}
					c.IO().Write(url, true, io.Normal)

					return 0, nil
				}
			}
		}

		return 0, NewError(ClassInvalidArgument, "There is no "+nameStr+" repository defined")

	case "disable":
		if name == nil {
			return runtimeErr("Usage: composer repo disable packagist.org")
		}
		if isPackagist(name) {
			// special handling mirrors ConfigCommand behavior
			if err := c.ConfigSource.AddRepository("packagist.org", false, console.BoolOption(in, "append")); err != nil {
				return 0, err
			}

			return 0, nil
		}

		return runtimeErr("Only packagist.org can be enabled/disabled using this command. Use add/remove for other repositories.")

	case "enable":
		if name == nil {
			return runtimeErr("Usage: composer repo enable packagist.org")
		}
		if isPackagist(name) {
			// Remove a false flag by setting packagist.org to true via removing the key
			// Here we re-add the default by removing overrides
			if err := c.ConfigSource.RemoveRepository("packagist.org"); err != nil {
				return 0, err
			}

			return 0, nil
		}

		return runtimeErr("Only packagist.org can be enabled/disabled using this command.")
	}

	return 0, NewError(ClassInvalidArgument, `Unknown action "`+action+`". Use list, add, remove, set-url, get-url, enable, disable`)
}

// listRepositories ports listRepositories.
func (c *RepositoryCommand) listRepositories(repos *php.Array) {
	out := c.IO()

	packagistPresent := false
	for _, repo := range repos.All() {
		r, ok := repo.(*php.Array)
		if !ok {
			continue
		}
		typ, tok := r.Get("type")
		url, uok := r.Get("url")
		if tok && uok && typ != nil && url != nil && typ == "composer" {
			host := ""
			if u, ok := util.ParseURL(php.ToString(url)); ok {
				host = u.Host
			}
			if strings.HasSuffix(host, "packagist.org") {
				packagistPresent = true

				break
			}
		}
	}
	if !packagistPresent {
		repos.Append(php.ArrayOf("packagist.org", false))
	}

	if repos.Len() == 0 {
		out.Write("No repositories configured", true, io.Normal)

		return
	}

	for key, repo := range repos.All() {
		if repo == false {
			out.Write("["+key.String()+"] <info>disabled</info>", true, io.Normal)

			continue
		}

		r, ok := repo.(*php.Array)
		if !ok {
			continue
		}
		if r.Len() == 1 {
			if k, v, _ := r.First(); v == false {
				out.Write("["+k.String()+"] <info>disabled</info>", true, io.Normal)

				continue
			}
		}

		name := key.String()
		if n, ok := r.Get("name"); ok && n != nil {
			name = php.ToString(n)
		}
		typ := "unknown"
		if t, ok := r.Get("type"); ok && t != nil {
			typ = php.ToString(t)
		}
		var url string
		if u, ok := r.Get("url"); ok && u != nil {
			url = php.ToString(u)
		} else {
			url, _ = json.EncodeDefault(r)
		}
		out.Write("["+name+"] <info>"+typ+"</info> "+url, true, io.Normal)
	}
}

// suggestTypeForAdd ports suggestTypeForAdd.
func suggestTypeForAdd(input *console.CompletionInput, _ *console.CompletionSuggestions) []console.Suggestion {
	if input.Argument("action") == "add" {
		return suggestStrings([]string{"composer", "vcs", "artifact", "path"})
	}

	return nil
}

// suggestRepoNames ports suggestRepoNames.
func (c *RepositoryCommand) suggestRepoNames() console.SuggestFunc {
	return func(input *console.CompletionInput, _ *console.CompletionSuggestions) []console.Suggestion {
		action := input.Argument("action")
		if action == "enable" || action == "disable" {
			return suggestStrings([]string{"packagist.org"})
		}

		if action != "remove" && action != "set-url" && action != "get-url" {
			return nil
		}

		cfg := must(c.factory().CreateConfig(nil, ""))
		configFile := must(json.NewFile(must(c.ComposerConfigFile(input, cfg)), nil, nil))

		data := must(configFile.Read())
		var repos []string
		if d, ok := data.(*php.Array); ok {
			if list, ok := d.GetArray("repositories"); ok {
				for _, repo := range list.All() {
					if r, ok := repo.(*php.Array); ok {
						if n, ok := r.Get("name"); ok && n != nil {
							repos = append(repos, php.ToString(n))
						}
					}
				}
			}
		}

		php.SortSlice(repos, func(a, b string) int { return php.Compare(a, b) })

		return suggestStrings(repos)
	}
}
