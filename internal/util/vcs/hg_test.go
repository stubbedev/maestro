// Tests of src/Composer/Util/Hg.php (Composer has no HgTest).

package vcs

import (
	"testing"

	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/processmock"
)

func hgClone(url string) util.Command { return util.Cmd("hg", "clone", "--", url, "dir") }

func TestHg_RunCommandAsIs(t *testing.T) {
	process := processmock.New()
	process.Expects([]processmock.Expectation{processmock.Cmd("hg", "clone", "--", "https://example.org/r", "dir")}, true, nil)

	if err := NewHg(newFakeIO(), newFakeConfig(nil), process).RunCommand(hgClone, "https://example.org/r", ""); err != nil {
		t.Fatal(err)
	}

	assertComplete(t, process)
}

func TestHg_RunCommandWithAuthentication(t *testing.T) {
	for _, tc := range []struct{ url, authURL string }{
		{"https://example.org/r", "https://us%20er:p%40ss@example.org/r"},
		{"ssh://bob@example.org/r", "ssh://bob@example.org/r"},
		{"ssh://example.org/r", "ssh://example.org/r"},
	} {
		fio := newFakeIO()
		fio.auths["example.org"] = authOf("us er", "p@ss")

		process := processmock.New()
		process.Expects([]processmock.Expectation{
			{Cmd: hgClone(tc.url), Return: 1},
			processmock.Cmd("hg", "clone", "--", tc.authURL, "dir"),
		}, true, nil)

		if err := NewHg(fio, newFakeConfig(nil), process).RunCommand(hgClone, tc.url, ""); err != nil {
			t.Fatal(err)
		}

		assertComplete(t, process)
	}
}

func TestHg_RunCommandErrors(t *testing.T) {
	hgVersion.set("", false)
	t.Cleanup(func() { hgVersion.set("", false) })

	process := processmock.New()
	process.Expects([]processmock.Expectation{
		{Cmd: hgClone("https://u:secret@example.org/r"), Return: 1},
		{Cmd: util.Cmd("hg", "--version"), Stdout: "Mercurial Distributed SCM (version 6.5.2)\n"},
	}, true, nil)

	err := NewHg(newFakeIO(), newFakeConfig(nil), process).RunCommand(hgClone, "https://u:secret@example.org/r", "")

	want := "Failed to clone https://u:***@example.org/r, \n\nThe given URL (https://u:***@example.org/r) does not match the required format (ssh|http(s)://(username:password@)example.com/path-to-repository)"
	if err == nil || err.Error() != want {
		t.Fatalf("got %v\nwant %s", err, want)
	}

	if v, ok, _ := GetHgVersion(process); !ok || v != "6.5.2" {
		t.Fatalf("version %q %v", v, ok)
	}

	// cached: no second hg --version
	process.Expects([]processmock.Expectation{{Cmd: hgClone("git://x/r"), Return: 1}}, true, nil)

	if err := NewHg(newFakeIO(), newFakeConfig(nil), process).RunCommand(hgClone, "git://x/r", ""); err == nil {
		t.Fatal("no error")
	}

	assertComplete(t, process)
}

func TestHg_NotFound(t *testing.T) {
	hgVersion.set("", false)
	t.Cleanup(func() { hgVersion.set("", false) })

	process := processmock.New()
	process.Expects([]processmock.Expectation{
		{Cmd: hgClone("git://x/r"), Return: 1},
		{Cmd: util.Cmd("hg", "--version"), Return: 127, Stderr: "sh: hg: not found"},
	}, true, nil)

	err := NewHg(newFakeIO(), newFakeConfig(nil), process).RunCommand(hgClone, "git://x/r", "")
	if want := "Failed to clone git://x/r, hg was not found, check that it is installed and in your PATH env.\n\nsh: hg: not found"; err == nil || err.Error() != want {
		t.Fatalf("got %v", err)
	}
}
