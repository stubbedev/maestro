// Tests of the local-changes handling of GitDownloader and SvnDownloader
// (cleanChanges' prompts and the discard-changes setting) and of
// SvnDownloader, which has no test in Composer.

package vcs

import (
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/console"
	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/processmock"
)

// gitChanged are the commands of cleanChanges finding modified files.
func gitChanged(status string) []processmock.Expectation {
	return []processmock.Expectation{
		processmock.Cmd("git", "show-ref", "--head", "-d"),
		{Cmd: util.Cmd("git", "status", "--porcelain", "--untracked-files=no"), Stdout: status},
	}
}

func TestGitDownloader_CleanChangesPrompt(t *testing.T) {
	g := newGitTest(t)
	buffer := bufferIO(t, console.VerbosityNormal)
	buffer.SetUserInputs([]string{"x", "v", "d", "s"})
	g.io = buffer

	dir := gitDir(t, ".git")
	g.process.Expects(append(gitChanged(" M a.php\n M b.php\n"),
		processmock.Expectation{Cmd: util.Cmd("git", "diff", "HEAD"), Stdout: "the diff"},
		processmock.Cmd("git", "stash", "--include-untracked"),
	), true, nil)

	d := g.downloader(t)
	p := sourcePackage("1.0.0.0", "1.0.0", "ref")
	_, err := d.Prepare("update", p, dir, p)
	noError(t, err)
	assertComplete(t, g.process)

	want := strings.Join([]string{
		"    dummy/pkg has modified files:",
		"    M a.php",
		"    M b.php",
		"    Discard changes [y,n,v,d,s,?]? " +
			"    y - discard changes and apply the update",
		"    n - abort the update and let you manually clean things up",
		"    v - view modified files",
		"    d - view local modifications (diff)",
		"    s - stash changes and try to reapply them after the update",
		"    ? - print help",
		"    Discard changes [y,n,v,d,s,?]?     M a.php",
		"    M b.php",
		"    Discard changes [y,n,v,d,s,?]? the diff",
		"    Discard changes [y,n,v,d,s,?]? ",
	}, "\n")
	if got := php.NormalizeEOL(buffer.Output()); got != want {
		t.Fatalf("output:\n%s\nwant:\n%s", got, want)
	}

	// the stash is popped by cleanup, and later checkouts are forced
	g.process.Expects([]processmock.Expectation{processmock.Cmd("git", "stash", "pop")}, true, nil)
	_, err = d.Cleanup("update", p, dir, p)
	noError(t, err)
	assertComplete(t, g.process)
}

func TestGitDownloader_CleanChangesUninstallHasNoStash(t *testing.T) {
	g := newGitTest(t)
	buffer := bufferIO(t, console.VerbosityNormal)
	buffer.SetUserInputs([]string{"s", "n"})
	g.io = buffer

	dir := gitDir(t, ".git")
	g.process.Expects(gitChanged(" M a.php\n"), true, nil)

	p := sourcePackage("1.0.0.0", "1.0.0", "ref")
	_, err := g.downloader(t).Prepare("uninstall", p, dir, nil)
	wantError[*util.RuntimeError](t, err, "Update aborted")

	out := php.NormalizeEOL(buffer.Output())
	if !strings.Contains(out, "Discard changes [y,n,v,d,?]?") || !strings.Contains(out, "y - discard changes and apply the uninstall") || strings.Contains(out, "s - stash") {
		t.Fatalf("output %q", out)
	}
}

func TestGitDownloader_CleanChangesNonInteractive(t *testing.T) {
	// VcsDownloader::cleanChanges asks for the local changes again
	statusAgain := []processmock.Expectation{{Cmd: util.Cmd("git", "status", "--porcelain", "--untracked-files=no"), Stdout: " M a.php\n"}}

	cases := []struct {
		name     string
		discard  any
		uninst   bool
		commands []processmock.Expectation
		err      string
	}{
		{name: "fail", discard: false, commands: statusAgain, err: "Source directory %s has uncommitted changes."},
		{name: "discard", discard: true, commands: []processmock.Expectation{
			processmock.Cmd("git", "clean", "-df"),
			processmock.Cmd("git", "reset", "--hard"),
		}},
		{name: "stash", discard: "stash", commands: []processmock.Expectation{
			processmock.Cmd("git", "stash", "--include-untracked"),
		}},
		{name: "stash uninstall", discard: "stash", uninst: true, commands: statusAgain, err: "Source directory %s has uncommitted changes."},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newGitTest(t)
			g.config = newConfig(t, "discard-changes", c.discard)

			dir := gitDir(t, ".git")
			g.process.Expects(append(gitChanged(" M a.php\n"), c.commands...), true, nil)

			p := sourcePackage("1.0.0.0", "1.0.0", "ref")

			var err error
			if c.uninst {
				_, err = g.downloader(t).Prepare("uninstall", p, dir, nil)
			} else {
				_, err = g.downloader(t).Prepare("update", p, dir, p)
			}

			if c.err != "" {
				// normalizePath() realpath()s the directory on Windows.
				shown := dir
				if util.IsWindows() {
					shown = util.Realpath(dir)
				}
				wantError[*util.RuntimeError](t, err, strings.Replace(c.err, "%s", shown, 1))

				return
			}

			noError(t, err)
			assertComplete(t, g.process)
		})
	}
}

