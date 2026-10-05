// Ports src/Command/HelpCommand.php and ListCommand.php (symfony/console).

package console

// HelpCommand displays help for a command.
type HelpCommand struct {
	*Command
	command Commander
}

// NewHelpCommand configures the help command.
func NewHelpCommand() *HelpCommand {
	c := &HelpCommand{Command: NewCommand("")}
	c.SetImpl(c)
	c.IgnoreValidationErrors()

	c.SetName("help").
		SetDefinitionItems(
			MustArgument("command_name", ArgumentOptional, "The command name", "help"),
			MustOption("format", "", OptionValueRequired, "The output format (txt, xml, json, or md)", "txt"),
			MustOption("raw", "", OptionValueNone, "To output raw command help", nil),
		).
		SetDescription("Display help for a command").
		SetHelp(`The <info>%command.name%</info> command displays help for a given command:

  <info>%command.full_name% list</info>

You can also output the help in other formats by using the <comment>--format</comment> option:

  <info>%command.full_name% --format=xml list</info>

To display the list of available commands, please use the <info>list</info> command.`)

	return c
}

// SetCommand implements HelpSetter.
func (c *HelpCommand) SetCommand(cmd Commander) { c.command = cmd }

// Execute implements Executor.
func (c *HelpCommand) Execute(in Input, out Output) (int, error) {
	if c.command == nil {
		cmd, err := c.Application().Find(StringArgument(in, "command_name"))
		if err != nil {
			return 0, err
		}
		c.command = cmd
	}

	helper := NewDescriptorHelper()
	err := helper.Describe(out, c.command, DescriptorOptions{
		Format:  StringOption(in, "format"),
		RawText: BoolOption(in, "raw"),
	})

	c.command = nil
	if err != nil {
		return 0, err
	}

	return 0, nil
}

// Complete implements Commander.
func (c *HelpCommand) Complete(in *CompletionInput, suggestions *CompletionSuggestions) {
	if in.MustSuggestArgumentValuesFor("command_name") {
		names, _, err := NewApplicationDescription(c.Application(), "", false).Commands()
		if err == nil {
			suggestions.SuggestStrings(names...)
		}

		return
	}

	if in.MustSuggestOptionValuesFor("format") {
		suggestions.SuggestStrings(NewDescriptorHelper().Formats()...)
	}
}

// ListCommand lists commands.
type ListCommand struct {
	*Command
}

// NewListCommand configures the list command.
func NewListCommand() *ListCommand {
	c := &ListCommand{Command: NewCommand("")}
	c.SetImpl(c)

	c.SetName("list").
		SetDefinitionItems(
			MustArgument("namespace", ArgumentOptional, "The namespace name", nil),
			MustOption("raw", "", OptionValueNone, "To output raw command list", nil),
			MustOption("format", "", OptionValueRequired, "The output format (txt, xml, json, or md)", "txt"),
			MustOption("short", "", OptionValueNone, "To skip describing commands' arguments", nil),
		).
		SetDescription("List commands").
		SetHelp(`The <info>%command.name%</info> command lists all commands:

  <info>%command.full_name%</info>

You can also display the commands for a specific namespace:

  <info>%command.full_name% test</info>

You can also output the information in other formats by using the <comment>--format</comment> option:

  <info>%command.full_name% --format=xml</info>

It's also possible to get raw list of commands (useful for embedding command runner):

  <info>%command.full_name% --raw</info>`)

	return c
}

// Execute implements Executor.
func (c *ListCommand) Execute(in Input, out Output) (int, error) {
	helper := NewDescriptorHelper()
	err := helper.Describe(out, c.Application(), DescriptorOptions{
		Format:    StringOption(in, "format"),
		RawText:   BoolOption(in, "raw"),
		Namespace: StringArgument(in, "namespace"),
		Short:     BoolOption(in, "short"),
	})
	if err != nil {
		return 0, err
	}

	return 0, nil
}

// Complete implements Commander.
func (c *ListCommand) Complete(in *CompletionInput, suggestions *CompletionSuggestions) {
	if in.MustSuggestArgumentValuesFor("namespace") {
		namespaces, err := NewApplicationDescription(c.Application(), "", false).Namespaces()
		if err == nil {
			ids := make([]string, len(namespaces))
			for i, ns := range namespaces {
				ids[i] = ns.ID
			}
			suggestions.SuggestStrings(ids...)
		}

		return
	}

	if in.MustSuggestOptionValuesFor("format") {
		suggestions.SuggestStrings(NewDescriptorHelper().Formats()...)
	}
}
