package phperr_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/phperr"
)

// Frames go to the exception and to its previous exceptions, innermost
// first, through Go wrappers keeping the message; TraceOf reads them from
// the same error SiteOf takes the site from.
func TestCall(t *testing.T) {
	prev := &exc{msg: "inner", Site: phperr.At("VersionParser.php", 526)}
	phperr.Call(prev, `Composer\Semver\VersionParser->parseConstraint`, "VersionParser.php", 281)
	e := &exc{msg: "outer", prev: prev, Site: phperr.At("ArrayLoader.php", 412)}
	var err error = &wrapper{err: e}

	if got := phperr.Call(err, `Composer\Package\Loader\ArrayLoader->createLink`, "ArrayLoader.php", 384); got != err {
		t.Errorf("Call returned %v, want its error", got)
	}
	phperr.Calls(err, phperr.Frame{Function: `Composer\Package\Loader\ArrayLoader->parseLinks`, File: "ArrayLoader.php", Line: 63})

	want := []phperr.Frame{
		{Function: `Composer\Package\Loader\ArrayLoader->createLink`, File: "ArrayLoader.php", Line: 384},
		{Function: `Composer\Package\Loader\ArrayLoader->parseLinks`, File: "ArrayLoader.php", Line: 63},
	}
	if got := phperr.TraceOf(err); !slices.Equal(got, want) {
		t.Errorf("TraceOf(wrapper) = %v, want %v", got, want)
	}
	want = append([]phperr.Frame{{Function: `Composer\Semver\VersionParser->parseConstraint`, File: "VersionParser.php", Line: 281}}, want...)
	if got := phperr.TraceOf(prev); !slices.Equal(got, want) {
		t.Errorf("TraceOf(previous) = %v, want %v", got, want)
	}

	if phperr.Call(nil, "f", "X.php", 1) != nil {
		t.Error("Call(nil) != nil")
	}
	// errors without a trace of their own are left alone
	plain := errors.New("plain")
	if phperr.Call(plain, "f", "X.php", 1) != plain || phperr.TraceOf(plain) != nil {
		t.Error("Call on an untraced error")
	}
}

func TestAbsPath(t *testing.T) {
	phperr.SetRoot("phar:///usr/local/bin/maestro")
	t.Cleanup(func() { phperr.SetRoot("") })

	for file, want := range map[string]string{
		"Factory.php":       "phar:///usr/local/bin/maestro/src/Composer/Factory.php",
		"VersionParser.php": "phar:///usr/local/bin/maestro/vendor/composer/semver/src/VersionParser.php",
		"vendor/symfony/filesystem/Filesystem.php": "phar:///usr/local/bin/maestro/vendor/symfony/filesystem/Filesystem.php",
		"bin/composer":           "phar:///usr/local/bin/maestro/bin/composer",
		"/home/u/plugin/Foo.php": "/home/u/plugin/Foo.php",
		"phar:///x.phar/Foo.php": "phar:///x.phar/Foo.php",
		"":                       "",
	} {
		if got := phperr.AbsPath(file); got != want {
			t.Errorf("AbsPath(%q) = %q, want %q", file, got, want)
		}
	}
}

// Every PHP file maestro's sources name by its basename resolves to a file
// of Composer's tree (paths.go; regenerate it when one is missing). With
// the reference Composer in .ref/composer, the paths exist there.
func TestPathsCoverSources(t *testing.T) {
	root := filepath.Join("..", "..")
	literal := regexp.MustCompile(`"([A-Za-z0-9_-]+\.php)"`)
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		if strings.Contains(path, string(filepath.Separator)+"plugin"+string(filepath.Separator)) {
			return nil // the plugin shim's own PHP files
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// the files naming throw sites: those using phperr, and the
		// console's and commands' file constants
		dir := filepath.Base(filepath.Dir(path))
		if !strings.Contains(string(data), "phperr.") && dir != "console" && dir != "command" && dir != "eventdispatcher" {
			return nil
		}
		for _, m := range literal.FindAllStringSubmatch(string(data), -1) {
			if phperr.Path(m[1]) == m[1] && !notComposer[m[1]] {
				t.Errorf("%s: %s is not in paths.go", path, m[1])
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	ref := filepath.Join(root, ".ref", "composer")
	if _, err := os.Stat(ref); err != nil {
		t.Skip("no reference Composer in .ref/composer")
	}
	for _, base := range phperr.Basenames() {
		if _, err := os.Stat(filepath.Join(ref, phperr.Path(base))); err != nil {
			t.Errorf("%s: %v", base, err)
		}
	}
}

// notComposer are PHP file names in maestro's sources that are not files of
// Composer's tree, or that name several (given as paths where they are
// throw sites).
var notComposer = map[string]bool{
	"Application.php":        true, // symfony/console's and Composer's
	"HttpDownloaderMock.php": true, // tests/ of Composer
	"include_paths.php":      true, // a file the autoload dump writes
}
