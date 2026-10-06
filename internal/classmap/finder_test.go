package classmap

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestRealDirCache checks the cached realpath against a plain realpath()
// over symlinks to files and directories, chains, dangling links, "." and
// ".." segments and files used as directories.
func TestRealDirCache(t *testing.T) {
	dir := uniqueTmpDirectory(t)
	writeFile(t, dir+"/a/b/f.php", "x")
	writeFile(t, dir+"/a/g.php", "x")
	for link, target := range map[string]string{
		"/a/lb":      "b",
		"/a/lb2":     "lb",
		"/lf.php":    "a/b/f.php",
		"/a/b/up":    "..",
		"/dangling":  "nowhere",
		"/abs":       dir + "/a",
		"/a/b/self":  ".",
		"/loop1":     "loop2",
		"/loop2":     "loop1",
		"/a/lnfile":  "g.php",
		"/a/b/lnabs": dir + "/a/g.php",
	} {
		if err := os.Symlink(target, dir+link); err != nil {
			t.Fatal(err)
		}
	}
	paths := []string{
		"/a/b/f.php", "/a/g.php", "/a/lb/f.php", "/a/lb2/f.php", "/lf.php", "/a/b/up/g.php", "/a/b/up/b/up/lb/f.php",
		"/dangling", "/dangling/x.php", "/abs/b/f.php", "/a/b/self/self/f.php", "/loop1/x.php", "/a/lnfile", "/a/b/lnabs",
		"/a/g.php/x.php", "/a/b/../g.php", "/a/lb/../g.php", "/a/./g.php", "/a/b/f.php/..", "/a/missing/x.php", "/a/b/",
		"/a/lb/./f.php", "/abs/../lf.php",
	}
	var c realDirCache
	for range 2 { // the second round hits the cache
		for _, p := range paths {
			want, wantOK := realpath(dir + p)
			for _, notLink := range []bool{false, true} {
				if notLink {
					info, err := os.Lstat(dir + p)
					if err != nil || info.Mode()&os.ModeSymlink != 0 {
						continue
					}
				}
				got, ok := c.realpath(dir+p, notLink)
				if got != want || ok != wantOK {
					t.Errorf("realpath(%s, %v) = %q %v, want %q %v", p, notLink, got, ok, want, wantOK)
				}
			}
		}
	}
}

func TestFinderNormalizeDir(t *testing.T) {
	for in, want := range map[string]string{
		"/": "/", "a/": "a", "a//": "a", "/a/b/": "/a/b", "ftp://h/x/": "ftp://h/x/", "sftp://h/x": "sftp://h/x/",
		"ssh2.sftp://h/x": "ssh2.sftp://h/x/", "ssh2.ftp://h": "ssh2.ftp://h/", "ftps://h": "ftps://h", "xftp://h": "xftp://h",
	} {
		if got := finderNormalizeDir(in); got != want {
			t.Errorf("finderNormalizeDir(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPhpGlobDirs(t *testing.T) {
	dir := uniqueTmpDirectory(t)
	for _, d := range []string{"a1", "a10", "a9", "b", ".hidden", "c[x]", "d,e", "e{f}"} {
		if err := os.MkdirAll(filepath.Join(dir, d, "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, dir+"/afile", "")
	t.Chdir(dir)
	// Results of PHP's glob() (with GLOB_BRACE | GLOB_ONLYDIR) and sort().
	for pattern, want := range map[string][]string{
		"a*":            {"a1", "a10", "a9"},
		"*":             {"a1", "a10", "a9", "b", "c[x]", "d,e", "e{f}"},
		".*":            {".", "..", ".hidden"},
		"{b,a1}":        {"a1", "b"},
		"{b,a{1,9}}":    {"a1", "a9", "b"},
		"*/sub":         {"a1/sub", "a10/sub", "a9/sub", "b/sub", "c[x]/sub", "d,e/sub", "e{f}/sub"},
		`c\[x]`:         {"c[x]"},
		"c[[]x]":        {"c[x]"},
		"[!a]*":         {"b", "c[x]", "d,e", "e{f}"},
		"e{f}":          {},
		`e\{f\}`:        {"e{f}"},
		`{d\,e,b}`:      {"b", "d,e"},
		"a1/":           {"a1/"},
		"nothing*":      {},
		"<dir>/a?":      {"<dir>/a1", "<dir>/a9"},
		"<dir>":         {"<dir>"},
		"{unbalanced":   {},
		"a1/sub/../sub": {"a1/sub/../sub"},
		"{a1,a1}":       {"a1", "a1"},
		"*{,/sub}":      {"a1", "a1/sub", "a10", "a10/sub", "a9", "a9/sub", "b", "b/sub", "c[x]", "c[x]/sub", "d,e", "d,e/sub", "e{f}", "e{f}/sub"},
		".*/sub":        {".hidden/sub"},
		"{.,..}":        {".", ".."},
		"[.]*":          {},
		"a1//sub":       {"a1//sub"},
		"/<dir>/b":      {"/<dir>/b"},
		"./b":           {"./b"},
		"*/../b":        {"a1/../b", "a10/../b", "a9/../b", "b/../b", "c[x]/../b", "d,e/../b", "e{f}/../b"},
	} {
		// PHP's own glob on Windows has no backslash quoting, and "/C:\..."
		// names no file there.
		if runtime.GOOS == "windows" && (strings.Contains(pattern, `\`) || strings.HasPrefix(pattern, "/<dir>")) {
			continue
		}
		pattern = strings.ReplaceAll(pattern, "<dir>", dir)
		for i := range want {
			want[i] = strings.ReplaceAll(want[i], "<dir>", dir)
		}
		got := phpGlobDirs(pattern)
		if len(got) != len(want) {
			t.Errorf("glob(%q) = %q, want %q", pattern, got, want)

			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("glob(%q) = %q, want %q", pattern, got, want)

				break
			}
		}
	}
}
