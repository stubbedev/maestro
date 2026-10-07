// Ports src/Composer/Command/SearchCommand.php.

package command

import (
	"strings"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/repository"
)

func init() {
	registerCommand(OrderSearch, func() console.Commander { return NewSearchCommand() })
}

// SearchCommand is Composer\Command\SearchCommand.
type SearchCommand struct{ *BaseCommand }

// NewSearchCommand ports new SearchCommand() (configure()).
func NewSearchCommand() *SearchCommand {
	c := &SearchCommand{BaseCommand: NewBaseCommand("")}
	c.SetImpl(c)
	c.SetName("search")
	c.SetDescription("Searches for packages")
	c.SetDefinitionItems(
		console.MustOption("only-name", "N", console.OptionValueNone, "Search only in package names", nil),
		console.MustOption("only-vendor", "O", console.OptionValueNone, `Search only for vendor / organization names, returns only "vendor" as result`, nil),
		console.MustOption("type", "t", console.OptionValueRequired, "Search for a specific package type", nil),
		formatOption("Format of the output: text or json", "text", jsonTextFormats),
		console.MustArgument("tokens", console.ArgumentIsArray|console.ArgumentRequired, "tokens to search for", nil),
	)
	c.SetHelp(`The search command searches for packages by its name
<info>php composer.phar search symfony composer</info>

Read more at https://getcomposer.org/doc/03-cli.md#search`)

	return c
}

// PHPClass implements php.Classer.
func (*SearchCommand) PHPClass() string { return `Composer\Command\SearchCommand` }

// Execute ports execute().
func (c *SearchCommand) Execute(in console.Input, out console.Output) (int, error) {
	// init repos
	platformRepo, err := c.newPlatformRepository(nil)
	if err != nil {
		return 0, err
	}
	cio := c.IO()

	format := in.Option("format")
	if format != "text" && format != "json" {
		cio.WriteError(`Unsupported format "`+php.ToString(format)+`". See help for supported formats.`, true, io.Normal)

		return 1, nil
	}

	comp, err := c.TryComposer(nil, nil)
	if err != nil {
		return 0, err
	}
	if comp == nil {
		if comp, err = c.CreateComposerInstance(in, c.IO(), php.NewArray(), false, false); err != nil {
			return 0, err
		}
	}
	localRepo := comp.RepositoryManager().LocalRepository()
	installedRepo, err := repository.NewCompositeRepository([]repository.RepositoryInterface{localRepo, platformRepo})
	if err != nil {
		return 0, err
	}
	repos, err := repository.NewCompositeRepository(append([]repository.RepositoryInterface{installedRepo}, comp.RepositoryManager().Repositories()...))
	if err != nil {
		return 0, err
	}

	commandEvent := eventdispatcher.NewCommandEvent(eventdispatcher.PluginCommand, "search", in, out, nil, nil)
	if _, err := comp.EventDispatcher().Dispatch(commandEvent.Name(), commandEvent); err != nil {
		return 0, err
	}

	mode := repository.SearchFulltext
	if in.Option("only-name") == true {
		if in.Option("only-vendor") == true {
			return 0, NewError(ClassInvalidArgument, "--only-name and --only-vendor cannot be used together")
		}
		mode = repository.SearchName
	} else if in.Option("only-vendor") == true {
		mode = repository.SearchVendor
	}

	typ, _ := in.Option("type").(string)

	query := strings.Join(console.StringsArgument(in, "tokens"), " ")
	if mode != repository.SearchFulltext {
		query = php.PregQuote(query, "")
	}

	results, err := repos.SearchWithIO(query, mode, typ, cio)
	if err != nil {
		return 0, err
	}

	if len(results) > 0 && format == "text" {
		width := c.TerminalWidth()

		nameLength := 0
		for _, result := range results {
			nameLength = max(len(result.Name), nameLength)
		}
		nameLength++
		for _, result := range results {
			description := result.Description.S
			warning := ""
			if php.ToBool(result.Abandoned) {
				warning = "<warning>! Abandoned !</warning> "
			}
			remaining := width - nameLength - len(warning) - 2
			if len(description) > remaining {
				description = searchSubstr(description, remaining-3) + "..."
			}

			if result.URL.Valid {
				cio.Write("<href="+console.Escape(result.URL.S)+">"+result.Name+"</>"+strings.Repeat(" ", nameLength-len(result.Name))+warning+description, true, io.Normal)
			} else {
				cio.Write(php.StrPad(result.Name, nameLength, " ", php.StrPadRight)+warning+description, true, io.Normal)
			}
		}
	} else if format == "json" {
		list := php.NewArray()
		for _, result := range results {
			list.Append(searchResultArray(result))
		}
		encoded, err := json.EncodeDefault(list)
		if err != nil {
			return 0, err
		}
		cio.Write(encoded, true, io.Normal)
	}

	return 0, nil
}

// searchSubstr is substr($s, 0, $length), a negative length counting from
// the end.
func searchSubstr(s string, length int) string {
	if length < 0 {
		length += len(s)
		if length < 0 {
			length = 0
		}
	}
	if length > len(s) {
		length = len(s)
	}

	return s[:length]
}

// searchResultArray is the PHP array of a search result: the search API's
// own array when the repository kept it, else the keys
// RepositoryInterface::search documents.
func searchResultArray(r repository.SearchResult) *php.Array {
	if r.Raw != nil {
		return r.Raw
	}
	a := php.ArrayOf("name", r.Name)
	if r.Description.Valid {
		a.Set("description", r.Description.S)
	} else {
		a.Set("description", nil)
	}
	if r.Abandoned != nil {
		a.Set("abandoned", r.Abandoned)
	}
	if r.URL.Valid {
		a.Set("url", r.URL.S)
	}

	return a
}
