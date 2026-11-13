package vcs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util/http/httpmock"
	"github.com/stubbedev/maestro/internal/util/processmock"
)

// Ports tests/Composer/Test/Repository/Vcs/GitDriverTest.php.

func newGitDriverTest(t *testing.T, url string) (*GitDriver, *processmock.Mock, *testIO, string) {
	t.Helper()

	home := t.TempDir()
	process := processmock.New()
	ioi := newTestIO()
	driver := NewGitDriver(php.ArrayOf("url", url), deps(ioi, newConfig(t, home), httpmock.New(), process))

	return driver, process, ioi, home
}

func TestGitDriver_GetRootIdentifierFromRemoteLocalRepository(t *testing.T) {
	driver, process, _, home := newGitDriverTest(t, "")
	driver.url = home
	driver.repoDir = home

	process.Expects([]processmock.Expectation{
		{Cmd: processmock.Cmd("git", "branch", "--no-color").Cmd, Stdout: "* main\n  2.2\n  1.10"},
	}, true, nil)

	root, err := driver.RootIdentifier()
	noErr(t, err)

	if root != "main" {
		t.Fatalf("root = %q", root)
	}

	assertProcessComplete(t, process)
}

func TestGitDriver_GetRootIdentifierFromRemote(t *testing.T) {
	t.Setenv("COMPOSER_DISABLE_NETWORK", "")

	driver, process, ioi, home := newGitDriverTest(t, "https://example.org/acme.git")
	driver.repoDir = home

	stdout := "* remote origin\n  Fetch URL: https://example.org/acme.git\n  Push  URL: https://example.org/acme.git\n  HEAD branch: main\n  Remote branches:\n    1.10                       tracked\n    2.2                        tracked\n    main                       tracked"

	process.Expects([]processmock.Expectation{
		processmock.Cmd("git", "remote", "-v"),
		processmock.Cmd("git", "remote", "set-url", "origin", "--", "https://example.org/acme.git"),
		{Cmd: processmock.Cmd("git", "remote", "show", "origin").Cmd, Stdout: stdout},
		processmock.Cmd("git", "remote", "set-url", "origin", "--", "https://example.org/acme.git"),
	}, false, nil)

	root, err := driver.RootIdentifier()
	noErr(t, err)

	if root != "main" {
		t.Fatalf("root = %q", root)
	}

	assertProcessComplete(t, process)

	if msgs := ioi.messages(); len(msgs) != 0 {
		t.Fatalf("unexpected output %q", msgs)
	}
}

func newMirrorSyncDriver(t *testing.T, gitVersion string, existingMirror bool) (*GitDriver, *processmock.Mock, *testIO, string, string) {
	t.Helper()

	t.Setenv("COMPOSER_DISABLE_NETWORK", "")
	pinGitVersion(t, gitVersion)

	url := "https://example.org/acme.git"
	cacheVcsDir := filepath.Join(t.TempDir(), "cache", "vcs")
	cfg := newConfig(t, t.TempDir(), "cache-vcs-dir", cacheVcsDir)
	repoDir := cacheVcsDir + "/https---example.org-acme.git/"
	if existingMirror {
		noErr(t, os.MkdirAll(repoDir, 0o777))
	}

	process := processmock.New()
	ioi := newTestIO()
	driver := NewGitDriver(php.ArrayOf("url", url), deps(ioi, cfg, httpmock.New(), process))

	return driver, process, ioi, url, repoDir
}

func TestGitDriver_GetRootIdentifierAfterMirrorSync(t *testing.T) {
	for _, tc := range []struct {
		name             string
		gitVersion       string
		existingMirror   bool
		expectRemoteShow bool
	}{
		{"fresh clone on old git uses mirror HEAD", "2.47.0", false, false},
		{"updated mirror on old git asks the remote", "2.47.0", true, true},
		{"updated mirror on git 2.48+ uses mirror HEAD", "2.48.0", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			driver, process, ioi, url, repoDir := newMirrorSyncDriver(t, tc.gitVersion, tc.existingMirror)

			var expected []processmock.Expectation
			if tc.existingMirror {
				expected = append(expected,
					processmock.Expectation{Cmd: processmock.Cmd("git", "rev-parse", "--git-dir").Cmd, Stdout: ".\n"},
					processmock.Cmd("git", "remote", "-v"),
					processmock.Cmd("git", "remote", "set-url", "origin", "--", url),
					processmock.Cmd("git", "-c", "remote.origin.followRemoteHEAD=always", "remote", "update", "--prune", "origin"),
					processmock.Cmd("git", "gc", "--auto"),
				)
			} else {
				expected = append(expected, processmock.Cmd("git", "clone", "--mirror", "--", url, repoDir))
			}
			expected = append(expected,
				processmock.Cmd("git", "remote", "-v"),
				processmock.Cmd("git", "remote", "set-url", "origin", "--", url),
				processmock.Cmd("git", "show-ref", "--tags", "--dereference"),
				processmock.Cmd("git", "branch", "--no-color", "--no-abbrev", "-v"),
			)
			if tc.expectRemoteShow {
				expected = append(expected,
					processmock.Cmd("git", "remote", "-v"),
					processmock.Cmd("git", "remote", "set-url", "origin", "--", url),
					processmock.Expectation{Cmd: processmock.Cmd("git", "remote", "show", "origin").Cmd, Stdout: "* remote origin\n  HEAD branch: main\n"},
					processmock.Cmd("git", "remote", "-v"),
					processmock.Cmd("git", "remote", "set-url", "origin", "--", url),
				)
			} else {
				expected = append(expected, processmock.Expectation{Cmd: processmock.Cmd("git", "branch", "--no-color").Cmd, Stdout: "  2.2\n* main\n"})
			}
			process.Expects(expected, true, nil)

			noErr(t, driver.Initialize())
			assertRoot(t, driver, "main")
			assertProcessComplete(t, process)

			if msgs := ioi.messages(); len(msgs) != 0 {
				t.Fatalf("unexpected output %q", msgs)
			}
		})
	}
}

