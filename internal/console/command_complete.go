// Ports src/Command/CompleteCommand.php, src/Command/DumpCompletionCommand.php,
// src/Completion/Output/CompletionOutputInterface.php and
// src/Completion/Output/BashCompletionOutput.php (symfony/console).

package console

import (
	_ "embed"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
)

//go:embed resources/completion.bash
var completionBash string

// completionScripts mirrors the Resources/completion.* files.
var completionScripts = []struct{ shell, script string }{
	{"bash", completionBash},
}

// CompletionOutput is CompletionOutputInterface: it renders suggestions in
// the format a shell's completion script reads.
type CompletionOutput interface {
	Write(suggestions *CompletionSuggestions, out Output)
}

// BashCompletionOutput renders one suggestion per line.
type BashCompletionOutput struct{}

// Write implements CompletionOutput.
func (BashCompletionOutput) Write(suggestions *CompletionSuggestions, out Output) {
	values := make([]string, 0, len(suggestions.ValueSuggestions())+len(suggestions.OptionSuggestions()))
	for _, v := range suggestions.ValueSuggestions() {
		values = append(values, v.Value)
	}
	for _, option := range suggestions.OptionSuggestions() {
		values = append(values, "--"+option.Name())
		if option.IsNegatable() {
			values = append(values, "--no-"+option.Name())
		}
	}
	out.Writeln(strings.Join(values, "\n"))
}

// ShellCompletionOutput registers a CompletionOutput for a shell name.
type ShellCompletionOutput struct {
	Shell  string
	Output CompletionOutput
}

// CompleteCommand is the hidden _complete command the completion scripts call.
type CompleteCommand struct {
	*Command
	completionOutputs []ShellCompletionOutput
	isDebug           bool
}

// PHPClass implements php.Classer.
func (*CompleteCommand) PHPClass() string { return `Symfony\Component\Console\Command\CompleteCommand` }

// NewCompleteCommand mirrors new CompleteCommand($completionOutputs): the
// given outputs come first, bash is added unless overridden.
func NewCompleteCommand(outputs ...ShellCompletionOutput) *CompleteCommand {
	c := &CompleteCommand{Command: NewCommand("")}
	c.SetImpl(c)

	c.completionOutputs = append(c.completionOutputs, outputs...)
	if c.completionOutput("bash") == nil {
		c.completionOutputs = append(c.completionOutputs, ShellCompletionOutput{"bash", BashCompletionOutput{}})
	}

	// $defaultName = '|_complete': the leading "|" hides the command.
	c.SetHidden(true)
	c.SetName("_complete")
	c.SetDescription("Internal command to provide shell completion suggestions")

	c.AddOption("shell", "s", OptionValueRequired, `The shell type ("`+strings.Join(c.shells(), `", "`)+`")`, nil).
		AddOption("input", "i", OptionValueRequired|OptionValueIsArray, "An array of input tokens (e.g. COMP_WORDS or argv)", nil).
		AddOption("current", "c", OptionValueRequired, `The index of the "input" array that the cursor is in (e.g. COMP_CWORD)`, nil).
		AddOption("symfony", "S", OptionValueRequired, "The version of the completion script", nil)

	return c
}

func (c *CompleteCommand) shells() []string {
	names := make([]string, len(c.completionOutputs))
	for i, o := range c.completionOutputs {
		names[i] = o.Shell
	}

	return names
}

func (c *CompleteCommand) completionOutput(shell string) CompletionOutput {
	for _, o := range c.completionOutputs {
		if o.Shell == shell {
			return o.Output
		}
	}

	return nil
}

// Initialize implements Initializer.
func (c *CompleteCommand) Initialize(Input, Output) error {
	c.isDebug = filterValidateBool(os.Getenv("SYMFONY_COMPLETION_DEBUG"))

	return nil
}

// filterValidateBool is filter_var($v, FILTER_VALIDATE_BOOLEAN).
func filterValidateBool(v string) bool {
	switch php.Strtolower(php.Trim(v)) {
	case "1", "true", "on", "yes":
		return true
	}

	return false
}

// Execute implements Executor.
func (c *CompleteCommand) Execute(in Input, out Output) (code int, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = recoverThrowable(r)
		}
		if err != nil {
			c.log("<error>Error!</error>", throwableString(err))

			if out.IsDebug() {
				return
			}

			code, err = 2, nil
		}
	}()

	return c.execute(in, out)
}

// throwableString approximates (string) $e for the debug log.
func throwableString(err error) string {
	class, _, _ := throwableInfo(err)

	return class + ": " + err.Error()
}

