package command_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/pkg"
)

// What the input may have meant is its own item of the rendered error,
// after the message, which keeps Composer's text (and so its "Did you
// mean" block) for plugins: a command's alternatives, a mistyped option's
// and an installed package's. Decorated, only escape sequences differ.
func TestDidYouMeanRendering(t *testing.T) {
	commandtest.InitTempComposer(t, `{"require": {"vendor/package": "^1.0"}}`, nil, nil, true)
	installed := []pkg.PackageInterface{commandtest.GetPackage(t, "vendor/package", "1.0.0")}
	commandtest.CreateInstalledJSON(t, installed, nil, true)
	commandtest.CreateComposerLock(t, installed, nil)

	for _, tc := range []struct {
		args    []console.Param
		message string
		want    string
		code    int
	}{
		{[]console.Param{console.P("command", "instll")}, `Command "instll" is not defined.`, "  Did you mean this? install", 1},
		{[]console.Param{console.P("command", "install"), console.P("--dry-runn", true)}, `The "--dry-runn" option does not exist.`, "  Did you mean this? --dry-run", 1},
		{[]console.Param{console.P("command", "show"), console.P("package", "vendor/pakage")}, `Package "vendor/pakage" not found, try using --available (-a) to show all available packages.`, "  Did you mean this? vendor/package", 1},
	} {
		t.Run(tc.message, func(t *testing.T) {
			run := func(decorated bool) (int, string) {
				app := commandtest.NewApplication()
				app.SetAutoExit(false)
				app.SetCatchExceptions(true)
				tester := commandtest.NewApplicationTester(t, app)
				code, err := tester.Run(tc.args, commandtest.Options{Decorated: &decorated, CaptureStderrSeparately: true, Interactive: new(false)})
				if err != nil {
					t.Fatal(err)
				}

				return code, tester.ErrorOutput(true)
			}
			code, plain := run(false)
			if code != tc.code {
				t.Errorf("exit code %d, want %d", code, tc.code)
			}
			lines := strings.Split(plain, "\n")
			if !strings.Contains(plain, "Error: "+tc.message+"\n") {
				t.Errorf("stderr lacks the message %q:\n%s", tc.message, plain)
			}
			if n := strings.Count(plain, "Did you mean"); n != 1 || !slices.Contains(lines, tc.want) {
				t.Errorf("stderr does not offer %q once (%d times):\n%s", tc.want, n, plain)
			}
			// decorated, the usage is wrapped to the terminal
			_, decorated := run(true)
			words := func(s string) string { return strings.Join(strings.Fields(s), " ") }
			if got := regexp.MustCompile("\x1b\\[[0-9;]*m").ReplaceAllString(decorated, ""); words(got) != words(plain) {
				t.Errorf("decorated stderr without escapes:\n%s\nundecorated:\n%s", got, plain)
			}
		})
	}
}
