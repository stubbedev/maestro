// Ports tests/Composer/Test/Command/DiagnoseCommandTest.php.

package command_test

import (
	"bytes"
	"context"
	gojson "encoding/json"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/platform"
	"github.com/stubbedev/maestro/internal/util"
	utilhttp "github.com/stubbedev/maestro/internal/util/http"
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
	if appTester.StatusCode() != 0 {
		t.Errorf("status %d, want 0\n%s", appTester.StatusCode(), output)
	}
	for _, want := range []string{
		"Composer version: " + composer.Version + "\n",
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

// TestDiagnoseCommand_GithubOauth checks a github-oauth domain's token
// against the domain's API: an invalid token (401) and a valid one with an
// expiry. Every other connection goes to a proxy that refuses it, so the
// network checks fail fast and the exit code is 2.
func TestDiagnoseCommand_GithubOauth(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{"invalid", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}, `The oauth token for DOMAIN seems invalid, run "composer config --global --unset github-oauth.DOMAIN" to remove it`},
		{"expires", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v3/" || r.Header.Get("Authorization") == "" {
				w.WriteHeader(http.StatusNotFound)

				return
			}
			w.Header().Set("GitHub-Authentication-Token-Expiration", "2030-01-01 00:00:00 UTC")
			_, _ = w.Write([]byte("{}"))
		}, "OK expires on 2030-01-01 00:00:00 UTC"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewTLSServer(tc.handler)
			defer srv.Close()
			domain := srv.Listener.Addr().String()
			cafile := filepath.Join(t.TempDir(), "ca.pem")
			if err := os.WriteFile(cafile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Setenv("COMPOSER_DISABLE_NETWORK", "")
			t.Setenv("http_proxy", "http://127.0.0.1:1")
			t.Setenv("https_proxy", "http://127.0.0.1:1")
			t.Setenv("no_proxy", "127.0.0.1")
			utilhttp.ResetProxyManager()
			t.Cleanup(utilhttp.ResetProxyManager)
			composerJSON, _ := gojson.Marshal(map[string]any{
				"name": "foo/bar", "description": "test pkg", "license": "MIT",
				"config": map[string]any{"cafile": cafile, "github-oauth": map[string]any{domain: "token"}, "github-domains": []any{domain}},
			})
			commandtest.InitTempComposer(t, string(composerJSON), nil, nil, true)

			appTester := commandtest.GetApplicationTester(t)
			if _, err := appTester.RunArgs(commandtest.Options{}, "command", "diagnose"); err != nil {
				t.Fatal(err)
			}
			output := appTester.Display(true)
			if appTester.StatusCode() != 2 {
				t.Errorf("status %d, want 2\n%s", appTester.StatusCode(), output)
			}
			want := "Checking " + domain + " oauth access: " + strings.ReplaceAll(tc.want, "DOMAIN", domain) + "\n"
			if !strings.Contains(output, want) {
				t.Errorf("output lacks %q:\n%s", want, output)
			}
			if strings.Contains(output, "rate limit") {
				t.Errorf("output has the rate limit check:\n%s", output)
			}
		})
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

// TestDiagnoseCommand_NoPHP runs diagnose with no php on PATH: "Checking
// PHP" fails in place of the PHP's lines and its platform settings, the
// network checks run (allow_url_fopen only gates PHP's streams), and the
// exit code is 2.
func TestDiagnoseCommand_NoPHP(t *testing.T) {
	requirePackagist(t)
	commandtest.InitTempComposer(t, `{"name": "foo/bar", "description": "test pkg", "license": "MIT"}`, nil, nil, true)

	detector := &platform.Detector{FindPHP: func() (string, bool) { return "", false }}
	app := command.NewApplication(&composer.Factory{Runtime: composer.NewRuntime("test", detector)})
	app.SetAutoExit(false)
	app.SetCatchExceptions(false)
	appTester := commandtest.NewApplicationTester(t, app)
	if _, err := appTester.RunArgs(commandtest.Options{}, "command", "diagnose"); err != nil {
		t.Fatal(err)
	}

	output := appTester.Display(true)
	if appTester.StatusCode() != 2 {
		t.Errorf("status %d, want 2\n%s", appTester.StatusCode(), output)
	}
	for _, want := range []string{
		"Checking Composer and its dependencies for vulnerabilities: OK\n",
		"Checking PHP: FAIL\nNo php binary was found in PATH. maestro runs PHP code (platform detection, plugins, scripts) with the php first on PATH: install PHP or put it on PATH.\nzip: ",
		"Checking composer.json: OK\n",
		"Checking http connectivity to packagist: OK\nChecking https connectivity to packagist: OK\n",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output lacks %q:\n%s", want, output)
		}
	}
	for _, unwanted := range []string{"PHP version:", "OpenSSL version:", "curl version:", "Checking platform settings:"} {
		if strings.Contains(output, unwanted) {
			t.Errorf("output has %q:\n%s", unwanted, output)
		}
	}
}

// TestDiagnoseCommand_PlatformWarningsWindowsEOL checks that checkPlatform
// builds its messages with PHP_EOL, "\r\n" on Windows, as the output's own
// line endings are.
func TestDiagnoseCommand_PlatformWarningsWindowsEOL(t *testing.T) {
	php.SetEOLForTest(t, "\r\n")
	t.Setenv("COMPOSER_DISABLE_NETWORK", "1")
	t.Setenv("COMPOSER_IPRESOLVE", "")
	probe, err := os.ReadFile("../composer/testdata/platform/php84.probe")
	if err != nil {
		t.Fatal(err)
	}
	commandtest.InitTempComposer(t, `{"name": "foo/bar", "description": "test pkg", "require": {"acme/lib": "*"}}`, nil, nil, true)

	appTester := diagnoseWithPlatform(t, probe, "'./configure'  '--enable-sigchild' '--with-curlwrappers'")
	if _, err := appTester.RunArgs(commandtest.Options{}, "command", "diagnose"); err != nil {
		t.Fatal(err)
	}
	output := appTester.Display(false)
	for _, want := range []string{
		// checkComposerSchema
		"Checking composer.json: <warning>WARNING</warning>\r\n" +
			"<warning>No license specified, it is recommended to do so. For closed-source software you may use \"proprietary\" as license.</warning>\r\n" +
			"<warning>require.acme/lib : unbound version constraints (*) should be avoided</warning>\r\n" +
			"Checking platform settings: ",
		// checkPlatform
		"Checking platform settings: PHP was compiled with --enable-sigchild which can cause issues on some platforms.\r\n" +
			"Recompile it without this flag if possible, see also:\r\n" +
			"  https://bugs.php.net/bug.php?id=22999\r\n" +
			"PHP was compiled with --with-curlwrappers which will cause issues with HTTP authentication and GitHub.\r\n" +
			" Recompile it without this flag if possible\r\n" +
			"Checking git settings: ",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output lacks %q:\n%q", want, output)
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
		"1.0.0": "Checking Composer version: SKIP Network is disabled by COMPOSER_DISABLE_NETWORK.\nComposer version: " + composer.Version + "\n",
		"dev":   "Composer version: " + composer.Version + "\n",
		"test":  "Composer version: " + composer.Version + "\n",
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
