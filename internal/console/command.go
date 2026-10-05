// Ports src/Command/Command.php (symfony/console).

package console

import (
	"os"
	"os/exec"
	"strings"
	"sync"
)

// Exit codes (Command constants).
const (
	Success = 0
	Failure = 1
	Invalid = 2
)

// Commander is implemented by every command: *Command itself, and types
// embedding *Command, which may override Run and Complete.
type Commander interface {
	Base() *Command
	Run(in Input, out Output) (int, error)
	Complete(in *CompletionInput, suggestions *CompletionSuggestions)
}

// Executor is the execute() hook of a concrete command.
type Executor interface {
	Execute(in Input, out Output) (int, error)
}

// Initializer is the initialize() hook.
type Initializer interface {
	Initialize(in Input, out Output) error
}

// Interactor is the interact() hook.
type Interactor interface {
	Interact(in Input, out Output) error
}

// Enabler is the isEnabled() hook.
type Enabler interface {
	IsEnabled() bool
}

// HelpSetter is implemented by the help command (HelpCommand::setCommand).
type HelpSetter interface {
	SetCommand(cmd Commander)
}

// Command is the base command.
type Command struct {
	self                   Commander
	application            *Application
	name                   string
	hasName                bool
	processTitle           string
	aliases                []string
	definition             *InputDefinition
	hidden                 bool
	help                   string
	description            string
	fullDefinition         *InputDefinition
	ignoreValidationErrors bool
	code                   func(in Input, out Output) (int, error)
	synopsisShort          string
	synopsisLong           string
	usages                 []string
	helperSet              *HelperSet
}

// NewCommand mirrors new Command($name); an empty name leaves it unset.
func NewCommand(name string) *Command {
	c := &Command{definition: &InputDefinition{}}
	if name != "" {
		c.SetName(name)
	}

	return c
}

// SetImpl records the concrete command embedding c, whose Execute,
// Initialize, Interact and IsEnabled hooks c calls. Application.Add does
// this automatically.
func (c *Command) SetImpl(impl Commander) { c.self = impl }

func (c *Command) impl() Commander {
	if c.self != nil {
		return c.self
	}

	return c
}

// Base implements Commander.
func (c *Command) Base() *Command { return c }

// IgnoreValidationErrors makes Run tolerate binding errors.
func (c *Command) IgnoreValidationErrors() { c.ignoreValidationErrors = true }

// SetApplication attaches the command to an application (or detaches it).
func (c *Command) SetApplication(app *Application) {
	c.application = app
	if app != nil {
		c.SetHelperSet(app.HelperSet())
	} else {
		c.helperSet = nil
	}
	c.fullDefinition = nil
}

// SetHelperSet sets the helper set.
func (c *Command) SetHelperSet(set *HelperSet) { c.helperSet = set }

// HelperSet returns the helper set.
func (c *Command) HelperSet() *HelperSet { return c.helperSet }

// Application returns the application.
func (c *Command) Application() *Application { return c.application }

// commandEnabled is $command->isEnabled().
func commandEnabled(cmd Commander) bool {
	if e, ok := cmd.(Enabler); ok {
		return e.IsEnabled()
	}

	return true
}

// Run runs the command.
func (c *Command) Run(in Input, out Output) (int, error) {
	// add the application arguments and options
	if err := c.MergeApplicationDefinition(true); err != nil {
		return 0, err
	}

	// bind the input against the command specific arguments/options
	if err := in.Bind(c.Definition()); err != nil {
		if !IsConsoleException(err) || !c.ignoreValidationErrors {
			return 0, err
		}
	}

	impl := c.impl()
	if h, ok := impl.(Initializer); ok {
		if err := h.Initialize(in, out); err != nil {
			return 0, err
		}
	}

	if c.processTitle != "" {
		// cli_set_process_title has no portable Go equivalent; PHP prints
		// nothing either when it succeeds.
		_ = c.processTitle
	}

	if in.IsInteractive() {
		if h, ok := impl.(Interactor); ok {
			if err := h.Interact(in, out); err != nil {
				return 0, err
			}
		}
	}

	// The command name argument is often omitted when a command is executed directly with its run() method.
	// It would fail the validation if we didn't make sure the command argument is present,
	// since it's required by the application.
	if in.HasArgument("command") && in.Argument("command") == nil {
		in.SetArgument("command", c.name)
	}

	if err := in.Validate(); err != nil {
		return 0, err
	}

	if c.code != nil {
		return c.code(in, out)
	}
	if h, ok := impl.(Executor); ok {
		return h.Execute(in, out)
	}

	return 0, newError(KindLogic, "Command.php", 208, "You must override the execute() method in the concrete command class.")
}

