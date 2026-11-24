// Ports tests/Composer/Test/Command/StatusCommandTest.php.

package command_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/switches"
	"github.com/stubbedev/maestro/internal/util"
)

func TestStatusCommand_StatusForInstalledDistFiles(t *testing.T) {
	directory := commandtest.InitTempComposer(t, `{"require": {"root/req": "1.*"}, "repositories": []}`, nil, nil, true)
	if err := os.WriteFile("package.txt", []byte("original content"), 0o666); err != nil {
		t.Fatal(err)
	}
	p := commandtest.GetPackage(t, "root/req", "1.0.0")
	p.SetDistType(pkg.Str("file"))
	p.SetDistURL(pkg.Str("file://" + directory + "/package.txt"))
	p.SetInstallationSource(pkg.Some(pkg.FromDist))
	commandtest.CreateComposerLock(t, []pkg.PackageInterface{p}, nil)
	appTester := commandtest.GetApplicationTester(t)
	if _, err := appTester.RunArgs(commandtest.Options{}, "command", "install"); err != nil {
		t.Fatal(err)
	}
	if appTester.StatusCode() != 0 {
		t.Fatalf("install status %d: %s", appTester.StatusCode(), appTester.Display(false))
	}

	appTester = commandtest.GetApplicationTester(t)
	if _, err := appTester.RunArgs(commandtest.Options{}, "command", "status", "--verbose", true); err != nil {
		t.Fatal(err)
	}
	if appTester.StatusCode() != 0 {
		t.Fatalf("status %d: %s", appTester.StatusCode(), appTester.Display(false))
	}
	if got := php.Trim(appTester.Display(true)); got != "No local changes" {
		t.Errorf("got %q", got)
	}

	if err := os.WriteFile("vendor/root/req/package.txt", []byte("modified content"), 0o666); err != nil {
		t.Fatal(err)
	}
	appTester = commandtest.GetApplicationTester(t)
	if _, err := appTester.RunArgs(commandtest.Options{}, "command", "status", "--verbose", true); err != nil {
		t.Fatal(err)
	}
	if appTester.StatusCode() != 1 {
		t.Errorf("status %d, want 1", appTester.StatusCode())
	}
	display := appTester.Display(true)
	if !strings.Contains(display, "package.txt") {
		t.Errorf("display lacks package.txt:\n%s", display)
	}
	if strings.Contains(display, "Failed to detect changes:") {
		t.Errorf("display holds Failed to detect changes:\n%s", display)
	}
}

func TestStatusCommand_NoLocalChanges(t *testing.T) {
	commandtest.InitTempComposer(t, `{"require": {"root/req": "1.*"}}`, nil, nil, true)

	p := commandtest.GetPackage(t, "root/req", "1.0.0")
	p.SetType("metapackage")

	commandtest.CreateComposerLock(t, []pkg.PackageInterface{p}, nil)
	commandtest.CreateInstalledJSON(t, []pkg.PackageInterface{p}, nil, true)

	appTester := commandtest.GetApplicationTester(t)
	if _, err := appTester.RunArgs(commandtest.Options{}, "command", "status"); err != nil {
		t.Fatal(err)
	}

	if got := php.Trim(appTester.Display(true)); got != "No local changes" {
		t.Errorf("got %q", got)
	}
}