func (c *CompleteCommand) execute(in Input, out Output) (int, error) {
	shell := in.Option("shell")
	if !inputTruthy(shell) {
		return 0, newError(KindSPLRuntime, `The "--shell" option must be set.`)
	}

	completionOutput := c.completionOutput(phpToString(shell))
	if completionOutput == nil {
		return 0, newError(KindSPLRuntime, `Shell completion is not supported for your shell: "%s" (supported: "%s").`, phpToString(shell), strings.Join(c.shells(), `", "`))
	}

	completionInput, err := c.createCompletionInput(in)
	if err != nil {
		return 0, err
	}
	suggestions := &CompletionSuggestions{}

	c.log(
		"",
		"<comment>"+php.Date("Y-m-d H:i:s", time.Now().Unix())+"</>",
		`<info>Input:</> <comment>("|" indicates the cursor position)</>`,
		"  "+completionInput.String(),
		"<info>Command:</>",
		"  "+strings.Join(os.Args, " "),
		"<info>Messages:</>",
	)

	command, err := c.findCommand(completionInput)
	if err != nil {
		return 0, err
	}
	switch {
	case command == nil:
		c.log("  No command found, completing using the Application class.")

		c.Application().Complete(completionInput, suggestions)
	case completionInput.MustSuggestArgumentValuesFor("command") &&
		command.Base().Name() != completionInput.CompletionValue() &&
		!slices.Contains(command.Base().Aliases(), completionInput.CompletionValue()):
		c.log("  No command found, completing using the Application class.")

		// expand shortcut names ("cache:cl<TAB>") into their full name ("cache:clear")
		var names []string
		for _, n := range append([]string{command.Base().Name()}, command.Base().Aliases()...) {
			if php.Truthy(n) {
				names = append(names, n)
			}
		}
		suggestions.SuggestStrings(names...)
	default:
		base := command.Base()
		if err := base.MergeApplicationDefinition(true); err != nil {
			return 0, err
		}
		if err := completionInput.Bind(base.Definition()); err != nil {
			return 0, err
		}

		if completionInput.CompletionType() == CompletionTypeOptionName {
			c.log("  Completing option names for the <comment>" + command.PHPClass() + "</> command.")

			suggestions.SuggestOptions(base.Definition().Options()...)
		} else {
			c.log(
				"  Completing using the <comment>"+command.PHPClass()+"</> class.",
				"  Completing <comment>"+completionInput.CompletionType()+"</> for <comment>"+completionInput.CompletionName()+"</>",
			)
			c.log("  Current value: <comment>" + completionInput.CompletionValue() + "</>")

			command.Complete(completionInput, suggestions)
		}
	}

	c.log("<info>Suggestions:</>")
	if options := suggestions.OptionSuggestions(); len(options) > 0 {
		names := make([]string, len(options))
		for i, o := range options {
			names[i] = o.Name()
		}
		c.log("  --" + strings.Join(names, " --"))
	} else if values := suggestions.ValueSuggestions(); len(values) > 0 {
		strs := make([]string, len(values))
		for i, v := range values {
			strs[i] = v.Value
		}
		c.log("  " + strings.Join(strs, " "))
	} else {
		c.log("  <comment>No suggestions were provided</>")
	}

	completionOutput.Write(suggestions, out)

	return 0, nil
}

func (c *CompleteCommand) createCompletionInput(in Input) (*CompletionInput, error) {
	currentIndex := phpToString(in.Option("current"))
	if !inputTruthy(in.Option("current")) || !ctypeDigit(currentIndex) {
		return nil, newError(KindSPLRuntime, `The "--current" option must be set and it must be an integer.`)
	}

	completionInput := CompletionInputFromTokens(StringsOption(in, "input"), phpIntval(currentIndex))

	// Errors implementing ExceptionInterface are ignored; others propagate.
	if err := completionInput.Bind(c.Application().Definition()); err != nil && !IsConsoleException(err) {
		return nil, err
	}

	return completionInput, nil
}

func ctypeDigit(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}

	return true
}

func (c *CompleteCommand) findCommand(completionInput *CompletionInput) (Commander, error) {
	inputName := completionInput.FirstArgument()
	if inputName == "" {
		return nil, nil //nolint:nilnil // PHP returns null: no command typed yet.
	}

	cmd, err := c.Application().Find(inputName)
	if err != nil {
		if phperr.InstanceOf(err, ClassCommandNotFound) {
			return nil, nil //nolint:nilnil // CommandNotFoundException is swallowed.
		}

		return nil, err
	}

	return cmd, nil
}

// completionLogFile is sys_get_temp_dir().'/sf_'.basename($_SERVER['argv'][0]).'.log'.
func completionLogFile() string {
	name := ""
	if len(os.Args) > 0 {
		name = php.Basename(os.Args[0], "")
	}

	return php.SysGetTempDir() + "/sf_" + name + ".log"
}

func (c *CompleteCommand) log(messages ...string) {
	if !c.isDebug {
		return
	}

	f, err := os.OpenFile(completionLogFile(), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o666) //nolint:gosec // file_put_contents() creates files with mode 0666 & ~umask.
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = io.WriteString(f, strings.Join(messages, php.EOL)+php.EOL)
}