// Complete implements Commander; the base command suggests nothing.
func (*Command) Complete(*CompletionInput, *CompletionSuggestions) {}

// SetCode sets the code run instead of Execute.
func (c *Command) SetCode(code func(in Input, out Output) (int, error)) *Command {
	c.code = code

	return c
}

// MergeApplicationDefinition merges the application's arguments and options
// into the command definition.
func (c *Command) MergeApplicationDefinition(mergeArgs bool) error {
	if c.application == nil {
		return nil
	}

	appDef := c.application.Definition()
	full := &InputDefinition{}
	if err := full.SetOptions(c.definition.Options()...); err != nil {
		return err
	}
	if err := full.AddOptions(appDef.Options()...); err != nil {
		return err
	}

	if mergeArgs {
		if err := full.SetArguments(appDef.Arguments()...); err != nil {
			return err
		}
		if err := full.AddArguments(c.definition.Arguments()...); err != nil {
			return err
		}
	} else if err := full.SetArguments(c.definition.Arguments()...); err != nil {
		return err
	}
	c.fullDefinition = full

	return nil
}

// SetDefinition replaces the definition.
func (c *Command) SetDefinition(definition *InputDefinition) *Command {
	c.definition = definition
	c.fullDefinition = nil

	return c
}

// SetDefinitionItems replaces the definition's arguments and options; it
// panics on an invalid definition.
func (c *Command) SetDefinitionItems(items ...any) *Command {
	if err := c.definition.SetDefinition(items...); err != nil {
		panic(err)
	}
	c.fullDefinition = nil

	return c
}

// Definition returns the merged definition if any, else the native one.
func (c *Command) Definition() *InputDefinition {
	if c.fullDefinition != nil {
		return c.fullDefinition
	}

	return c.definition
}

// NativeDefinition returns the command's own definition.
func (c *Command) NativeDefinition() *InputDefinition { return c.definition }

// AddArgument adds an argument; it panics on an invalid definition, like
// the PHP exception escaping configure().
func (c *Command) AddArgument(name string, mode int, description string, def any) *Command {
	if err := c.definition.AddArgument(MustArgument(name, mode, description, def)); err != nil {
		panic(err)
	}
	if c.fullDefinition != nil {
		if err := c.fullDefinition.AddArgument(MustArgument(name, mode, description, def)); err != nil {
			panic(err)
		}
	}

	return c
}

// AddOption adds an option; it panics on an invalid definition.
func (c *Command) AddOption(name, shortcut string, mode int, description string, def any) *Command {
	return c.AddInputOption(MustOption(name, shortcut, mode, description, def))
}

// AddInputOption adds a prepared option (e.g. one with suggested values).
func (c *Command) AddInputOption(o *InputOption) *Command {
	if err := c.definition.AddOption(o); err != nil {
		panic(err)
	}
	if c.fullDefinition != nil {
		if err := c.fullDefinition.AddOption(o); err != nil {
			panic(err)
		}
	}

	return c
}

// AddInputArgument adds a prepared argument (e.g. one with suggested values).
func (c *Command) AddInputArgument(a *InputArgument) *Command {
	if err := c.definition.AddArgument(a); err != nil {
		panic(err)
	}
	if c.fullDefinition != nil {
		if err := c.fullDefinition.AddArgument(a); err != nil {
			panic(err)
		}
	}

	return c
}

// SetName sets the name; it panics on an invalid name.
func (c *Command) SetName(name string) *Command {
	if err := validateCommandName(name); err != nil {
		panic(err)
	}
	c.name = name
	c.hasName = true

	return c
}

