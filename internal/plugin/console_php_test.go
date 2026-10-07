package plugin

// Commands and the Symfony Console (docs/PLUGINS.md tier 4): a plugin's
// CommandProvider commands in maestro's Application (listed, described,
// completed, run in PHP), the Application and maestro's commands as PHP
// sees them, an input change of a PRE_COMMAND_RUN listener reaching the
// command, nested Composer instances and Applications, BufferIO, and a
// script naming a Symfony command class.

import (
	"bytes"
	"runtime"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
)

// runApp runs maestro's Application on the plugin runtime of a project,
// as cmd/maestro does, with its output in memory.
func (p *project) runApp(args ...string) (string, int) {
	p.t.Helper()

	app := command.NewApplication(p.factory)
	app.SetAutoExit(false)
	app.SetCatchExceptions(true)

	var buf bytes.Buffer
	decorated := false
	out := console.NewStreamOutput(&buf, console.VerbosityNormal, &decorated, nil)
	in, err := console.NewArgvInput(append([]string{"maestro"}, args...), nil)
	if err != nil {
		p.t.Fatal(err)
	}
	code, err := app.Run(in, out)
	if err != nil {
		p.t.Fatalf("%v: %v", args, err)
	}

	// getDisplay(true): maestro's Output ends lines with PHP_EOL; the
	// expectations below use "\n" for those (and cr for PHP's own).
	return php.NormalizeEOL(buf.String()), code
}

func TestPlugins_Commands(t *testing.T) {
	requirePHP(t)

	p := newProject(t, "commands", console.VerbosityNormal)
	if code, err := p.install(true); err != nil || code != 0 {
		t.Fatalf("install = %d, %v\n%s", code, err, p.output())
	}

	// On Windows PHP's outputs end lines with PHP_EOL, "\r\n", and
	// BaseCommand::getTerminalWidth() takes one column off the 80 a
	// console-less Terminal reports.
	cr, wide := "", "true"
	if runtime.GOOS == "windows" {
		cr, wide = "\r", "false"
	}

	contains := func(name, output string, want ...string) {
		t.Helper()
		for _, w := range want {
			if !strings.Contains(output, w) {
				t.Errorf("%s: no %q in\n%s", name, w, output)
			}
		}
	}

	out, code := p.runApp("list")
	if code != 0 {
		t.Fatalf("list = %d\n%s", code, out)
	}
	contains("list", out, " maestro:hello ", "Says hello.", " maestro:nested ", " greet ", "[mh]")

	out, code = p.runApp("help", "mh")
	if code != 0 {
		t.Fatalf("help = %d\n%s", code, out)
	}
	contains("help", out, "maestro:hello [options] [--] [<times>]", "The maestro:hello command says hello.", "--no-plugins")

	// Run in PHP: its output, the IO, the Application and maestro's
	// commands as PHP sees them, a BufferIO created in PHP.
	out, code = p.runApp("maestro:hello", "--name=bob", "2")
	if code != 3 {
		t.Errorf("maestro:hello = %d, want 3\n%s", code, out)
	}
	contains("maestro:hello", out,
		"hello bob x2\n",
		"io maestro-test/commands-project\n",
		`app Composer\Console\Application true Composer\Command\InstallCommand true`+"\n",
		"install args: packages\n",
		`helpers {"acme\/lib":"^1.0","acme\/other":"2.0"} [false,true] `+wide+"\n",
		"buffered output"+cr+"|and errors"+cr+"|\n",
	)

	// Completion of the plugin command's option values (its
	// Composer\Console\Input\InputOption) and option names.
	out, code = p.runApp("_complete", "-n", "-c2", "--shell=bash", "-icomposer", "-imaestro:hello", "-i--name=")
	if code != 0 || out != "alice\nbob\n" {
		t.Errorf("_complete --name = %d %q", code, out)
	}
	out, _ = p.runApp("_complete", "-n", "-c2", "--shell=bash", "-icomposer", "-imaestro:hello", "-i--na")
	contains("_complete --na", out, "--name\n")

	// A PRE_COMMAND_RUN listener changed the input of maestro's licenses
	// command: it prints JSON.
	out, code = p.runApp("licenses")
	if code != 0 || !strings.HasPrefix(out, "{\n    \"name\": \"maestro-test/commands-project\"") {
		t.Errorf("licenses = %d\n%s", code, out)
	}

	// A Composer instance and Applications created in PHP, run in maestro
	// re-entrantly, writing to the command's output and to a PHP output.
	out, code = p.runApp("maestro:nested")
	if code != 0 {
		t.Errorf("maestro:nested = %d\n%s", code, out)
	}
	contains("maestro:nested", out,
		"created maestro-test/commands-project false\n",
		"  License   Number of dependencies  \n",
		"nested 0\n",
		"buffered 3: hello nested x1"+cr+"|io maestro-test/commands-project"+cr+"|",
	)

	// maestro's own commands run from PHP ($app->find()->run()): Symfony's
	// run() in PHP, maestro's hooks; the PRE_COMMAND_RUN listener's change
	// reaches the command and shows in PHP's input object.
	out, code = p.runApp("maestro:builtin")
	if code != 0 {
		t.Errorf("maestro:builtin = %d\n%s", code, out)
	}
	contains("maestro:builtin", out,
		"{\n    \"name\": \"maestro-test/commands-project\"",
		"licenses 0 json 'licenses'\n",
		"summary 0: ",
		"greetings them from greet\nrun-script 0\n",
		`error RuntimeException: Unsupported format "nope".  See help for supported formats.`+"\n",
		"proxy true false\n",
		"complete true\n",
	)

	// The script naming a Symfony command class is that command.
	out, code = p.runApp("greet", "them")
	if code != 0 || out != "greetings them from greet\n" {
		t.Errorf("greet = %d %q", code, out)
	}
}
