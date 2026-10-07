// Ports tests/Composer/Test/Command/RunScriptCommandTest.php.

package command_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/plugin"
	"github.com/stubbedev/maestro/internal/switches"
)

func TestRunScriptCommand_DetectAndPassDevModeToEventAndToDispatching(t *testing.T) {
	for _, tc := range []struct{ dev, noDev bool }{
		{true, true},
		{true, false},
		{false, true},
		{false, false},
	} {
		t.Run("", func(t *testing.T) {
			const scriptName = "testScript"
			commandtest.InitTempComposer(t, nil, nil, nil, true)

			tester := commandtest.GetApplicationTester(t)
			c, err := tester.Application.GetComposer(true, nil, nil)
			if err != nil {
				t.Fatal(err)
			}

			expectedDevMode := tc.dev || !tc.noDev
			calls := 0
			c.EventDispatcher().AddListener(scriptName, eventdispatcher.GoFunc(func(ev eventdispatcher.Event) error {
				calls++
				se, ok := ev.(*eventdispatcher.ScriptEvent)
				if !ok {
					t.Fatalf("event is %T, want *ScriptEvent", ev)
				}
				if se.Name() != scriptName || se.IsDevMode() != expectedDevMode {
					t.Errorf("event %q devMode %v, want %q %v", se.Name(), se.IsDevMode(), scriptName, expectedDevMode)
				}
				if len(se.Arguments()) != 0 {
					t.Errorf("arguments %v, want none", se.Arguments())
				}

				return nil
			}), 0)

			args := []any{"command", "run-script", "script", scriptName, "--no-interaction", true}
			if tc.dev {
				args = append(args, "--dev", true)
			}
			if tc.noDev {
				args = append(args, "--no-dev", true)
			}
			code, err := tester.RunArgs(commandtest.Options{}, args...)
			if err != nil {
				t.Fatal(err)
			}
			if code != 0 || calls != 1 {
				t.Fatalf("code %d, %d dispatches; want 0, 1", code, calls)
			}
		})
	}
}

func TestRunScriptCommand_CanListScripts(t *testing.T) {
	commandtest.InitTempComposer(t, `{
		"scripts": {"test": "@php test", "fix-cs": "php-cs-fixer fix"},
		"scripts-descriptions": {"fix-cs": "Run the codestyle fixer"}
	}`, nil, nil, true)

	tester := commandtest.GetApplicationTester(t)
	code, err := tester.RunArgs(commandtest.Options{}, "command", "run-script", "--list", true)
	if err != nil || code != 0 {
		t.Fatalf("code %d, err %v", code, err)
	}

	output := tester.Display(false)
	if !strings.Contains(output, "Runs the test script as defined in composer.json") {
		t.Errorf("The default description for the test script should be printed:\n%s", output)
	}
	if !strings.Contains(output, "Run the codestyle fixer") {
		t.Errorf("The custom description for the fix-cs script should be printed:\n%s", output)
	}
}

func TestRunScriptCommand_CanDefineAliases(t *testing.T) {
	expectedAliases := []string{"one", "two", "three"}

	commandtest.InitTempComposer(t, `{
		"scripts": {"test": "@php test"},
		"scripts-aliases": {"test": ["one", "two", "three"]}
	}`, nil, nil, true)

	tester := commandtest.GetApplicationTester(t)
	code, err := tester.RunArgs(commandtest.Options{}, "command", "test", "--help", true, "--format", "json")
	if err != nil || code != 0 {
		t.Fatalf("code %d, err %v", code, err)
	}

	decoded, err := php.JSONDecode(tester.Display(false), true)
	if err != nil {
		t.Fatal(err)
	}
	usage, _ := decoded.(*php.Array).GetArray("usage")
	var actual []string
	for _, v := range usage.Values()[1:] {
		actual = append(actual, php.ToString(v))
	}
	if strings.Join(actual, ",") != strings.Join(expectedAliases, ",") {
		t.Errorf("The custom aliases for the test command should be printed: got %v", actual)
	}
}