// SetProcessTitle records the process title.
func (c *Command) SetProcessTitle(title string) *Command {
	c.processTitle = title

	return c
}

// Name returns the command name.
func (c *Command) Name() string { return c.name }

// SetHidden hides the command from lists.
func (c *Command) SetHidden(hidden bool) *Command {
	c.hidden = hidden

	return c
}

// IsHidden reports whether the command is hidden.
func (c *Command) IsHidden() bool { return c.hidden }

// SetDescription sets the description.
func (c *Command) SetDescription(description string) *Command {
	c.description = description

	return c
}

// Description returns the description.
func (c *Command) Description() string { return c.description }

// SetHelp sets the help text.
func (c *Command) SetHelp(help string) *Command {
	c.help = help

	return c
}

// Help returns the raw help text.
func (c *Command) Help() string { return c.help }

// ScriptName is $_SERVER['PHP_SELF'] as used in help texts: the path the
// program was started with, resolved through $PATH like the kernel does for
// a script's interpreter. It is computed lazily unless set.
var ScriptName string

var scriptNameOnce sync.Once

func phpSelf() string {
	scriptNameOnce.Do(func() {
		if ScriptName != "" || len(os.Args) == 0 {
			return
		}
		arg0 := os.Args[0]
		if !strings.ContainsRune(arg0, '/') {
			if p, err := exec.LookPath(arg0); err == nil {
				arg0 = p
			}
		}
		ScriptName = arg0
	})

	return ScriptName
}

// ProcessedHelp returns the help with %command.name% and
// %command.full_name% replaced.
func (c *Command) ProcessedHelp() string {
	name := c.name
	isSingleCommand := c.application != nil && c.application.IsSingleCommand()

	fullName := phpSelf()
	if !isSingleCommand {
		fullName += " " + name
	}

	help := c.Help()
	if help == "" {
		help = c.Description()
	}

	return strings.NewReplacer("%command.name%", name, "%command.full_name%", fullName).Replace(help)
}

// SetAliases sets the aliases; it panics on an invalid alias.
func (c *Command) SetAliases(aliases ...string) *Command {
	for _, a := range aliases {
		if err := validateCommandName(a); err != nil {
			panic(err)
		}
	}
	c.aliases = aliases

	return c
}

// Aliases returns the aliases.
func (c *Command) Aliases() []string { return c.aliases }

// Synopsis returns the command synopsis (cached).
func (c *Command) Synopsis(short bool) string {
	if short {
		if c.synopsisShort == "" {
			c.synopsisShort = phpTrim(c.name + " " + c.definition.Synopsis(true))
		}

		return c.synopsisShort
	}
	if c.synopsisLong == "" {
		c.synopsisLong = phpTrim(c.name + " " + c.definition.Synopsis(false))
	}

	return c.synopsisLong
}

// AddUsage adds a usage example, prefixed with the command name.
func (c *Command) AddUsage(usage string) *Command {
	if !strings.HasPrefix(usage, c.name) {
		usage = c.name + " " + usage
	}
	c.usages = append(c.usages, usage)

	return c
}

// Usages returns the usage examples.
func (c *Command) Usages() []string { return c.usages }

// Helper returns a helper from the helper set.
func (c *Command) Helper(name string) (Helper, error) {
	if c.helperSet == nil {
		return nil, newError(KindLogic, "Command.php", 691, `Cannot retrieve helper "%s" because there is no HelperSet defined. Did you forget to add your command to the application or to set the application on the command using the setApplication() method? You can also set the HelperSet directly using the setHelperSet() method.`, name)
	}

	return c.helperSet.Get(name)
}

// validateCommandName is preg_match('/^[^\:]++(\:[^\:]++)*$/', $name):
// non-empty parts separated by single colons.
func validateCommandName(name string) error {
	valid := name != ""
	for p := range strings.SplitSeq(name, ":") {
		if p == "" {
			valid = false

			break
		}
	}
	if !valid {
		return newError(KindInvalidArgument, "Command.php", 707, `Command name "%s" is invalid.`, name)
	}

	return nil
}