func TestGitDownloader_UnpushedChanges(t *testing.T) {
	head := "abc123"
	refs := head + " HEAD\n" + head + " refs/heads/main\n" + "def456 refs/remotes/origin/main\n"

	g := newGitTest(t)
	dir := gitDir(t, ".git")
	g.process.Expects([]processmock.Expectation{
		{Cmd: util.Cmd("git", "show-ref", "--head", "-d"), Stdout: refs},
		{Cmd: util.Cmd("git", "diff", "--name-status", "origin/main...main", "--"), Stdout: "M\tfile.php\n"},
		processmock.Cmd("git", "fetch", "--all"),
		{Cmd: util.Cmd("git", "show-ref", "--head", "-d"), Stdout: refs},
		{Cmd: util.Cmd("git", "diff", "--name-status", "origin/main...main", "--"), Stdout: "M\tfile.php\n"},
	}, true, nil)

	got, err := g.downloader(t).UnpushedChanges(sourcePackage("1.0.0.0", "1.0.0", "ref"), dir)
	noError(t, err)

	if !got.Valid || got.S != "M\tfile.php" {
		t.Fatalf("got %+v", got)
	}

	assertComplete(t, g.process)

	// a branch on no remote
	g.process.Expects([]processmock.Expectation{
		{Cmd: util.Cmd("git", "show-ref", "--head", "-d"), Stdout: head + " HEAD\n" + head + " refs/heads/topic\n"},
		processmock.Cmd("git", "fetch", "--all"),
		{Cmd: util.Cmd("git", "show-ref", "--head", "-d"), Stdout: head + " HEAD\n" + head + " refs/heads/topic\n"},
	}, true, nil)

	got, err = g.downloader(t).UnpushedChanges(sourcePackage("1.0.0.0", "1.0.0", "ref"), dir)
	noError(t, err)

	if got.S != "Branch topic could not be found on any remote and appears to be unpushed" {
		t.Fatalf("got %+v", got)
	}

	assertComplete(t, g.process)
}

func TestGitDownloader_UpdateToCommitGone(t *testing.T) {
	g := newGitTest(t)
	buffer := bufferIO(t, console.VerbosityNormal)
	g.io = buffer

	ref := "abcdefabcdefabcdefabcdefabcdefabcdefabcd"
	p := sourcePackage("1.0.0.0", "1.0.0", ref, "https://example.com/a/b")

	g.process.Expects([]processmock.Expectation{
		processmock.Cmd("git", "clone", "--no-checkout", "--", "https://example.com/a/b", composerPath(t)),
		processmock.Cmd("git", "remote", "add", "composer", "--", "https://example.com/a/b"),
		processmock.Cmd("git", "fetch", "composer"),
		processmock.Cmd("git", "remote", "set-url", "origin", "--", "https://example.com/a/b"),
		processmock.Cmd("git", "remote", "set-url", "composer", "--", "https://example.com/a/b"),
		{Cmd: util.Cmd("git", "branch", "-r"), Stdout: "  composer/v1.0.0\n"},
		{Cmd: util.Cmd("git", "checkout", "v1.0.0", "--"), Return: 1},
		{Cmd: util.Cmd("git", "checkout", "-B", "v1.0.0", "composer/v1.0.0", "--"), Return: 1},
		{Cmd: util.Cmd("git", "checkout", ref, "--"), Return: 1, Stderr: "fatal: reference is not a tree: " + ref},
	}, true, nil)

	_, err := g.downloader(t).Install(p, "composerPath")
	wantError[*util.RuntimeError](t, err, "Failed to execute git checkout "+ref+" -- && git reset --hard "+ref+" --\n\nfatal: reference is not a tree: "+ref+
		"\nIt looks like the commit hash is not available in the repository, maybe the tag was recreated? Run \"composer update dummy/pkg\" to resolve this.")

	if !strings.Contains(php.NormalizeEOL(buffer.Output()), ref+" is gone (history was rewritten?)") {
		t.Fatalf("output %q", php.NormalizeEOL(buffer.Output()))
	}
}