// pluginTesters returns getApplicationTester() for Applications whose
// Composer instances run PHP (the plugin runtime: a script naming a Symfony
// Command class is a command written in PHP). It needs php
// (MAESTRO_PHP_TESTS=1).
func pluginTesters(t *testing.T) func() *commandtest.ApplicationTester {
	t.Helper()

	if !switches.On(switches.PHPTests) {
		t.Skip("set MAESTRO_PHP_TESTS=1 to run PHP commands with php")
	}

	factory := &composer.Factory{Runtime: commandtest.Runtime()}
	rt, err := plugin.Setup(factory, plugin.Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Close)

	return func() *commandtest.ApplicationTester {
		app := command.NewApplication(factory)
		app.SetAutoExit(false)
		app.SetCatchExceptions(false)

		return commandtest.NewApplicationTester(t, app)
	}
}

func TestRunScriptCommand_ExecutionOfSimpleSymfonyCommand(t *testing.T) {
	getApplicationTester := pluginTesters(t)

	description := "Sample description for test command"
	commandtest.InitTempComposer(t, `{
		"scripts": {
			"test-direct": "Test\\MyCommand",
			"test-ref": ["@test-direct --inneropt innerarg"]
		},
		"scripts-descriptions": {"test-direct": "`+description+`"},
		"autoload": {"psr-4": {"Test\\": ""}}
	}`, nil, nil, true)

	if err := os.WriteFile("MyCommand.php", []byte(`<?php

namespace Test;

use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Input\InputOption;
use Symfony\Component\Console\Input\InputArgument;
use Symfony\Component\Console\Output\OutputInterface;
use Symfony\Component\Console\Command\Command;

class MyCommand extends Command
{
    protected function configure(): void
    {
        $this->setDefinition([
            new InputArgument('req-arg', InputArgument::REQUIRED, 'Required arg.'),
            new InputArgument('opt-arg', InputArgument::OPTIONAL, 'Optional arg.'),
            new InputOption('inneropt', null, InputOption::VALUE_NONE, 'Option.'),
            new InputOption('outeropt', null, InputOption::VALUE_OPTIONAL, 'Optional option.'),
        ]);
    }

    public function execute(InputInterface $input, OutputInterface $output): int
    {
        $output->writeln($input->getArgument('req-arg'));
        $output->writeln((string) $input->getArgument('opt-arg'));
        $output->writeln('inneropt: '.($input->getOption('inneropt') ? 'set' : 'unset'));
        $output->writeln('outeropt: '.($input->getOption('outeropt') ? 'set' : 'unset'));

        return 2;
    }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	tester := getApplicationTester()
	if _, err := tester.RunArgs(commandtest.Options{}, "command", "test-direct", "--outeropt", true, "req-arg", "lala"); err != nil {
		t.Fatal(err)
	}
	if got := tester.Display(true); got != "lala\n\ninneropt: unset\nouteropt: set\n" {
		t.Errorf("test-direct display %q", got)
	}
	if tester.StatusCode() != 2 {
		t.Errorf("test-direct status %d, want 2", tester.StatusCode())
	}

	tester = getApplicationTester()
	if _, err := tester.RunArgs(commandtest.Options{}, "command", "test-ref", "--outeropt", true, "req-arg", "lala"); err != nil {
		t.Fatal(err)
	}
	if got := tester.Display(true); got != "innerarg\nlala\ninneropt: set\nouteropt: set\n" {
		t.Errorf("test-ref display %q", got)
	}
	if tester.StatusCode() != 2 {
		t.Errorf("test-ref status %d, want 2", tester.StatusCode())
	}

	// check if the description from composer.json is correctly shown
	tester = getApplicationTester()
	if code, err := tester.RunArgs(commandtest.Options{}, "command", "run-script", "--list", true); err != nil || code != 0 {
		t.Fatalf("code %d, err %v", code, err)
	}
	if !strings.Contains(tester.Display(false), description) {
		t.Errorf("The contents of scripts-description for the test script should be printed:\n%s", tester.Display(false))
	}
}

func TestRunScriptCommand_ExecutionOfSymfonyCommandWithConfiguration(t *testing.T) {
	getApplicationTester := pluginTesters(t)

	const (
		cmdName   = "custom-cmd-123"
		cmdAlias  = cmdName + "-alias"
		cmdDesc   = "This is a Symfony command with custom configuration"
		wrongDesc = "this should be ignored"
	)

	commandtest.InitTempComposer(t, `{
		"scripts": {"`+cmdName+`": "Test\\MyCommandWithDefinitions"},
		"scripts-descriptions": {"`+cmdName+`": "`+wrongDesc+`"},
		"autoload": {"psr-4": {"Test\\": ""}}
	}`, nil, nil, true)

	if err := os.WriteFile("MyCommandWithDefinitions.php", []byte(`<?php

namespace Test;

use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Input\InputArgument;
use Symfony\Component\Console\Output\OutputInterface;
use Symfony\Component\Console\Command\Command;

class MyCommandWithDefinitions extends Command
{
    protected function configure(): void
    {
        $this
            ->setDescription('`+cmdDesc+`')
            ->setAliases(['`+cmdAlias+`'])
            ->setDefinition([new InputArgument('req-arg', InputArgument::REQUIRED, 'Required arg.')]);
    }

    public function execute(InputInterface $input, OutputInterface $output): int
    {
        $output->writeln($input->getArgument('req-arg'));
        return Command::SUCCESS;
    }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	// makes sure the command executes with the name defined inside its `configure()`...
	tester := getApplicationTester()
	if _, err := tester.RunArgs(commandtest.Options{}, "command", cmdName, "req-arg", "lala"); err != nil {
		t.Fatal(err)
	}
	if got := tester.Display(true); got != "lala\n" {
		t.Errorf("%s display %q", cmdName, got)
	}

	// ...with the alias defined there as well...
	tester = getApplicationTester()
	if _, err := tester.RunArgs(commandtest.Options{}, "command", cmdAlias, "req-arg", "lala"); err != nil {
		t.Fatal(err)
	}
	if got := tester.Display(true); got != "lala\n" {
		t.Errorf("%s display %q", cmdAlias, got)
	}

	// ...and also uses its own description, instead of the one in composer.scripts-descriptions
	tester = getApplicationTester()
	if code, err := tester.RunArgs(commandtest.Options{}, "command", "run-script", "--list", true); err != nil || code != 0 {
		t.Fatalf("code %d, err %v", code, err)
	}
	output := tester.Display(false)
	if !strings.Contains(output, cmdDesc) {
		t.Errorf("The custom description for the test command should be printed:\n%s", output)
	}
	if strings.Contains(output, wrongDesc) {
		t.Errorf("The default description for the test command must not be printed:\n%s", output)
	}
}

// runScriptProject is the project of the run-script cases beyond
// RunScriptCommandTest: a described script, one printing the dev mode,
// one outlasting the one-second process-timeout, and an event script.
func runScriptProject(t *testing.T) {
	commandtest.InitTempComposer(t, `{
		"scripts": {
			"hello": "echo hello",
			"mode": "echo dev=$COMPOSER_DEV_MODE",
			"slow": "sleep 2",
			"post-install-cmd": "echo installed"
		},
		"scripts-descriptions": {"hello": "Says hello."},
		"config": {"process-timeout": 1}
	}`, nil, nil, true)
}

// Beyond RunScriptCommandTest: the interactive select, the options and the
// arguments of a run.
func TestRunScriptCommand_Runs(t *testing.T) {
	skipShellScripts(t)
	runScript := func(kv ...any) []console.Param { return cmd("run-script", kv...) }
	runCommandCases(t, runScriptProject, []commandCase{
		{
			name:     "the select runs the chosen script",
			inputs:   []string{"hello"},
			params:   runScript(),
			contains: []string{"Script to run: ", "[hello", "Says hello.", "> echo hello\nhello\n"},
		},
		{
			// the select's choices are keyed by name: a description
			// answers for its name
			name:     "the select takes a script's description",
			inputs:   []string{"Says hello."},
			params:   runScript(),
			contains: []string{"> echo hello\nhello\n"},
		},
		{name: "--dev wins over --no-dev", params: runScript("script", "mode", "--dev", true, "--no-dev", true), contains: []string{"dev=1\n"}},
		{name: "--no-dev", params: runScript("script", "mode", "--no-dev", true), contains: []string{"dev=0\n"}},
		{name: "arguments without --", params: runScript("script", "hello", "args", []string{"extra", "args"}), contains: []string{"hello extra args\n"}},
		{name: "--timeout=0 lifts process-timeout", params: runScript("script", "slow", "--timeout", "0")},
		{name: "COMPOSER_PROCESS_TIMEOUT", env: map[string]string{"COMPOSER_PROCESS_TIMEOUT": "5"}, params: runScript("script", "slow")},
		{name: "an event script", params: runScript("script", "post-install-cmd"), contains: []string{"installed\n"}},
	})
}

// Beyond RunScriptCommandTest: the error paths of interact() and
// execute().
func TestRunScriptCommand_Errors(t *testing.T) {
	skipShellScripts(t)
	const badTimeout = "Timeout value must be numeric and positive if defined, or 0 for forever"
	cannotRun := func(s string) string { return `Script "` + s + `" cannot be run with this command` }
	undefined := func(s string) string { return `Script "` + s + `" is not defined in this package` }
	runScript := func(kv ...any) []console.Param { return cmd("run-script", kv...) }
	cases := []commandCase{
		{name: "an index is no script name", inputs: []string{"1"}, params: runScript(), err: `Invalid script name "1"`},
		{name: "an unknown name in the select", inputs: []string{"nope"}, params: runScript(), err: `Invalid script name "nope"`},
		{name: "no script without interaction", params: runScript("--no-interaction", true), err: `Missing required argument "script"`},
		{name: "an unknown script", params: runScript("script", "nope"), err: undefined("nope")},
		{name: "an event that runs no scripts", params: runScript("script", "post-package-install"), err: undefined("post-package-install")},
		{name: "an event script not defined", params: runScript("script", "pre-install-cmd"), err: undefined("pre-install-cmd")},
		{name: "--no-scripts", params: runScript("script", "hello", "--no-scripts", true), err: undefined("hello")},
		{
			name:   "the process-timeout",
			params: runScript("script", "slow"),
			err:    `The process "sleep 2" exceeded the timeout of 1 seconds.`,
		},
		{
			name:   "--timeout overrides process-timeout",
			env:    map[string]string{"COMPOSER_PROCESS_TIMEOUT": "5"},
			params: runScript("script", "slow", "--timeout", "1"),
			err:    `The process "sleep 2" exceeded the timeout of 1 seconds.`,
		},
	}
	for _, spelling := range []string{"PRE_INSTALL_CMD", "pre_install_cmd"} {
		cases = append(cases, commandCase{name: "the constant " + spelling, params: runScript("script", spelling), err: cannotRun(spelling)})
	}
	for _, timeout := range []string{"1.5", "-1", "abc", ""} {
		cases = append(cases, commandCase{name: "--timeout=" + timeout, params: runScript("script", "hello", "--timeout", timeout), err: badTimeout, excludes: []string{"hello"}})
	}
	runCommandCases(t, runScriptProject, cases)
}

// Beyond RunScriptCommandTest: --list without scripts writes nothing.
func TestRunScriptCommand_ListWithoutScripts(t *testing.T) {
	runCommandCases(t, func(t *testing.T) { commandtest.InitTempComposer(t, nil, nil, nil, true) }, []commandCase{
		{name: "--list", params: cmd("run-script", "--list", true), streams: &streams{}},
	})
}

// Beyond RunScriptCommandTest: a script alias runs its script and passes
// its arguments.
func TestScriptAliasCommand_RunsScript(t *testing.T) {
	commandtest.InitTempComposer(t, `{"scripts": {"greet": "echo hello"}}`, nil, nil, true)

	tester := commandtest.GetApplicationTester(t)
	code, err := tester.RunArgs(commandtest.Options{}, "command", "greet", "args", []string{"world"})
	if err != nil || code != 0 {
		t.Fatalf("code %d, err %v", code, err)
	}
	if got := tester.Display(true); got != "hello world\n" {
		t.Errorf("display %q", got)
	}
}

// Beyond RunScriptCommandTest: the constructor's checks.
func TestScriptAliasCommand_InvalidAliases(t *testing.T) {
	commandtest.InitTempComposer(t, `{"scripts": {"greet": "echo hello"}, "scripts-aliases": {"greet": ["ok", 1]}}`, nil, nil, true)

	tester := commandtest.GetApplicationTester(t)
	_, err := tester.RunArgs(commandtest.Options{}, "command", "greet")
	if err == nil || err.Error() != `"scripts-aliases" element array values should contain only strings` {
		t.Errorf("err %v", err)
	}
}
