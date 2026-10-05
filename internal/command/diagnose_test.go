// Ports tests/Composer/Test/Command/DiagnoseCommandTest.php.

package command_test

import (
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/command/commandtest"
)

// requirePackagist skips tests that, like Composer's, talk to packagist.org
// and GitHub when the network is unreachable.
func requirePackagist(t *testing.T) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", "repo.packagist.org:443", 3*time.Second)
	if err != nil {
		t.Skipf("packagist.org unreachable: %v", err)
	}
	_ = conn.Close()
}

func TestDiagnoseCommand_CmdFail(t *testing.T) {
	requirePackagist(t)
	commandtest.InitTempComposer(t, `{"name": "foo/bar", "description": "test pkg"}`, nil, nil, true)

	appTester := commandtest.GetApplicationTester(t)
	if _, err := appTester.RunArgs(commandtest.Options{}, "command", "diagnose"); err != nil {
		t.Fatal(err)
	}

	if os.Getenv("COMPOSER_LOWEST_DEPS_TEST") == "1" {
		if appTester.StatusCode() < 1 {
			t.Errorf("status %d", appTester.StatusCode())
		}
	} else if appTester.StatusCode() != 1 {
		t.Errorf("status %d, want 1\n%s", appTester.StatusCode(), appTester.Display(true))
	}

	output := appTester.Display(true)
	for _, want := range []string{
		"Checking composer.json: <warning>WARNING</warning>\n<warning>No license specified, it is recommended to do so. For closed-source software you may use \"proprietary\" as license.</warning>",
		"Checking http connectivity to packagist: OK\nChecking https connectivity to packagist: OK\nChecking github.com rate limit: ",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output lacks %q:\n%s", want, output)
		}
	}
}

func TestDiagnoseCommand_CmdSuccess(t *testing.T) {
	requirePackagist(t)
	commandtest.InitTempComposer(t, `{"name": "foo/bar", "description": "test pkg", "license": "MIT"}`, nil, nil, true)

	appTester := commandtest.GetApplicationTester(t)
	if _, err := appTester.RunArgs(commandtest.Options{}, "command", "diagnose"); err != nil {
		t.Fatal(err)
	}

	output := appTester.Display(true)
	if os.Getenv("COMPOSER_LOWEST_DEPS_TEST") != "1" && appTester.StatusCode() != 0 {
		t.Errorf("status %d, want 0\n%s", appTester.StatusCode(), output)
	}

	for _, want := range []string{
		"Checking composer.json: OK",
		"Checking http connectivity to packagist: OK\nChecking https connectivity to packagist: OK\nChecking github.com rate limit: ",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output lacks %q:\n%s", want, output)
		}
	}
}

// TestDiagnoseCommand_Help compares `help diagnose` with Composer's
// (testdata/help/diagnose.txt, tools/oracle/command/help_diagnose.php).
func TestDiagnoseCommand_Help(t *testing.T) {
	want, err := os.ReadFile("testdata/help/diagnose.txt")
	if err != nil {
		t.Fatal(err)
	}
	commandtest.InitTempComposer(t, nil, nil, nil, true)

	appTester := commandtest.GetApplicationTester(t)
	if code, err := appTester.RunArgs(commandtest.Options{}, "command", "help", "command_name", "diagnose"); err != nil || code != 0 {
		t.Fatalf("run: %d %v", code, err)
	}
	if got := appTester.Display(true); got != string(want) {
		t.Errorf("help differs:\n got %q\nwant %q", got, want)
	}
}

// TestDiagnoseCommand_NetworkDisabled runs every check offline: the
// network checks are skipped and the exit code reflects the rest.
func TestDiagnoseCommand_NetworkDisabled(t *testing.T) {
	t.Setenv("COMPOSER_DISABLE_NETWORK", "1")
	commandtest.InitTempComposer(t, `{"name": "foo/bar", "description": "test pkg", "license": "MIT"}`, nil, nil, true)

	appTester := commandtest.GetApplicationTester(t)
	if _, err := appTester.RunArgs(commandtest.Options{}, "command", "diagnose"); err != nil {
		t.Fatal(err)
	}

	output := appTester.Display(true)
	for _, want := range []string{
		"Composer version: 2.10.3\n",
		"Checking Composer and its dependencies for vulnerabilities: SKIP Network is disabled by COMPOSER_DISABLE_NETWORK.\n",
		"Checking composer.json: OK\n",
		"Checking http connectivity to packagist: SKIP Network is disabled by COMPOSER_DISABLE_NETWORK.\n",
		"Checking https connectivity to packagist: SKIP Network is disabled by COMPOSER_DISABLE_NETWORK.\n",
		"Checking github.com rate limit: SKIP Network is disabled by COMPOSER_DISABLE_NETWORK.\n",
		"Checking disk free space: OK\n",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output lacks %q:\n%s", want, output)
		}
	}
}
