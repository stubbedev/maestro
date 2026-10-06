// Ports tests/Composer/Test/Command/DiagnoseCommandTest.php.

package command_test

import (
	"bytes"
	"context"
	gojson "encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/platform"
	"github.com/stubbedev/maestro/internal/util"
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

// diagnoseWithPlatform returns an application tester whose php is the PHP
// 8.4 build recorded in probe (internal/composer/testdata/platform), with
// the given phpinfo(INFO_GENERAL) "Configure Command".
func diagnoseWithPlatform(t *testing.T, probe []byte, configure string) *commandtest.ApplicationTester {
	t.Helper()
	enc, _ := gojson.Marshal(configure)
	probe = bytes.Replace(probe, []byte(`{"format":1,`), []byte(`{"format":1,"configure_command":`+string(enc)+`,`), 1)
	snapshot, err := platform.ParseSnapshot("/usr/bin/php", probe)
	if err != nil {
		t.Fatal(err)
	}
	detector := &platform.Detector{
		FindPHP: func() (string, bool) { return "/usr/bin/php", true },
		Probe:   func(context.Context, string) (*platform.Snapshot, error) { return snapshot, nil },
	}
	app := command.NewApplication(&composer.Factory{Runtime: composer.NewRuntime("test", detector)})
	app.SetAutoExit(false)
	app.SetCatchExceptions(false)

	return commandtest.NewApplicationTester(t, app)
}

// TestDiagnoseCommand_ConfigureCommandWarnings checks checkPlatform's
// sigchild and curlwrappers warnings, from the "Configure Command" the
// platform probe records.
func TestDiagnoseCommand_ConfigureCommandWarnings(t *testing.T) {
	t.Setenv("COMPOSER_DISABLE_NETWORK", "1")
	t.Setenv("COMPOSER_IPRESOLVE", "")
	probe, err := os.ReadFile("../composer/testdata/platform/php84.probe")
	if err != nil {
		t.Fatal(err)
	}
	commandtest.InitTempComposer(t, `{"name": "foo/bar", "description": "test pkg", "license": "MIT"}`, nil, nil, true)

	for _, tc := range []struct {
		configure, want string
	}{
		{
			"'./configure'  '--prefix=/usr' '--enable-sigchild' '--with-curlwrappers'",
			"Checking platform settings: PHP was compiled with --enable-sigchild which can cause issues on some platforms.\n" +
				"Recompile it without this flag if possible, see also:\n" +
				"  https://bugs.php.net/bug.php?id=22999\n" +
				"PHP was compiled with --with-curlwrappers which will cause issues with HTTP authentication and GitHub.\n" +
				" Recompile it without this flag if possible\n" +
				"Checking git settings: ",
		},
		{
			"'./configure'  '--with-curlwrappers'",
			"Checking platform settings: PHP was compiled with --with-curlwrappers which will cause issues with HTTP authentication and GitHub.\n" +
				" Recompile it without this flag if possible\n" +
				"Checking git settings: ",
		},
		{"'./configure'  '--prefix=/usr'", "Checking platform settings: OK\n"},
	} {
		appTester := diagnoseWithPlatform(t, probe, tc.configure)
		if _, err := appTester.RunArgs(commandtest.Options{}, "command", "diagnose"); err != nil {
			t.Fatal(err)
		}
		if output := appTester.Display(true); !strings.Contains(output, tc.want) {
			t.Errorf("%s: output lacks %q:\n%s", tc.configure, tc.want, output)
		}
	}
}

// diagnoseTester is an application whose diagnose runs as the release
// build current of maestro, installed at a temporary path, reading its
// releases from rs.
func diagnoseTester(t *testing.T, rs *releaseServer, current string) (*commandtest.ApplicationTester, *command.DiagnoseCommand) {
	t.Helper()
	appTester := commandtest.GetApplicationTester(t)
	cmd, err := appTester.Application.Find("diagnose")
	if err != nil {
		t.Fatal(err)
	}
	d := cmd.(*command.DiagnoseCommand)
	d.APIBase = rs.URL
	exe := filepath.Join(t.TempDir(), "maestro")
	d.Executable = func() (string, error) { return exe, nil }
	d.CurrentVersion = current

	return appTester, d
}

// TestDiagnoseCommand_VersionCheck is checkVersion against maestro's
// releases, the latest one found as self-update finds it.
func TestDiagnoseCommand_VersionCheck(t *testing.T) {
	rs := newReleaseServer(t, "1.2.0", "NEW")
	rs.prerelase = "1.3.0-RC1"

	for _, tc := range []struct {
		current, channel string
		want             any
	}{
		{"1.0.0+abc", "", "<comment>You are not running the latest stable version, run `composer self-update` to update (1.0.0 => 1.2.0)</comment>"},
		{"1.2.0", "", true},
		{"1.2.1", "", true},
		{"1.2.0", "preview", "<comment>You are not running the latest preview version, run `composer self-update` to update (1.2.0 => 1.3.0-RC1)</comment>"},
		{"1.3.0-RC1", "snapshot", true},
	} {
		commandtest.InitTempComposer(t, nil, nil, nil, true)
		if tc.channel != "" {
			home, _ := util.GetEnv("COMPOSER_HOME")
			if err := os.MkdirAll(home, 0o777); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, "maestro-update-channel"), []byte(tc.channel+"\n"), 0o666); err != nil {
				t.Fatal(err)
			}
		}
		_, d := diagnoseTester(t, rs, tc.current)
		got, err := command.DiagnoseCheckVersion(d)
		if err != nil || got != tc.want {
			t.Errorf("%s (%s): got %#v, %v, want %#v", tc.current, tc.channel, got, err, tc.want)
		}
	}

	// A failed request is the check's result (outputResult renders a FAIL).
	commandtest.InitTempComposer(t, nil, nil, nil, true)
	_, d := diagnoseTester(t, rs, "1.0.0")
	d.APIBase = rs.URL + "/nope"
	got, err := command.DiagnoseCheckVersion(d)
	if gotErr, ok := got.(error); err != nil || !ok || !isPHPInstance(gotErr, `Composer\Downloader\TransportException`) {
		t.Errorf("got %#v, %v", got, err)
	}
}

// TestDiagnoseCommand_VersionCheckPlacement checks where the version line
// goes, that only a release build has one (in Composer, only the phar)
// and that the pubkeys line never shows.
func TestDiagnoseCommand_VersionCheckPlacement(t *testing.T) {
	t.Setenv("COMPOSER_DISABLE_NETWORK", "1")
	rs := newReleaseServer(t, "1.2.0", "NEW")

	for current, want := range map[string]string{
		"1.0.0": "Checking Composer version: SKIP Network is disabled by COMPOSER_DISABLE_NETWORK.\nComposer version: 2.10.3\n",
		"dev":   "Composer version: 2.10.3\n",
		"test":  "Composer version: 2.10.3\n",
	} {
		commandtest.InitTempComposer(t, `{"name": "foo/bar", "version": "1.0.0", "description": "test pkg", "license": "MIT"}`, nil, nil, true)
		appTester, _ := diagnoseTester(t, rs, current)
		if _, err := appTester.RunArgs(commandtest.Options{}, "command", "diagnose"); err != nil {
			t.Fatal(err)
		}
		output := appTester.Display(true)
		if !strings.HasPrefix(output, want) {
			t.Errorf("%s: output does not start with %q:\n%s", current, want, output)
		}
		if strings.Contains(output, "pubkeys") {
			t.Errorf("%s: output has a pubkeys check:\n%s", current, output)
		}
	}
}
