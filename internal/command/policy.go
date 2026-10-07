// Ports src/Composer/Command/PolicyCommand.php.

package command

import (
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/filterlist/source"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/policy"
)

func init() {
	registerCommand(OrderPolicy, func() console.Commander { return NewPolicyCommand() })
}

// PolicyCommand is Composer\Command\PolicyCommand.
type PolicyCommand struct{ *BaseConfigCommand }

// NewPolicyCommand ports new PolicyCommand().
func NewPolicyCommand() *PolicyCommand {
	c := &PolicyCommand{BaseConfigCommand: NewBaseConfigCommand("")}
	c.SetImpl(c)
	c.SetName("policy").
		SetDescription("Manages custom dependency policies and their sources").
		SetDefinitionItems(
			console.MustOption("global", "g", console.OptionValueNone, "Apply command to the global config file", nil),
			console.MustOption("file", "f", console.OptionValueRequired, "If you want to choose a different composer.json or config.json", nil),
			console.MustArgument("action", console.ArgumentRequired, "Action to perform: add-source", nil).WithSuggestedValues("add-source"),
			console.MustArgument("name", console.ArgumentOptional, "Policy name", nil).WithSuggestFunc(c.suggestListNames()),
			console.MustArgument("arg1", console.ArgumentOptional, `Source type (e.g. "url") for add-source`, nil).WithSuggestFunc(suggestPolicyArg1),
			console.MustArgument("arg2", console.ArgumentOptional, "URL for add-source (if not using JSON)", nil),
		).
		SetHelp("This command lets you manage custom dependency policies and their sources in composer.json.\n" +
			"\n" +
			"Examples:\n" +
			"  composer policy add-source my-policy url https://example.org/my-pkgs.json\n" +
			"  composer policy add-source my-policy '{\"type\":\"url\",\"url\":\"https://example.org/my-pkgs.json\"}'\n" +
			"\n" +
			"Adding a source for a dependency policy that does not exist will create the policy.\n" +
			"\n" +
			"Built-in dependency policies (advisories, malware, abandoned) do not accept sources and\n" +
			"are rejected by add-source. Use `composer config policy.<policy>.<field>`\n" +
			"to adjust their settings.\n" +
			"\n" +
			"Use --global/-g to alter the global config.json instead.\n" +
			"Use --file to alter a specific file.")

	return c
}

// PHPClass implements php.Classer.
func (*PolicyCommand) PHPClass() string { return `Composer\Command\PolicyCommand` }

// nestedGet is `$a[k1][k2]... ?? null`.
func nestedGet(v any, keys ...any) (any, bool) {
	for _, k := range keys {
		a, ok := v.(*php.Array)
		if !ok {
			return nil, false
		}
		if v, ok = a.Get(k); !ok || v == nil {
			return nil, false
		}
	}

	return v, true
}

// Execute implements console.Executor.
func (c *PolicyCommand) Execute(in console.Input, _ console.Output) (int, error) {
	action := php.Strtolower(php.ToString(in.Argument("action")))
	listName := in.Argument("name")
	arg1 := in.Argument("arg1")
	arg2 := in.Argument("arg2")

	if action != "add-source" {
		return 0, NewError(ClassInvalidArgument, `Unknown action "`+action+`". Use add-source.`)
	}

	if listName == nil {
		return 0, NewError(ClassRuntime, "You must pass a dependency policy name. Example: composer policy add-source my-policy url https://example.org")
	}
	name := php.ToString(listName)
	if err := assertCustomListName(name); err != nil {
		return 0, err
	}
	if arg1 == nil {
		return 0, NewError(ClassRuntime, "You must pass the source type and a url, or a JSON string.")
	}

	var sourceConfig *php.Array
	s, isString := arg1.(string)
	isJSON := false
	if isString {
		var err error
		if isJSON, err = jsonConfigStart.IsMatch(s); err != nil {
			return 0, err
		}
	}
	if isJSON {
		parsed, err := json.ParseJSON(s, "")
		if err != nil {
			return 0, err
		}
		var ok bool
		if sourceConfig, ok = parsed.(*php.Array); !ok {
			return 0, NewError(ClassRuntime, "Source JSON must be an object.")
		}
	} else {
		if arg2 == nil {
			return 0, NewError(ClassRuntime, "You must pass the source type and a url. Example: composer policy add-source my-policy url https://example.org")
		}
		sourceConfig = php.ArrayOf("type", php.ToString(arg1), "url", php.ToString(arg2))
	}

	if _, err := source.Validate(name, sourceConfig); err != nil {
		return 0, err
	}

	data, err := c.ConfigFile.Read()
	if err != nil {
		return 0, err
	}
	currentSources, ok := nestedGet(data, "config", "policy", name, "sources")
	sources, isArray := currentSources.(*php.Array)
	if !ok || !isArray {
		sources = php.NewArray()
	} else {
		sources = sources.Clone()
	}

	srcType, _ := sourceConfig.Get("type")
	srcURL, _ := sourceConfig.Get("url")
	for _, existing := range sources.All() {
		e, ok := existing.(*php.Array)
		if !ok {
			continue
		}
		et, _ := e.Get("type")
		eu, _ := e.Get("url")
		if php.StrictEquals(et, srcType) && php.StrictEquals(eu, srcURL) {
			c.IO().Write("<info>Source "+php.ToString(srcURL)+" already present in policy "+name+"</info>", true, io.Normal)

			return 0, nil
		}
	}

	sources.Append(sourceConfig)
	if err := c.ConfigSource.AddConfigSetting("policy."+name+".sources", sources); err != nil {
		return 0, err
	}

	return 0, nil
}

// assertCustomListName ports assertCustomListName.
func assertCustomListName(name string) error {
	if slices.Contains(policy.BuiltinListNames[:], name) {
		return NewError(ClassRuntime, `Built-in dependency policy "`+name+"\" does not support sources. Use `composer config policy."+name+".<field>` to configure it.")
	}

	if msg := policy.FutureReservedListNameError(name); msg != "" {
		return NewError(ClassRuntime, msg)
	}

	if name == "" || strings.Contains(name, ".") {
		return NewError(ClassRuntime, `Invalid dependency policy name "`+name+`".`)
	}

	return nil
}

// suggestListNames ports suggestListNames.
func (c *PolicyCommand) suggestListNames() console.SuggestFunc {
	return func(input *console.CompletionInput, _ *console.CompletionSuggestions) []console.Suggestion {
		if input.Argument("action") != "add-source" {
			return nil
		}

		cfg := must(c.factory().CreateConfig(nil, ""))
		configFile := must(json.NewFile(must(c.ComposerConfigFile(input, cfg)), nil, nil))
		if !configFile.Exists() {
			return nil
		}

		data := must(configFile.Read())
		p, ok := nestedGet(data, "config", "policy")
		pol, isArray := p.(*php.Array)
		if !ok || !isArray {
			return nil
		}

		var names []string
		for key := range pol.All() {
			listName := key.String()
			if key.IsString() && (slices.Contains(policy.BuiltinListNames[:], listName) || slices.Contains(policy.NonListKeys[:], listName)) {
				continue
			}
			names = append(names, listName)
		}
		php.SortSlice(names, func(a, b string) int { return php.Compare(a, b) })

		return suggestStrings(names)
	}
}

// suggestPolicyArg1 ports suggestArg1.
func suggestPolicyArg1(input *console.CompletionInput, _ *console.CompletionSuggestions) []console.Suggestion {
	if input.Argument("action") == "add-source" {
		return suggestStrings([]string{"url"})
	}

	return nil
}