func TestGitDriver_GetRootIdentifierAsksRemoteAfterFailedMirrorSync(t *testing.T) {
	driver, process, _, url, _ := newMirrorSyncDriver(t, "2.48.0", true)

	process.Expects([]processmock.Expectation{
		{Cmd: processmock.Cmd("git", "rev-parse", "--git-dir").Cmd, Stdout: ".\n"},
		processmock.Cmd("git", "remote", "-v"),
		processmock.Cmd("git", "remote", "set-url", "origin", "--", url),
		{Cmd: processmock.Cmd("git", "-c", "remote.origin.followRemoteHEAD=always", "remote", "update", "--prune", "origin").Cmd, Return: 1},
		processmock.Cmd("git", "remote", "-v"),
		processmock.Cmd("git", "remote", "set-url", "origin", "--", url),
		processmock.Cmd("git", "show-ref", "--tags", "--dereference"),
		processmock.Cmd("git", "branch", "--no-color", "--no-abbrev", "-v"),
		processmock.Cmd("git", "remote", "-v"),
		processmock.Cmd("git", "remote", "set-url", "origin", "--", url),
		{Cmd: processmock.Cmd("git", "remote", "show", "origin").Cmd, Stdout: "* remote origin\n  HEAD branch: main\n"},
		processmock.Cmd("git", "remote", "-v"),
		processmock.Cmd("git", "remote", "set-url", "origin", "--", url),
	}, true, nil)

	noErr(t, driver.Initialize())
	assertRoot(t, driver, "main")
	assertProcessComplete(t, process)
}

func TestGitDriver_GetRootIdentifierFromLocalWithNetworkDisabled(t *testing.T) {
	t.Setenv("COMPOSER_DISABLE_NETWORK", "1")

	driver, process, _, home := newGitDriverTest(t, "https://example.org/acme.git")
	driver.repoDir = home

	process.Expects([]processmock.Expectation{
		{Cmd: processmock.Cmd("git", "branch", "--no-color").Cmd, Stdout: "* main\n  2.2\n  1.10"},
	}, false, nil)

	root, err := driver.RootIdentifier()
	noErr(t, err)

	if root != "main" {
		t.Fatalf("root = %q", root)
	}

	assertProcessComplete(t, process)
}

func TestGitDriver_GetBranchesFilterInvalidBranchNames(t *testing.T) {
	driver, process, _, home := newGitDriverTest(t, "https://example.org/acme.git")
	driver.repoDir = home

	// Branches starting with a - character are not valid git branches names
	// Still assert that they get filtered to prevent issues later on
	stdout := "* main 089681446ba44d6d9004350192486f2ceb4eaa06 commit\n  2.2  12681446ba44d6d9004350192486f2ceb4eaa06 commit\n  -h   089681446ba44d6d9004350192486f2ceb4eaa06 commit"

	process.Expects([]processmock.Expectation{
		{Cmd: processmock.Cmd("git", "branch", "--no-color", "--no-abbrev", "-v").Cmd, Stdout: stdout},
	}, false, nil)

	branches, err := driver.Branches()
	noErr(t, err)
	assertArray(t, branches, `{"main":"089681446ba44d6d9004350192486f2ceb4eaa06","2.2":"12681446ba44d6d9004350192486f2ceb4eaa06"}`)
	assertProcessComplete(t, process)
}

func TestGitDriver_FileGetContentInvalidIdentifier(t *testing.T) {
	driver, _, _, _ := newGitDriverTest(t, "https://example.org/acme.git")

	if content, ok, err := driver.FileContent("file.txt", "h"); err != nil || ok {
		t.Fatalf("got %q %v %v, want null", content, ok, err)
	}

	_, _, err := driver.FileContent("file.txt", "-h")
	expectRuntimeError(t, err, "Invalid git identifier detected. Identifier must not start with a -, given: -h")
}

func TestGitDriver_GetChangeDateInvalidIdentifier(t *testing.T) {
	driver, _, _, _ := newGitDriverTest(t, "https://example.org/acme.git")

	_, _, err := driver.ChangeDate("-n1 --format=%at HEAD")
	expectRuntimeError(t, err, "Invalid git identifier detected. Identifier must not start with a -, given: -n1 --format=%at HEAD")
}
