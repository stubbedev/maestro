package php

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDirnameOn(t *testing.T) {
	for _, c := range []struct {
		in, posix, windows string
	}{
		{"", "", ""},
		{"/", "/", `\`},
		{"/a/b", "/a", "/a"},
		{"/a/b/", "/a", "/a"},
		{"a", ".", "."},
		{"a/b", "a", "a"},
		{"//a", "/", `\`},
		{`c:\foo\bar`, ".", `c:\foo`},
		{`c:\foo`, ".", `c:\`},
		{"c:", ".", "c:"},
		{"c:foo", ".", "c:."},
		{`\\`, ".", `\`},
	} {
		if got := DirnameOn(c.in, false); got != c.posix {
			t.Errorf("DirnameOn(%q, posix) = %q, want %q", c.in, got, c.posix)
		}
		if got := DirnameOn(c.in, true); got != c.windows {
			t.Errorf("DirnameOn(%q, windows) = %q, want %q", c.in, got, c.windows)
		}
	}
}

func TestBasenameOn(t *testing.T) {
	for _, c := range []struct {
		in, suffix, posix, windows string
	}{
		{"", "", "", ""},
		{"/", "", "", ""},
		{"/a/b.php", "", "b.php", "b.php"},
		{"/a/b.php", ".php", "b", "b"},
		{"/a/.php", ".php", ".php", ".php"}, // not longer than the suffix
		{"a/b/", "", "b", "b"},
		{`c:\a\b`, "", `c:\a\b`, "b"},
	} {
		if got := BasenameOn(c.in, c.suffix, false); got != c.posix {
			t.Errorf("BasenameOn(%q, %q, posix) = %q, want %q", c.in, c.suffix, got, c.posix)
		}
		if got := BasenameOn(c.in, c.suffix, true); got != c.windows {
			t.Errorf("BasenameOn(%q, %q, windows) = %q, want %q", c.in, c.suffix, got, c.windows)
		}
	}
}

func TestPathinfoExtensionOn(t *testing.T) {
	for in, want := range map[string]string{
		"a.tar.gz": "gz", "/x/a.php": "php", "a": "", ".git": "git", "a.": "", "/x.y/a": "",
	} {
		if got := PathinfoExtensionOn(in, false); got != want {
			t.Errorf("PathinfoExtensionOn(%q) = %q, want %q", in, got, want)
		}
	}
	if got := PathinfoExtensionOn(`c:\x.y\a`, true); got != "" {
		t.Errorf("PathinfoExtensionOn(windows) = %q", got)
	}
}

// realpath() resolves each component before ".." applies, as PHP does:
// "link/../x" is x next to the link's target, not next to the link (what
// filepath.Abs's lexical cleaning gives).
func TestRealpath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir, err := EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"target/sub", "target/x", "x"} {
		if err := os.MkdirAll(filepath.Join(dir, p), 0o777); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "target", "sub"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}

	for in, want := range map[string]string{
		dir + "/link":          dir + "/target/sub",
		dir + "/link/../x":     dir + "/target/x",
		dir + "/link/./":       dir + "/target/sub",
		dir + "/target/../x/.": dir + "/x",
	} {
		if got, ok := Realpath(in); !ok || got != want {
			t.Errorf("Realpath(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	if got, ok := Realpath(dir + "/missing/.."); ok {
		t.Errorf("Realpath(missing/..) = %q, want false", got)
	}

	t.Chdir(dir + "/link")
	cwd, err := Getcwd()
	if err != nil || cwd != dir+"/target/sub" {
		t.Errorf("Getcwd() = %q, %v; want the physical directory", cwd, err)
	}
	if got, ok := Realpath(""); !ok || got != cwd {
		t.Errorf(`Realpath("") = %q, %v; want the working directory`, got, ok)
	}
	if got, ok := Realpath("../x"); !ok || got != dir+"/target/x" {
		t.Errorf("Realpath(../x) = %q, %v", got, ok)
	}
}
