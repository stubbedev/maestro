// Ports src/Composer/Command/ExecCommand.php.

package command

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

func init() {
	registerCommand(OrderExec, func() console.Commander { return NewExecCommand() })
}

// ExecCommand is Composer\Command\ExecCommand.
type ExecCommand struct{ *BaseCommand }

// NewExecCommand ports new ExecCommand() (configure()).
func NewExecCommand() *ExecCommand {
	c := &ExecCommand{BaseCommand: NewBaseCommand("")}
	c.SetImpl(c)
	c.SetName("exec")
	c.SetDescription("Executes a vendored binary/script")
	c.SetDefinitionItems(
		console.MustOption("list", "l", console.OptionValueNone, "", nil),
		console.MustArgument("binary", console.ArgumentOptional, "The binary to run, e.g. phpunit", nil).WithSuggestFunc(func(*console.CompletionInput, *console.CompletionSuggestions) []console.Suggestion {
			binaries, err := c.binaries(false)
			if err != nil {
				return nil
			}
			suggestions := make([]console.Suggestion, len(binaries))
			for i, b := range binaries {
				suggestions[i] = console.Suggestion{Value: b}
			}

			return suggestions
		}),
		console.MustArgument("args", console.ArgumentIsArray|console.ArgumentOptional, "Arguments to pass to the binary. Use <info>--</info> to separate from composer arguments", nil),
	)
	c.SetHelp(`Executes a vendored binary/script.

Read more at https://getcomposer.org/doc/03-cli.md#exec`)

	return c
}

// ClassName implements console.ClassNamer.
func (*ExecCommand) ClassName() string { return `Composer\Command\ExecCommand` }

// Interact ports interact().
func (c *ExecCommand) Interact(in console.Input, _ console.Output) error {
	binaries, err := c.binaries(false)
	if err != nil {
		return err
	}
	if len(binaries) == 0 {
		return nil
	}

	if in.Argument("binary") != nil || console.BoolOption(in, "list") {
		return nil
	}

	choices := php.NewArray()
	for _, b := range binaries {
		choices.Append(b)
	}
	binary, err := c.IO().Select("Binary to run: ", choices, "", 1, `Invalid binary name "%s"`, false)
	if err != nil {
		return err
	}

	in.SetArgument("binary", binaries[php.ToInt(binary)])

	return nil
}

// Execute ports execute().
func (c *ExecCommand) Execute(in console.Input, _ console.Output) (int, error) {
	cmp, err := c.RequireComposer(nil, nil)
	if err != nil {
		return 0, err
	}
	if console.BoolOption(in, "list") || in.Argument("binary") == nil {
		bins, err := c.binaries(true)
		if err != nil {
			return 0, err
		}
		if len(bins) == 0 {
			binDir, err := cmp.Config().Get("bin-dir", 0)
			if err != nil {
				return 0, err
			}

			return 0, NewError(ClassRuntime, "No binaries found in composer.json or in bin-dir ("+php.ToString(binDir)+")")
		}

		c.IO().Write("<comment>Available binaries:</comment>", true, io.Normal)

		for _, bin := range bins {
			c.IO().Write("<info>- "+bin+"</info>", true, io.Normal)
		}

		return 0, nil
	}

	binary := console.StringArgument(in, "binary")

	dispatcher := cmp.EventDispatcher()
	dispatcher.AddListener("__exec_command", eventdispatcher.Script(binary), 0)

	// If the CWD was modified, we restore it to what it was initially, as it was
	// most likely modified by the global command, and we want exec to run in the local working directory
	// not the global one
	app, err := c.App()
	if err != nil {
		return 0, err
	}
	initial := app.InitialWorkingDirectory()
	if cwd, _ := util.GetCwd(false); cwd != initial && initial != "" {
		if err := os.Chdir(initial); err != nil {
			e := NewError(ClassRuntime, `Could not switch back to working directory "`+initial+`"`)
			e.Prev = &util.ErrorException{Message: "chdir(): " + chdirWarning(err)}

			return 0, e
		}
	}

	return dispatcher.DispatchScript("__exec_command", true, console.StringsArgument(in, "args"), nil)
}

// chdirWarning is the text of PHP's chdir() warning for err after
// "chdir(): ": "Message (errno N)".
func chdirWarning(err error) string {
	errno, ok := php.Errno(err)
	if !ok {
		return err.Error()
	}

	return php.Strerror(err) + " (errno " + strconv.Itoa(errno) + ")"
}

// binaries ports getBinaries.
func (c *ExecCommand) binaries(forDisplay bool) ([]string, error) {
	cmp, err := c.RequireComposer(nil, nil)
	if err != nil {
		return nil, err
	}
	binDirValue, err := cmp.Config().Get("bin-dir", 0)
	if err != nil {
		return nil, err
	}
	bins := phpGlobAll(php.ToString(binDirValue) + "/*")
	var localBins []string
	for _, e := range cmp.Package().Binaries().Values() {
		if forDisplay {
			localBins = append(localBins, php.ToString(e)+" (local)")
		} else {
			localBins = append(localBins, php.ToString(e))
		}
	}

	var binaries []string
	previousBin, hasPrevious := "", false
	for _, bin := range append(bins, localBins...) {
		// skip .bat copies
		if hasPrevious && bin == previousBin+".bat" {
			continue
		}

		previousBin, hasPrevious = bin, true
		binaries = append(binaries, php.Basename(bin, ""))
	}

	return binaries, nil
}

// phpGlobAll is glob($pattern) for a pattern whose only wildcard is a
// trailing "/*": the sorted entries of the directory, without dot files
// (an unreadable or missing directory gives no entries).
func phpGlobAll(pattern string) []string {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	result := matches[:0]
	for _, m := range matches {
		if !strings.HasPrefix(filepath.Base(m), ".") {
			result = append(result, m)
		}
	}

	return result
}