// DumpCompletionCommand dumps the shell completion script.
type DumpCompletionCommand struct {
	*Command
}

// PHPClass implements php.Classer.
func (*DumpCompletionCommand) PHPClass() string {
	return `Symfony\Component\Console\Command\DumpCompletionCommand`
}

// NewDumpCompletionCommand configures the completion command.
func NewDumpCompletionCommand() *DumpCompletionCommand {
	c := &DumpCompletionCommand{Command: NewCommand("completion")}
	c.SetImpl(c)
	c.SetDescription("Dump the shell completion script")

	fullCommand := phpSelf()
	commandName := php.Basename(fullCommand, "")
	if real, ok := php.Realpath(fullCommand); ok {
		fullCommand = real
	}

	c.SetHelp(`The <info>%command.name%</> command dumps the shell completion script required
to use shell autocompletion (currently only bash completion is supported).

<comment>Static installation
-------------------</>

Dump the script to a global completion file and restart your shell:

    <info>%command.full_name% bash | sudo tee /etc/bash_completion.d/`+commandName+`</>

Or dump the script to a local file and source it:

    <info>%command.full_name% bash > completion.sh</>

    <comment># source the file whenever you use the project</>
    <info>source completion.sh</>

    <comment># or add this line at the end of your "~/.bashrc" file:</>
    <info>source /path/to/completion.sh</>

<comment>Dynamic installation
--------------------</>

Add this to the end of your shell configuration file (e.g. <info>"~/.bashrc"</>):

    <info>eval "$(`+fullCommand+` completion bash)"</>`).
		AddArgument("shell", ArgumentOptional, `The shell type (e.g. "bash"), the value of the "$SHELL" env var will be used if this is not given`, nil).
		AddOption("debug", "", OptionValueNone, "Tail the completion debug log", nil)

	return c
}

// Complete implements Commander.
func (c *DumpCompletionCommand) Complete(in *CompletionInput, suggestions *CompletionSuggestions) {
	if in.MustSuggestArgumentValuesFor("shell") {
		suggestions.SuggestStrings(supportedShells()...)
	}
}

func supportedShells() []string {
	shells := make([]string, len(completionScripts))
	for i, s := range completionScripts {
		shells[i] = s.shell
	}

	return shells
}

// Execute implements Executor.
func (c *DumpCompletionCommand) Execute(in Input, out Output) (int, error) {
	commandName := ""
	if len(os.Args) > 0 {
		commandName = php.Basename(os.Args[0], "")
	}

	if BoolOption(in, "debug") {
		tailDebugLog(commandName, out)

		return 0, nil
	}

	var shell string
	if v := in.Argument("shell"); v != nil {
		shell = phpToString(v)
	} else {
		// guessShell(): basename($_SERVER['SHELL'] ?? '')
		shell = php.Basename(os.Getenv("SHELL"), "")
	}

	script := ""
	found := false
	for _, s := range completionScripts {
		if s.shell == shell {
			script, found = s.script, true
		}
	}
	if !found {
		supported := supportedShells()
		out = ErrorOutputOf(out)
		if php.Truthy(shell) {
			out.Writeln(`<error>Detected shell "` + shell + `", which is not supported by Symfony shell completion (supported shells: "` + strings.Join(supported, `", "`) + `").</>`)
		} else {
			out.Writeln(`<error>Shell not detected, Symfony shell completion only supports "` + strings.Join(supported, `", "`) + `").</>`)
		}

		return 2, nil
	}

	out.Write(strings.NewReplacer("{{ COMMAND_NAME }}", commandName, "{{ VERSION }}", c.Application().Version()).Replace(script), false, OutputNormal)

	return 0, nil
}

// tailDebugLog runs `tail -f` on the completion debug log, forwarding its
// output. Like Process::run(), a failing tail is not an error.
func tailDebugLog(commandName string, out Output) {
	debugFile := php.SysGetTempDir() + "/sf_" + commandName + ".log"
	if _, err := os.Stat(debugFile); err != nil {
		if f, err := os.OpenFile(debugFile, os.O_CREATE|os.O_WRONLY, 0o666); err == nil { //nolint:gosec // touch() creates files with mode 0666 & ~umask.
			_ = f.Close()
		}
	}

	cmd := exec.Command("tail", "-f", debugFile) //nolint:gosec // the log path is derived from the temp dir and argv[0], as in PHP.
	cmd.Stdout = outputWriter{out}
	cmd.Stderr = outputWriter{out}
	_ = cmd.Run()
}

type outputWriter struct{ out Output }

func (w outputWriter) Write(p []byte) (int, error) {
	w.out.Write(string(p), false, OutputNormal)

	return len(p), nil
}
