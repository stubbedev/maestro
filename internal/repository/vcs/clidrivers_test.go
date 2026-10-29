package vcs

import (
	"os"
	"path/filepath"
	"strings"
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

func TestHgDriver_UpdateFailureOutputIsSanitized(t *testing.T) {
	home := t.TempDir()
	repoDir := filepath.Join(home, "cache", "vcs", "https---example-org-acme") + "/"
	noErr(t, os.MkdirAll(repoDir, 0o777))

	process := processmock.New()
	process.Expects([]processmock.Expectation{
		{Cmd: processmock.Cmd("hg", "summary").Cmd},
		{Cmd: processmock.Cmd("hg", "pull").Cmd, Return: 255, Stderr: "pulling from https://user:secret@example.org/acme\nabort: HTTP Error 500\n"},
	}, false, nil)

	ioi := newTestIO()
	cfg := newConfig(t, home, "cache-vcs-dir", filepath.Join(home, "cache", "vcs"))
	driver := NewHgDriver(php.ArrayOf("url", "https://example.org/acme"), deps(ioi, cfg, httpmock.New(), process))
	noErr(t, driver.Initialize())

	output := strings.Join(ioi.messages(), "\n")
	if !strings.Contains(output, "pulling from https://user:***@example.org/acme") {
		t.Errorf("output %q", output)
	}
	if strings.Contains(output, "secret") {
		t.Errorf("output %q holds the credentials", output)
	}
}

func TestFossilDriver_CloneFailureOutputIsSanitized(t *testing.T) {
	home := t.TempDir()
	url := "https://user:secret@example.org/acme"
	repoFile := filepath.Join(home, "cache", "repo") + "/https---user-----example-org-acme.fossil"

	process := processmock.New()
	process.Expects([]processmock.Expectation{
		{Cmd: processmock.Cmd("fossil", "version").Cmd},
		{Cmd: processmock.Cmd("fossil", "clone", "--", url, repoFile).Cmd, Return: 1, Stderr: `Error: unable to connect to "` + url + `"`},
	}, true, nil)

	cfg := newConfig(t, home,
		"cache-repo-dir", filepath.Join(home, "cache", "repo"),
		"cache-vcs-dir", filepath.Join(home, "cache", "vcs"),
	)
	driver := NewFossilDriver(php.ArrayOf("url", url), deps(newTestIO(), cfg, httpmock.New(), process))

	err := driver.Initialize()
	expectRuntimeError(t, err, "")
	if !strings.Contains(err.Error(), `unable to connect to "https://user:***@example.org/acme"`) {
		t.Errorf("error %q", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("error %q holds the credentials", err)
	}
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
