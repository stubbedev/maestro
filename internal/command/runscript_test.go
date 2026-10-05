// Ports tests/Composer/Test/Command/RunScriptCommandTest.php.

package command_test

import (
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/php"
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

func TestRunScriptCommand_ExecutionOfSimpleSymfonyCommand(t *testing.T) {
	t.Skip("needs the plugin runtime: a script naming a Symfony Command class is a PHP command (Application's ScriptCommandProvider)")
}

func TestRunScriptCommand_ExecutionOfSymfonyCommandWithConfiguration(t *testing.T) {
	t.Skip("needs the plugin runtime: a script naming a Symfony Command class is a PHP command (Application's ScriptCommandProvider)")
}

// Beyond RunScriptCommandTest: the error paths of execute().
func TestRunScriptCommand_Errors(t *testing.T) {
	commandtest.InitTempComposer(t, `{"scripts": {"test": "echo hi"}}`, nil, nil, true)

	for _, tc := range []struct {
		args []any
		want string
	}{
		{[]any{"script", "PRE_INSTALL_CMD"}, `Script "PRE_INSTALL_CMD" cannot be run with this command`},
		{[]any{"script", "nope"}, `Script "nope" is not defined in this package`},
		{[]any{"script", "test", "--timeout", "1.5"}, "Timeout value must be numeric and positive if defined, or 0 for forever"},
		{nil, `Missing required argument "script"`},
	} {
		tester := commandtest.GetApplicationTester(t)
		args := append([]any{"command", "run-script", "--no-interaction", true}, tc.args...)
		_, err := tester.RunArgs(commandtest.Options{}, args...)
		if err == nil || err.Error() != tc.want {
			t.Errorf("%v: err %v, want %q", tc.args, err, tc.want)
		}
	}
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
