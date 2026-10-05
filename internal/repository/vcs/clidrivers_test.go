package vcs

import (
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util/http/httpmock"
	"github.com/stubbedev/maestro/internal/util/processmock"
	uvcs "github.com/stubbedev/maestro/internal/util/vcs"
)

// Ports tests/Composer/Test/Repository/Vcs/{HgDriverTest,FossilDriverTest,SvnDriverTest}.php.

func TestHgDriver_Supports(t *testing.T) {
	t.Parallel()

	for _, url := range []string{
		"ssh://bitbucket.org/user/repo",
		"ssh://hg@bitbucket.org/user/repo",
		"ssh://user@bitbucket.org/user/repo",
		"https://bitbucket.org/user/repo",
		"https://user@bitbucket.org/user/repo",
	} {
		ok, err := hgDriverType.Supports(deps(newTestIO(), newConfig(t, t.TempDir()), nil, processmock.New()), url, false)
		if err != nil || !ok {
			t.Errorf("supports(%q) = %v, %v", url, ok, err)
		}
	}
}

func newHgDriverTest(t *testing.T) (*HgDriver, *processmock.Mock) {
	t.Helper()

	process := processmock.New()
	driver := NewHgDriver(php.ArrayOf("url", "https://example.org/acme.git"), deps(newTestIO(), newConfig(t, t.TempDir()), httpmock.New(), process))

	return driver, process
}

func TestHgDriver_GetBranchesFilterInvalidBranchNames(t *testing.T) {
	t.Parallel()

	driver, process := newHgDriverTest(t)

	process.Expects([]processmock.Expectation{
		{Cmd: processmock.Cmd("hg", "branches").Cmd, Stdout: "default 1:dbf6c8acb640\n--help  1:dbf6c8acb640"},
		{Cmd: processmock.Cmd("hg", "bookmarks").Cmd, Stdout: "help    1:dbf6c8acb641\n--help  1:dbf6c8acb641\n"},
	}, false, nil)

	branches, err := driver.Branches()
	noErr(t, err)
	assertArray(t, branches, `{"help":"dbf6c8acb641","default":"dbf6c8acb640"}`)
	assertProcessComplete(t, process)
}

func TestHgDriver_FileGetContentInvalidIdentifier(t *testing.T) {
	t.Parallel()

	driver, _ := newHgDriverTest(t)

	if content, ok, err := driver.FileContent("file.txt", "h"); err != nil || ok {
		t.Fatalf("got %q %v %v, want null", content, ok, err)
	}

	_, _, err := driver.FileContent("file.txt", "-h")
	expectRuntimeError(t, err, "Invalid hg identifier detected. Identifier must not start with a -, given: -h")
}

func TestHgDriver_GetChangeDateInvalidIdentifier(t *testing.T) {
	t.Parallel()

	driver, _ := newHgDriverTest(t)

	_, _, err := driver.ChangeDate("-r foo")
	expectRuntimeError(t, err, "Invalid hg identifier detected. Identifier must not start with a -, given: -r foo")
}

func TestFossilDriver_Support(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		url  string
		want bool
	}{
		{"http://fossil.kd2.org/kd2fw/", true},
		{"https://chiselapp.com/user/rkeene/repository/flint/index", true},
		{"ssh://fossil.kd2.org/kd2fw.fossil", true},
	} {
		got, err := fossilDriverType.Supports(deps(newTestIO(), newConfig(t, t.TempDir()), nil, processmock.New()), c.url, false)
		if err != nil || got != c.want {
			t.Errorf("supports(%q) = %v, %v", c.url, got, err)
		}
	}
}

// TestSvnDriver_WrongCredentialsInUrl must not run in parallel: it pins
// the process-wide svn version cache.
func TestSvnDriver_WrongCredentialsInUrl(t *testing.T) {
	uvcs.SetSvnVersion("", false)
	t.Cleanup(func() { uvcs.SetSvnVersion("", false) })

	output := "svn: OPTIONS of 'https://corp.svn.local/repo':" +
		" authorization failed: Could not authenticate to server:" +
		" rejected Basic challenge (https://corp.svn.local/)"

	authedCommand := processmock.Cmd("svn", "ls", "--verbose", "--non-interactive", "--username", "till", "--password", "secret", "--", "https://till:secret@corp.svn.local/repo/trunk").Cmd
	failure := processmock.Expectation{Cmd: authedCommand, Return: 1, Stderr: output}

	process := processmock.New()
	process.Expects([]processmock.Expectation{
		failure, failure, failure, failure, failure, failure,
		{Cmd: processmock.Cmd("svn", "--version").Cmd, Stdout: "1.2.3"},
	}, true, nil)

	svn := NewSvnDriver(php.ArrayOf("url", "https://till:secret@corp.svn.local/repo"), deps(newTestIO(), newConfig(t, t.TempDir()), nil, process))

	err := svn.Initialize()
	expectRuntimeError(t, err, "Repository https://till:***@corp.svn.local/repo could not be processed, wrong credentials provided (svn: OPTIONS of 'https://corp.svn.local/repo': authorization failed: Could not authenticate to server: rejected Basic challenge (https://corp.svn.local/))")
	assertProcessComplete(t, process)
}

func TestSvnDriver_Support(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		url  string
		want bool
	}{
		{"http://svn.apache.org", true},
		{"https://svn.sf.net", true},
		{"svn://example.org", true},
		{"svn+ssh://example.org", true},
	} {
		got, err := svnDriverType.Supports(deps(newTestIO(), newConfig(t, t.TempDir()), nil, processmock.New()), c.url, false)
		if err != nil || got != c.want {
			t.Errorf("supports(%q) = %v, %v", c.url, got, err)
		}
	}
}