func svnDownloader(t *testing.T, io mio.IO, process *processmock.Mock, kv ...any) *SvnDownloader {
	t.Helper()

	return NewSvnDownloader(Deps{IO: io, Config: newConfig(t, kv...), Process: process, Filesystem: &fakeFS{}})
}

func TestSvnDownloader_LocalChanges(t *testing.T) {
	dir := gitDir(t, ".svn")

	for status, changed := range map[string]bool{
		"":                   false,
		"X      external\n":  false,
		"M       file.php\n": true,
		"?       new.php\n":  true,
	} {
		process := processmock.New()
		process.Expects([]processmock.Expectation{{Cmd: util.Cmd("svn", "status", "--ignore-externals"), Stdout: status}}, true, nil)

		got, err := svnDownloader(t, mio.NewNullIO(), process).LocalChanges(sourcePackage("1.0.0.0", "1.0.0", "ref"), dir)
		noError(t, err)

		if got.Valid != changed || (changed && got.S != status) {
			t.Fatalf("status %q: got %+v", status, got)
		}
	}
}

func TestSvnDownloader_CleanChanges(t *testing.T) {
	dir := gitDir(t, ".svn")
	status := "M       a.php\n"

	process := processmock.New()
	process.Expects([]processmock.Expectation{
		{Cmd: util.Cmd("svn", "status", "--ignore-externals"), Stdout: status},
		processmock.Cmd("svn", "revert", "-R", "."),
	}, true, nil)

	p := sourcePackage("1.0.0.0", "1.0.0", "ref")
	_, err := svnDownloader(t, mio.NewNullIO(), process, "discard-changes", true).Prepare("update", p, dir, p)
	noError(t, err)
	assertComplete(t, process)

	buffer := bufferIO(t, console.VerbosityNormal)
	buffer.SetUserInputs([]string{"?", "n"})

	process.Expects([]processmock.Expectation{{Cmd: util.Cmd("svn", "status", "--ignore-externals"), Stdout: status}}, true, nil)
	_, err = svnDownloader(t, buffer, process).Prepare("uninstall", p, dir, nil)
	wantError[*util.RuntimeError](t, err, "Update aborted")

	want := strings.Join([]string{
		"    dummy/pkg has modified files:",
		"    M       a.php",
		"    ",
		"    Discard changes [y,n,v,?]? " +
			"    y - discard changes and apply the uninstall",
		"    n - abort the uninstall and let you manually clean things up",
		"    v - view modified files",
		"    ? - print help",
		"    Discard changes [y,n,v,?]? ",
	}, "\n")
	if got := php.NormalizeEOL(buffer.Output()); got != want {
		t.Fatalf("output:\n%q\nwant:\n%q", got, want)
	}
}

func TestSvnDownloader_CommitLogsWithoutRevisions(t *testing.T) {
	got, err := svnDownloader(t, mio.NewNullIO(), processmock.New()).commitLogs("trunk", "trunk@12", "/path")
	noError(t, err)

	if want := "Could not retrieve changes between trunk and trunk@12 due to missing revision information"; got != want {
		t.Fatalf("got %q", got)
	}
}

func TestSvnDownloader_InstallErrorNamesPackage(t *testing.T) {
	dir := t.TempDir()
	p := sourcePackage("1.0.0.0", "1.0.0", "trunk@3", "https://svn.example.org/repo")

	process := processmock.New()
	process.Expects([]processmock.Expectation{
		{Cmd: util.Cmd("svn", "co", "--non-interactive", "--", "https://svn.example.org/repo/trunk@3", dir), Return: 1, Stderr: "svn: E170013: unreachable"},
	}, true, nil)

	_, err := svnDownloader(t, mio.NewNullIO(), process).Install(p, dir)
	wantError[*util.RuntimeError](t, err, "dummy/pkg could not be downloaded, ")
}