// TestStatusCommand_LocallyModifiedPackages installs real packages from
// GitHub as the PHP test does, so it needs the network: it runs only with
// MAESTRO_NETWORK_TESTS=1.
func TestStatusCommand_LocallyModifiedPackages(t *testing.T) {
	if !switches.On(switches.NetworkTests) {
		t.Skip("needs the network (set MAESTRO_NETWORK_TESTS=1)")
	}
	cases := []struct {
		name               string
		composerJSON       string
		commandFlags       []console.Param
		pkgName, version   string
		installationSource pkg.InstallationSource
		typ, url, ref      string
	}{
		{
			name:               "locally modified package from source",
			composerJSON:       `{"require": {"composer/class-map-generator": "^1.0"}}`,
			pkgName:            "composer/class-map-generator",
			version:            "1.1",
			installationSource: pkg.FromSource,
			typ:                "git",
			url:                "https://github.com/composer/class-map-generator.git",
			ref:                "953cc4ea32e0c31f2185549c7d216d7921f03da9",
		},
		{
			name:               "locally modified package from dist",
			composerJSON:       `{"require": {"composer/ca-bundle": "^1.5"}}`,
			commandFlags:       []console.Param{console.P("--verbose", true)},
			pkgName:            "composer/ca-bundle",
			version:            "1.5.12",
			installationSource: pkg.FromDist,
			typ:                "zip",
			url:                "https://api.github.com/repos/composer/ca-bundle/zipball/00a2f4201641d5c53f7fc0195e6c8d9fcc321a78",
			ref:                "00a2f4201641d5c53f7fc0195e6c8d9fcc321a78",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			commandtest.InitTempComposer(t, tc.composerJSON, nil, nil, true)

			p := commandtest.GetPackage(t, tc.pkgName, tc.version)
			p.SetInstallationSource(pkg.Some(tc.installationSource))
			if tc.installationSource == pkg.FromSource {
				p.SetSourceType(pkg.Str(tc.typ))
				p.SetSourceURL(pkg.Str(tc.url))
				p.SetSourceReference(pkg.Str(tc.ref))
			}
			if tc.installationSource == "dist" {
				p.SetDistType(pkg.Str(tc.typ))
				p.SetDistURL(pkg.Str(tc.url))
				p.SetDistReference(pkg.Str(tc.ref))
			}

			commandtest.CreateComposerLock(t, []pkg.PackageInterface{p}, nil)

			appTester := commandtest.GetApplicationTester(t)
			if _, err := appTester.RunArgs(commandtest.Options{}, "command", "install"); err != nil {
				t.Fatal(err)
			}

			cwd, _ := os.Getwd()
			if err := os.WriteFile(cwd+"/vendor/"+tc.pkgName+"/composer.json", []byte("{}"), 0o666); err != nil {
				t.Fatal(err)
			}

			params := append([]console.Param{console.P("command", "status")}, tc.commandFlags...)
			if _, err := appTester.Run(params, commandtest.Options{}); err != nil {
				t.Fatal(err)
			}

			actual := php.Trim(appTester.Display(true))
			for _, want := range []string{"You have changes in the following dependencies:", tc.pkgName} {
				if !strings.Contains(actual, want) {
					t.Errorf("%q does not contain %q", actual, want)
				}
			}
		})
	}
}

func TestStatusCommand_NoLocalChangesForPathRepositoryPackages(t *testing.T) {
	cases := []struct {
		name                       string
		symlink                    bool
		packageHasOwnGitRepository bool
	}{
		{"symlinked", true, false},
		{"mirrored", false, false},
		{"symlinked with its own git repository", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			statusInitPathRepositoryProject(t, tc.symlink, tc.packageHasOwnGitRepository)

			appTester := statusRun(t, "status", "--verbose", true)

			if got := php.Trim(appTester.Display(true)); got != "No local changes" {
				t.Errorf("got %q", got)
			}
			if code := appTester.StatusCode(); code != 0 {
				t.Errorf("status %d, want 0", code)
			}
		})
	}
}

func TestStatusCommand_VersionVariationOfPathRepositoryPackageWithOwnGitRepository(t *testing.T) {
	cases := []struct {
		name                    string
		replaceWithShallowClone bool
	}{
		{"new commit", false},
		{"shallow clone of a new commit", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := statusInitPathRepositoryProject(t, true, true)
			statusGit(t, dir+"/packages/bar", "commit", "--allow-empty", "-m", "new commit after locking")
			if tc.replaceWithShallowClone {
				if err := os.Rename(dir+"/packages/bar", dir+"/bar-origin"); err != nil {
					t.Fatal(err)
				}
				statusGit(t, dir, "clone", "-q", "--no-local", "--depth", "1", dir+"/bar-origin", dir+"/packages/bar")
			}

			appTester := statusRun(t, "status", "--verbose", true)

			display := appTester.Display(true)
			for _, want := range []string{"You have version variations in the following dependencies:", "vendor/test/bar"} {
				if !strings.Contains(display, want) {
					t.Errorf("%q does not contain %q", display, want)
				}
			}
		})
	}
}

func TestStatusCommand_VersionVariationOfPathPackageInGitWorktree(t *testing.T) {
	statusSkipWithoutGit(t)

	dir := commandtest.InitTempComposer(t, nil, nil, nil, true)
	if err := os.Mkdir(dir+"/bar-repository", 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/bar-repository/composer.json", []byte(`{"name": "test/bar"}`), 0o666); err != nil {
		t.Fatal(err)
	}
	statusGit(t, dir+"/bar-repository", "init", "-q")
	statusGit(t, dir+"/bar-repository", "add", "-A")
	statusGit(t, dir+"/bar-repository", "commit", "-m", "init")
	statusGit(t, dir+"/bar-repository", "worktree", "add", "-b", "feature", dir+"/bar")
	commit := php.Trim(statusGit(t, dir+"/bar", "rev-parse", "HEAD"))

	composerJSON := `{"repositories":[{"type":"package","package":{"name":"test/bar","version":"1.0.0","dist":{"type":"path","url":"bar","reference":"` + commit + `"}}},{"packagist.org":false}],"require":{"test/bar":"1.0.0"}}`
	if err := os.WriteFile(dir+"/composer.json", []byte(composerJSON), 0o666); err != nil {
		t.Fatal(err)
	}

	if code := statusRun(t, "install").StatusCode(); code != 0 {
		t.Fatalf("install status %d", code)
	}

	statusGit(t, dir+"/bar", "commit", "--allow-empty", "-m", "new commit after locking")
	appTester := statusRun(t, "status", "--verbose", true)

	display := appTester.Display(true)
	for _, want := range []string{"You have version variations in the following dependencies:", "vendor/test/bar"} {
		if !strings.Contains(display, want) {
			t.Errorf("%q does not contain %q", display, want)
		}
	}
}

func TestStatusCommand_SymlinkReplacingMirroredPathRepositoryPackageIsReported(t *testing.T) {
	if util.IsWindows() {
		t.Skip("Creating a directory symlink requires extra privileges on Windows")
	}

	dir := statusInitPathRepositoryProject(t, false, false)

	if err := os.RemoveAll(dir + "/vendor/test/bar"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dir+"/packages/bar", dir+"/vendor/test/bar"); err != nil {
		t.Fatal(err)
	}

	appTester := statusRun(t, "status", "--verbose", true)

	if display := appTester.Display(true); !strings.Contains(display, "vendor/test/bar is a symbolic link.") {
		t.Errorf("symbolic link not reported:\n%s", display)
	}
	if code := appTester.StatusCode(); code != 1 {
		t.Errorf("status %d, want 1", code)
	}
}

func TestStatusCommand_SymlinkedDistPackageIsReported(t *testing.T) {
	if util.IsWindows() {
		t.Skip("Creating a directory symlink requires extra privileges on Windows")
	}

	artifact, err := os.ReadFile(filepath.Join("..", "repository", "testdata", "Fixtures", "artifacts", "package0.zip"))
	if err != nil {
		t.Fatal(err)
	}
	dir := commandtest.InitTempComposer(t, `{"repositories": [{"type": "artifact", "url": "artifacts/"}], "require": {"vendor0/package0": "0.0.1"}}`, nil, nil, true)
	if err := os.Mkdir(dir+"/artifacts", 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/artifacts/package0.zip", artifact, 0o666); err != nil {
		t.Fatal(err)
	}

	if code := statusRun(t, "install").StatusCode(); code != 0 {
		t.Fatalf("install status %d", code)
	}

	if err := os.Rename(dir+"/vendor/vendor0/package0", dir+"/package0"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dir+"/package0", dir+"/vendor/vendor0/package0"); err != nil {
		t.Fatal(err)
	}

	appTester := statusRun(t, "status", "--verbose", true)

	if display := appTester.Display(true); !strings.Contains(display, "vendor/vendor0/package0 is a symbolic link.") {
		t.Errorf("symbolic link not reported:\n%s", display)
	}
	if code := appTester.StatusCode(); code != 1 {
		t.Errorf("status %d, want 1", code)
	}
}

func statusRun(t *testing.T, command string, kv ...any) *commandtest.ApplicationTester {
	t.Helper()
	appTester := commandtest.GetApplicationTester(t)
	if _, err := appTester.RunArgs(commandtest.Options{}, append([]any{"command", command}, kv...)...); err != nil {
		t.Fatal(err)
	}

	return appTester
}

func statusSkipWithoutGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

// statusInitPathRepositoryProject ports initPathRepositoryProject: it
// installs test/bar from a path repository inside a git repository, the
// layout of a project with local packages.
func statusInitPathRepositoryProject(t *testing.T, symlink, packageHasOwnGitRepository bool) string {
	t.Helper()
	statusSkipWithoutGit(t)

	dir := commandtest.InitTempComposer(t, php.ArrayOf(
		"repositories", php.ListOf(php.ArrayOf("type", "path", "url", "packages/*", "options", php.ArrayOf("symlink", symlink))),
		"require", php.ArrayOf("test/bar", "^1.0"),
	), nil, nil, true)
	if err := os.MkdirAll(dir+"/packages/bar", 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/packages/bar/composer.json", []byte(`{"name": "test/bar", "version": "1.0.0"}`), 0o666); err != nil {
		t.Fatal(err)
	}
	if packageHasOwnGitRepository {
		statusGit(t, dir+"/packages/bar", "init", "-q")
		statusGit(t, dir+"/packages/bar", "add", "-A")
		statusGit(t, dir+"/packages/bar", "commit", "-m", "init")
	}

	if err := os.WriteFile(dir+"/.gitignore", []byte("/vendor/\n/composer-home/\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	statusGit(t, dir, "init", "-q")
	statusGit(t, dir, "add", "-A")
	statusGit(t, dir, "commit", "-m", "init")

	if code := statusRun(t, "install").StatusCode(); code != 0 {
		t.Fatalf("install status %d", code)
	}

	return dir
}

// statusGit ports git(): it runs git in cwd with a fixed identity and
// returns its output.
func statusGit(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=Composer", "-c", "user.email=composer@example.org", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = cwd
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Failed to execute git %s: %v: %s", strings.Join(args, " "), err, stderr.String())
	}

	return string(out)
}
